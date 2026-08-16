//go:build e2e

package imgoci

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// dockerConfigEnv is the directory override [WithDockerCredentials] reads.
const dockerConfigEnv = "DOCKER_CONFIG"

// dockerConfigName is the configuration file inside that directory.
const dockerConfigName = "config.json"

// TestE2EDockerCredentialsAuthenticatesFromATemporaryConfig proves the
// opt-in Docker store is what authenticates a transfer: the same htpasswd
// registry that refuses an anonymous client serves a fetch when the
// credential is filed under the dialed host, and refuses it when the same
// credential is filed under a host this transfer never dials.
//
// It runs sequentially: DOCKER_CONFIG belongs to the process, not to the test.
func TestE2EDockerCredentialsAuthenticatesFromATemporaryConfig(t *testing.T) {
	host := startHtpasswdRegistry(t)
	repo := testRepo(t)
	cred := e2eCreds{user: e2eUser, pass: e2ePass}
	seeded := seedCanonicalRelease(t, host, repo, cred)
	ref := tagRef(host, repo)

	t.Run("the matching host authenticates", func(t *testing.T) {
		useDockerConfig(t, host, e2ePass)
		client, err := New(WithPlainHTTP(), WithDockerCredentials())
		if err != nil {
			t.Fatal(err)
		}
		rel := mustFetch(t, client, ref)
		sel := resolveQEMU(t, client, rel)
		dir := t.TempDir()
		mustFetchFiles(t, client, rel, sel, ToDir(dir))
		assertFileContent(t, filepath.Join(dir, seeded.qemu.filename), seeded.qemu.content)
	})

	t.Run("a different host is not sent", func(t *testing.T) {
		useDockerConfig(t, "other.example:5000", e2ePass)
		client, err := New(WithPlainHTTP(), WithDockerCredentials())
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Fetch(t.Context(), ref)
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("credential stored for another host: err = %v, want ErrUnauthorized", err)
		}
	})
}

// useDockerConfig writes the Docker configuration a login to host as the
// registry's one account would have left behind, and points $DOCKER_CONFIG at
// the directory holding it for the length of the test.
//
// The file is the one `docker login` writes, in the format it writes it, which
// is the point of the row: what is being tested is that the client reads what
// a login left behind, not that it reads a shape invented here.
func useDockerConfig(t *testing.T, host, password string) {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			host: map[string]string{
				"auth": base64.StdEncoding.EncodeToString([]byte(e2eUser + ":" + password)),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dockerConfigName), body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dockerConfigEnv, dir)
}
