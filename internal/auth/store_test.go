package auth_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"oras.land/oras-go/v2/registry/remote/credentials/trace"

	"github.com/imgoci/go/internal/auth"
)

// The registries the rows below are written for and looked up under.
const (
	// fixtureRegistry is an ordinary registry host carrying a port, which is
	// the shape that has to survive a lookup unrewritten.
	fixtureRegistry = "registry.example:5000"
	// hubRegistry is the registry a Docker Hub reference names, which is the
	// spelling users actually write.
	hubRegistry = "docker.io"
	// hubDialedHost is the host such a transfer really dials, the other
	// spelling a caller can write out explicitly.
	hubDialedHost = "registry-1.docker.io"
	// hubConfigKey is the key `docker login` files a Docker Hub credential
	// under. It is not the host above, and that mismatch is the only reason
	// the lookup translates anything at all.
	hubConfigKey = "https://index.docker.io/v1/"
)

// What the fake credential helper prints when it is asked for a credential.
const (
	// helperUser is the user name it answers with.
	helperUser = "helper-user"
	// helperSecret is the secret it answers with.
	helperSecret = "helper-secret"
)

// fakeHelperScript is a credential helper that answers every lookup with the
// same credential and records the action it was asked to perform.
//
// The record's path is written into the program rather than passed in the
// environment, because the environment is shared by the whole test binary and
// a record file must not be: a row proving a helper ran and a row proving one
// did not would otherwise be reading the same file.
const fakeHelperScript = `#!/bin/sh
echo "$1" >> %q
echo '{"ServerURL":"","Username":"` + helperUser + `","Secret":"` + helperSecret + `"}'
`

// wedgedHelperScript is a credential helper that never answers. It records
// that it started and then sleeps for longer than any test would wait.
//
// The sleep replaces the shell rather than running under it, so the one
// process the lookup knows about is the one holding its output open: a
// grandchild left behind would keep the pipe open after its parent was killed
// and the lookup would wait for the sleep it was meant to escape.
const wedgedHelperScript = `#!/bin/sh
PATH=/usr/bin:/bin
export PATH
echo "$1" >> %q
exec sleep 60
`

// wedgedHelperBudget is the longest the lookup may take before the row fails.
// It sits comfortably past the ten seconds the store allows itself and well
// short of the minute the helper above would otherwise take.
const wedgedHelperBudget = 30 * time.Second

// The modes the fixtures are written with. Both are the owner's business and
// nobody else's, which is what a file holding credentials is.
const (
	// configPerm is the mode a planted configuration file gets.
	configPerm os.FileMode = 0o600
	// helperPerm is the mode a credential helper gets: the test user also has
	// to be able to run it.
	helperPerm os.FileMode = 0o700
)

// TestStoreCredentialReadsTheConfiguration walks the shapes a Docker
// configuration stores a credential in, planted in a file only the row can
// see, and checks each comes back in the right Credential field.
func TestStoreCredentialReadsTheConfiguration(t *testing.T) {
	t.Parallel()

	planted := fmt.Sprintf(`{"auth":%q}`, basicAuth("alice", "s3cret"))

	tests := []struct {
		name     string
		config   string
		registry string
		want     auth.Credential
	}{
		{
			name:     "an entry for the registry becomes a user name and a password",
			config:   authsConfig(fixtureRegistry, planted),
			registry: fixtureRegistry,
			want:     auth.Credential{Username: "alice", Password: "s3cret"},
		},
		{
			name:     "a registry the configuration does not mention is anonymous",
			config:   authsConfig(fixtureRegistry, planted),
			registry: "other.example",
			want:     auth.Credential{},
		},
		{
			name:     "a Docker Hub login is found for the registry a reference names",
			config:   authsConfig(hubConfigKey, fmt.Sprintf(`{"auth":%q}`, basicAuth("bob", "hub-token"))),
			registry: hubRegistry,
			want:     auth.Credential{Username: "bob", Password: "hub-token"},
		},
		{
			name:     "a Docker Hub login is found for the dialed host spelled out",
			config:   authsConfig(hubConfigKey, fmt.Sprintf(`{"auth":%q}`, basicAuth("bob", "hub-token"))),
			registry: hubDialedHost,
			want:     auth.Credential{Username: "bob", Password: "hub-token"},
		},
		{
			name: "an identity token beside a password uses the password",
			config: authsConfig(
				fixtureRegistry,
				fmt.Sprintf(`{"auth":%q,"identitytoken":"refresh-me"}`, basicAuth("alice", "s3cret")),
			),
			registry: fixtureRegistry,
			want:     auth.Credential{Username: "alice", Password: "s3cret"},
		},
		{
			name: "an access token beside a password uses the password",
			config: authsConfig(
				fixtureRegistry,
				fmt.Sprintf(`{"auth":%q,"registrytoken":"access-me"}`, basicAuth("alice", "s3cret")),
			),
			registry: fixtureRegistry,
			want:     auth.Credential{Username: "alice", Password: "s3cret"},
		},
		{
			name:     "a configuration holding nothing at all is anonymous",
			config:   `{}`,
			registry: fixtureRegistry,
			want:     auth.Credential{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newStore(t, plantConfig(t, tt.config))

			got, err := store.Credential(noExec(t), tt.registry)
			if err != nil {
				t.Fatalf("Credential: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Credential = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestStoreCredentialNeverRepeatsAMalformedEntry pins the error shape: an
// auth entry that decodes to something other than user:password is reported
// without its decoded content, because that content is usually a secret
// somebody pasted where a base64 pair belongs, and the message reaches
// terminals and CI logs verbatim.
func TestStoreCredentialNeverRepeatsAMalformedEntry(t *testing.T) {
	t.Parallel()

	const pasted = "ghp_SUPERSECRETTOKENVALUE"
	config := authsConfig(
		fixtureRegistry,
		fmt.Sprintf(`{"auth":%q}`, base64.StdEncoding.EncodeToString([]byte(pasted))),
	)
	path := plantConfig(t, config)
	store := newStore(t, path)

	_, err := store.Credential(noExec(t), fixtureRegistry)
	if err == nil {
		t.Fatal("a malformed auth entry must fail the lookup")
	}
	if strings.Contains(err.Error(), pasted) {
		t.Fatalf("the decoded entry is the secret and stays out of the message: %v", err)
	}
	if !strings.Contains(err.Error(), fixtureRegistry) {
		t.Fatalf("the message names the registry: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the message names the file: %v", err)
	}
}

// TestStoreCredentialRejectsUnsupportedTokens pins the loud refusal: a stored
// refresh or access token this client cannot present must not become an
// anonymous credential.
func TestStoreCredentialRejectsUnsupportedTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		entry  string
		secret string
	}{
		{
			name:   "refresh token",
			entry:  `{"identitytoken":%q}`,
			secret: "refresh-me-SUPERSECRET",
		},
		{
			name:   "access token",
			entry:  `{"registrytoken":%q}`,
			secret: "access-me-SUPERSECRET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := authsConfig(fixtureRegistry, fmt.Sprintf(tt.entry, tt.secret))
			assertUnsupportedTokenRefused(t, plantConfig(t, config), tt.secret)
		})
	}
}

// TestStoreCredentialOnAConfigurationThatIsNotThere pins the zero-config
// default: a machine nobody has logged in on resolves every registry to the
// anonymous credential, with no error anywhere on the way.
func TestStoreCredentialOnAConfigurationThatIsNotThere(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	pin(t, path)

	store, err := auth.NewStore(path)
	if err != nil {
		t.Fatalf("a machine nobody has logged in on is not a failure: %v", err)
	}

	got, err := store.Credential(noExec(t), fixtureRegistry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if !got.Empty() {
		t.Fatalf("a configuration that is not there resolves to the anonymous credential, got %+v", got)
	}
}

// TestNewStoreOnAConfigurationItCannotRead pins the one failure NewStore
// owns: a file that exists but is not a configuration fails at build time,
// where the caller who asked for credentials can still be told.
func TestNewStoreOnAConfigurationItCannotRead(t *testing.T) {
	t.Parallel()

	path := plantConfig(t, `{"auths": this is not a configuration`)

	store, err := auth.NewStore(path)
	if err == nil {
		t.Fatal("a caller who asked for credentials must not transfer anonymously instead")
	}
	if store != nil {
		t.Fatal("a broken configuration must not return a store")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the failure has to say which file could not be read: %v", err)
	}
}

// TestStoreReadsTheFileItWasGivenAndSaysWhichOneItWas is where the suite's
// isolation stops being a claim. The credential comes back from a file this
// row wrote seconds ago in a directory of its own, and the store names that
// same file when asked — so the answer cannot have come from the machine's
// real configuration, whatever is in it and whoever is logged in.
func TestStoreReadsTheFileItWasGivenAndSaysWhichOneItWas(t *testing.T) {
	t.Parallel()

	planted := authsConfig(fixtureRegistry, fmt.Sprintf(`{"auth":%q}`, basicAuth("alice", "s3cret")))
	path := plantConfig(t, planted)
	store := newStore(t, path)

	got, err := store.Credential(noExec(t), fixtureRegistry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	want := auth.Credential{Username: "alice", Password: "s3cret"}
	if got != want {
		t.Fatalf("Credential = %+v, want %+v", got, want)
	}
	if store.ConfigPath() != path {
		t.Fatalf("ConfigPath = %q, want %q", store.ConfigPath(), path)
	}
	if !strings.HasPrefix(store.ConfigPath(), filepath.Dir(path)) {
		t.Fatalf("the store read a file outside the directory this row planted one in: %s", store.ConfigPath())
	}
}

// TestDefaultConfigPathFollowsTheOverride pins the $DOCKER_CONFIG
// short-circuit, which is also what makes that variable a complete
// isolation gate for every suite in this repository.
func TestDefaultConfigPathFollowsTheOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)

	path, err := auth.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if want := filepath.Join(dir, "config.json"); path != want {
		t.Fatalf("DefaultConfigPath = %q, want %q", path, want)
	}
}

// TestDefaultConfigPathFallsBackToTheHomeDirectory pins where the
// configuration is looked for when no variable names a directory.
func TestDefaultConfigPathFallsBackToTheHomeDirectory(t *testing.T) {
	t.Setenv(configDirEnv, "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	path, err := auth.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if want := filepath.Join(home, ".docker", "config.json"); path != want {
		t.Fatalf("DefaultConfigPath = %q, want %q", path, want)
	}
	if filepath.Base(home) != homeDirName {
		t.Fatalf(
			"the fallback resolved against the real home directory rather than the one this suite installed: %s",
			home,
		)
	}
}

// The two rows below share the one directory on the test PATH and both write a
// program called docker-credential-fake into it, so neither runs in parallel:
// they take turns, which is what a top-level test that never calls
// [testing.T.Parallel] does.

func TestStoreCredentialRunsTheHelperTheConfigurationNames(t *testing.T) {
	record := writeHelper(t, "fake", fakeHelperScript)
	store := newStore(t, plantConfig(t, `{"credsStore":"fake"}`))

	got, err := store.Credential(t.Context(), fixtureRegistry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	want := auth.Credential{Username: helperUser, Password: helperSecret}
	if got != want {
		t.Fatalf("Credential = %+v, want %+v", got, want)
	}
	if ran := helperRan(t, record); ran != "get\n" {
		t.Fatalf("helper invocations = %q, want get", ran)
	}
}

// TestStoreCredentialDoesNotRunAHelperTheConfigurationDoesNotName is the
// twin of the positive control: same fake, same PATH, no credsStore — the
// helper must stay unexecuted.
func TestStoreCredentialDoesNotRunAHelperTheConfigurationDoesNotName(t *testing.T) {
	record := writeHelper(t, "fake", fakeHelperScript)
	config := authsConfig(fixtureRegistry, fmt.Sprintf(`{"auth":%q}`, basicAuth("alice", "s3cret")))
	store := newStore(t, plantConfig(t, config))

	got, err := store.Credential(noExec(t), fixtureRegistry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	want := auth.Credential{Username: "alice", Password: "s3cret"}
	if got != want {
		t.Fatalf("Credential = %+v, want %+v", got, want)
	}
	if ran := helperRan(t, record); ran != "" {
		t.Fatalf("a helper sitting on PATH was run without the configuration naming it: %q", ran)
	}
}

// TestStoreCredentialDoesNotRunThePlatformsOwnHelper pins the
// DetectDefaultNativeStore choice: an empty configuration must not fall
// through to the platform keychain helper, which would read the
// developer's real credentials from a test.
func TestStoreCredentialDoesNotRunThePlatformsOwnHelper(t *testing.T) {
	records := make(map[string]string, len(platformHelpers()))
	for _, name := range platformHelpers() {
		records[name] = writeHelper(t, name, fakeHelperScript)
	}

	store := newStore(t, plantConfig(t, `{}`))

	got, err := store.Credential(noExec(t), fixtureRegistry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if !got.Empty() {
		t.Fatalf("an empty configuration must stay anonymous, got %+v", got)
	}

	for name, record := range records {
		if ran := helperRan(t, record); ran != "" {
			t.Fatalf("an empty configuration reached the platform's own credential store through %s: %q", name, ran)
		}
	}
}

// TestStoreCredentialGivesUpOnAHelperThatNeverAnswers pins the lookup
// bound: a wedged helper costs one bounded wait, never a hung transfer.
func TestStoreCredentialGivesUpOnAHelperThatNeverAnswers(t *testing.T) {
	t.Parallel()

	record := writeHelper(t, "wedged", wedgedHelperScript)
	store := newStore(t, plantConfig(t, `{"credsStore":"wedged"}`))

	started := time.Now()
	got, err := store.Credential(t.Context(), fixtureRegistry)
	took := time.Since(started)

	if err == nil {
		t.Fatal("a wedged helper must fail the lookup")
	}
	if !got.Empty() {
		t.Fatalf("a timed-out lookup must not return a credential: %+v", got)
	}
	if helperRan(t, record) == "" {
		t.Fatal("the helper never ran, so this row proved nothing about waiting for one")
	}
	if took >= wedgedHelperBudget {
		t.Fatalf("the lookup waited %s for the helper instead of bounding it", took)
	}
	if !strings.Contains(err.Error(), "10s") {
		t.Fatalf("the failure has to name the limit it ran out of: %v", err)
	}
	if !strings.Contains(err.Error(), fixtureRegistry) {
		t.Fatalf("the failure has to name the registry it was looking up: %v", err)
	}
}

// TestStoreCredentialCancelsAWedgedHelper pins that cancelling the caller
// context stops a helper that has already started.
func TestStoreCredentialCancelsAWedgedHelper(t *testing.T) {
	t.Parallel()

	record := writeHelper(t, "wedged-cancel", wedgedHelperScript)
	store := newStore(t, plantConfig(t, `{"credsStore":"wedged-cancel"}`))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	started := time.Now()
	errc := make(chan error, 1)
	go func() {
		_, err := store.Credential(ctx, fixtureRegistry)
		errc <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for helperRan(t, record) == "" {
		if time.Now().After(deadline) {
			t.Fatal("the helper never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	err := <-errc
	took := time.Since(started)
	if err == nil {
		t.Fatal("cancelling a wedged helper must fail the lookup")
	}
	if took >= wedgedHelperBudget {
		t.Fatalf("the cancelled lookup waited %s instead of returning", took)
	}
}

// noExec returns a context that fails the test if a credential helper runs
// under it.
//
// It is what turns "this row did not need a helper" into something proved
// rather than assumed. Every row that reads a configuration file and nothing
// else carries it, so a change that made an ordinary lookup shell out —
// through a platform default, a fallback, a helper named somewhere
// unexpected — fails here instead of quietly reaching a developer's keychain
// on their machine and nothing on the build's.
func noExec(t *testing.T) context.Context {
	t.Helper()

	return trace.WithExecutableTrace(t.Context(), &trace.ExecutableTrace{
		ExecuteStart: func(program, action string) {
			t.Fatalf("the lookup ran %s (%s); this row must not run a program", program, action)
		},
	})
}

// assertUnsupportedTokenRefused requires that the token stored at path is
// refused as [auth.ErrAuth], returns no credential, and names the registry
// and file without repeating secret.
func assertUnsupportedTokenRefused(t *testing.T, path, secret string) {
	t.Helper()

	got, err := newStore(t, path).Credential(noExec(t), fixtureRegistry)
	if err == nil {
		t.Fatal("a stored token must be refused, not treated as anonymous")
	}
	if !got.Empty() {
		t.Fatalf("a refused token must not return a credential: %+v", got)
	}
	if !errors.Is(err, auth.ErrAuth) {
		t.Fatalf("token error = %v, want ErrAuth", err)
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("the message must say what was refused: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the token stays out of the message: %v", err)
	}
	if !strings.Contains(err.Error(), fixtureRegistry) {
		t.Fatalf("the message names the registry: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the message names the file: %v", err)
	}
}

// newStore returns a store reading the configuration at path.
func newStore(t *testing.T, path string) *auth.Store {
	t.Helper()

	store, err := auth.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	return store
}

// plantConfig writes config into a directory of this test's own and returns
// the path, pinned by [pin] so nothing the row does can change it.
func plantConfig(t *testing.T, config string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(config), configPerm); err != nil {
		t.Fatal(err)
	}

	pin(t, path)

	return path
}

// pin records what the configuration file holds and asserts, once the test has
// finished, that it holds exactly the same thing.
//
// The port this package looks credentials up through has one read method, so
// a write is unrepresentable rather than merely unlikely — but the store
// underneath it can write, and this is what keeps the difference between
// those two statements honest. A file that was not there has to still not be
// there.
func pin(t *testing.T, path string) {
	t.Helper()

	before, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)

	if !missing && err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		after, err := os.ReadFile(path)
		if missing {
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the lookup created a Docker configuration: %v", err)
			}

			return
		}

		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Errorf("the lookup wrote to the Docker configuration")
		}
	})
}

// writeHelper writes a credential helper program called
// docker-credential-<name> into the one directory on the test PATH, and
// returns the file that program records its invocations in.
//
// The program is a shell script, which is why these rows do not run on
// Windows. What they prove — that a helper runs when the configuration names
// one and never otherwise — is about the store's own decision rather than
// about how a program is spelled on a platform.
func writeHelper(t *testing.T, name, script string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the credential helper rows run a shell script")
	}

	record := filepath.Join(t.TempDir(), "invocations")
	program := filepath.Join(os.Getenv(pathEnv), "docker-credential-"+name)

	if err := os.WriteFile(program, fmt.Appendf(nil, script, record), helperPerm); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(program); err != nil {
			t.Errorf("remove helper %s: %v", program, err)
		}
	})

	return record
}

// helperRan returns what the helper at record wrote down, and the empty string
// when it never ran at all.
func helperRan(t *testing.T, record string) string {
	t.Helper()

	ran, err := os.ReadFile(record)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}

	return string(ran)
}

// platformHelpers names the credential helpers a store would reach for if it
// detected the platform's own, which is the fallback this package turns off.
// Linux picks between two depending on what is installed, so both are planted.
func platformHelpers() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"osxkeychain"}
	case "windows":
		return []string{"wincred"}
	default:
		return []string{"pass", "secretservice"}
	}
}

// authsConfig renders a Docker configuration holding one auths entry: key is
// the server address it is filed under and entry is the JSON object stored
// there.
func authsConfig(key, entry string) string {
	return fmt.Sprintf(`{"auths":{%q:%s}}`, key, entry)
}

// basicAuth renders the base64 "user:secret" that a configuration entry's auth
// field carries, which is how `docker login` stores a password.
func basicAuth(username, secret string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + secret))
}
