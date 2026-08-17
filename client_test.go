package imgoci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/decomp"
)

func TestNewOptionSealing(t *testing.T) {
	t.Parallel()

	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.settings.plainHTTP {
		t.Fatal("default scheme is https")
	}
	if c.settings.httpClient != nil {
		t.Fatal("default HTTP client is unset")
	}
	if c.settings.allowUnverifiedExternal {
		t.Fatal("default transport is verified")
	}
	if c.settings.resolved != nil {
		t.Fatal("default credentials are anonymous")
	}

	c, err = New(WithPlainHTTP())
	if err != nil {
		t.Fatal(err)
	}
	if !c.settings.plainHTTP {
		t.Fatal("WithPlainHTTP must set plain HTTP")
	}

	c, err = New(WithHTTPClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.settings.httpClient != nil {
		t.Fatal("nil HTTP client must be ignored")
	}

	hc := &http.Client{}
	c, err = New(WithHTTPClient(hc), WithUnverifiedExternalTransport(), WithCredentials("user", "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if c.settings.httpClient != hc {
		t.Fatal("WithHTTPClient must keep the given client")
	}
	if !c.settings.allowUnverifiedExternal {
		t.Fatal("WithUnverifiedExternalTransport must set the escape hatch")
	}
	if c.settings.resolved == nil {
		t.Fatal("WithCredentials must install a static source")
	}
	got, err := c.settings.resolved.Credential(t.Context(), "registry.example")
	if err != nil {
		t.Fatal(err)
	}
	want := auth.Credential{Username: "user", Password: "secret"}
	if got != want {
		t.Fatalf("static credential = %+v, want %+v", got, want)
	}
}

func TestClientCapabilitiesIncludeBigOCI(t *testing.T) {
	t.Parallel()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	caps := c.Capabilities()
	if !caps.supports(standardFileMediaType) {
		t.Fatal("standard file type must be supported")
	}
	if !caps.supports(bigociFileMediaType) {
		t.Fatal("Capabilities must include the BigOCI file type")
	}
}

func TestNewIgnoresNilOption(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err != nil {
		t.Fatal(err)
	}
}

// TestNewDecoderMaxWindow pins the one knob over the decoder working set: a
// default that admits mainstream producer output, an override kept verbatim in
// either direction, and zero refused at construction rather than silently
// treated as "no limit".
func TestNewDecoderMaxWindow(t *testing.T) {
	t.Parallel()

	if decomp.DefaultDecoderMaxWindow != 128<<20 {
		t.Fatalf("DefaultDecoderMaxWindow = %d, want 128 MiB", decomp.DefaultDecoderMaxWindow)
	}

	tests := []struct {
		name    string
		opts    []Option
		want    uint64
		wantErr bool
	}{
		{
			name: "unset is the package default",
			want: decomp.DefaultDecoderMaxWindow,
		},
		{
			name: "a lowered ceiling is kept verbatim",
			opts: []Option{WithDecoderMaxWindow(8 << 20)},
			want: 8 << 20,
		},
		{
			name: "a raised ceiling is kept verbatim",
			opts: []Option{WithDecoderMaxWindow(1 << 30)},
			want: 1 << 30,
		},
		{
			name: "the last ceiling named wins",
			opts: []Option{WithDecoderMaxWindow(1 << 30), WithDecoderMaxWindow(16 << 20)},
			want: 16 << 20,
		},
		{
			name:    "zero is rejected",
			opts:    []Option{WithDecoderMaxWindow(0)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := New(tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("New must reject a zero decoder window")
				}
				if c != nil {
					t.Fatal("New must not return a client with an error")
				}
				if !strings.Contains(err.Error(), "decoder max window") {
					t.Fatalf("error %q does not name the decoder max window", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.settings.decoderMaxWindow != tt.want {
				t.Fatalf("decoderMaxWindow = %d, want %d", c.settings.decoderMaxWindow, tt.want)
			}
		})
	}
}

// TestClientDecoderMaxWindowRejectsRealToolFixtures is the option's end-to-end
// proof, on the stored files the reference compressors actually produce (the
// committed decomp fixtures, whose provenance is recorded in
// internal/decomp/testdata/README.md).
//
// A client held at the 8 MiB this library used to hardcode refuses both of
// them through the public surface, wrapping [ErrDecode], and refuses them in
// pass 1 before a byte reaches the registry. The same files under the default
// publish, which is what makes the rejection the configuration's rather than
// the file's.
func TestClientDecoderMaxWindowRejectsRealToolFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		compression string
	}{
		{name: "xz -9", path: "internal/decomp/testdata/xz-9.xz", compression: "xz"},
		{name: "zstd --long=27", path: "internal/decomp/testdata/zstd-long-27.zst", compression: "zstd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stored, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			spec := fixtureReleaseSpec(t, stored, tt.compression)

			lowered := clientWithDecoderMaxWindow(t, 8<<20)
			_, err = lowered.Publish(t.Context(), "example.com/os/example:v1", spec)
			if !errors.Is(err, ErrDecode) {
				t.Fatalf("error %v is not ErrDecode", err)
			}

			atDefault := clientWithDecoderMaxWindow(t, decomp.DefaultDecoderMaxWindow)
			if _, err := atDefault.Publish(t.Context(), "example.com/os/example:v1", spec); err != nil {
				t.Fatalf("publish at the default ceiling: %v", err)
			}
		})
	}
}

// clientWithDecoderMaxWindow builds a client at maxWindow whose repository
// ports are the publish stubs, so no network is involved.
func clientWithDecoderMaxWindow(t *testing.T, maxWindow uint64) *Client {
	t.Helper()
	c, err := New(WithDecoderMaxWindow(maxWindow))
	if err != nil {
		t.Fatal(err)
	}
	manifests := &publishManifests{}
	blobs := &publishBlobs{}
	c.newAdapter = func(context.Context, string, string, clientSettings) (adapterPorts, error) {
		return adapterPorts{manifests: manifests, blobs: blobs}, nil
	}
	return c
}

// fixtureReleaseSpec publishes one stored file of the named compression.
func fixtureReleaseSpec(t *testing.T, stored []byte, compression string) ReleaseSpec {
	t.Helper()
	return ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{{
			Source: FromFile(writePublishFile(t, "fixture.bin", stored)),
			Selector: Selector{
				Architecture:   "amd64",
				Target:         "x-test-target",
				Representation: "x-test-format",
				Role:           "x-test-file",
				Compression:    compression,
			},
			Filename: "a",
		}},
	}
}

// TestNewReadsTheDockerConfiguration pins that [WithDockerCredentials] is
// applied at [New]: a missing config is anonymous, a malformed config fails
// construction, and without the option DOCKER_CONFIG is not read.
//
// Sequential: DOCKER_CONFIG belongs to the process, not to the test.
func TestNewReadsTheDockerConfiguration(t *testing.T) {
	t.Run("a directory holding no configuration is the anonymous machine", func(t *testing.T) {
		assertMissingDockerConfigIsAnonymous(t)
	})

	t.Run("a configuration that cannot be read fails while the client is built", func(t *testing.T) {
		assertMalformedDockerConfigFailsNew(t)
	})

	t.Run("without the option no configuration is read", func(t *testing.T) {
		assertUnusedDockerConfigIsIgnored(t)
	})
}

// TestNewWithDockerCredentialsOnAMachineWithNoHome pins that a scratch
// container with no $DOCKER_CONFIG and no home is anonymous, not an error: a
// public pull must not need a home.
func TestNewWithDockerCredentialsOnAMachineWithNoHome(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	client, err := New(WithDockerCredentials())
	if err != nil {
		t.Fatalf("a machine that cannot name its configuration has none, and none is not an error: %v", err)
	}
	if client == nil {
		t.Fatal("New must return a client")
	}
	if client.settings.resolved != nil {
		t.Fatal("an unlocatable configuration must stay anonymous")
	}
}

// TestLastCredentialOptionWins pins that naming both credential options
// leaves the last one named in effect.
func TestLastCredentialOptionWins(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	t.Run("static after docker keeps the static secret", func(t *testing.T) {
		client, err := New(WithDockerCredentials(), WithCredentials("user", "secret"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := client.settings.resolved.Credential(t.Context(), "registry.example")
		if err != nil {
			t.Fatal(err)
		}
		want := auth.Credential{Username: "user", Password: "secret"}
		if got != want {
			t.Fatalf("last option = %+v, want static %+v", got, want)
		}
	})

	t.Run("docker after static reads the empty config", func(t *testing.T) {
		client, err := New(WithCredentials("user", "secret"), WithDockerCredentials())
		if err != nil {
			t.Fatal(err)
		}
		got, err := client.settings.resolved.Credential(t.Context(), "registry.example")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Empty() {
			t.Fatalf("last option must be the empty Docker store, got %+v", got)
		}
	})
}

// TestFetchCancelledContextReachesCredentialResolution pins that adapter
// construction looks credentials up under the operation's caller context, so
// a cancelled transfer interrupts a helper instead of waiting out the lookup
// cap.
func TestFetchCancelledContextReachesCredentialResolution(t *testing.T) {
	t.Parallel()

	client, err := New()
	if err != nil {
		t.Fatal(err)
	}
	client.settings.resolved = blockingCredentials{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	started := time.Now()
	_, err = client.Fetch(ctx, "registry.example/os/example:v1")
	took := time.Since(started)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled construction = %v, want context.Canceled", err)
	}
	if took >= time.Second {
		t.Fatalf("cancelled construction waited %s instead of returning", took)
	}
}

// TestFetchUnsupportedStoredTokenIsUnauthorized pins that a stored token
// this client cannot present fails adapter construction as [ErrUnauthorized],
// before any transfer error mapper runs.
func TestFetchUnsupportedStoredTokenIsUnauthorized(t *testing.T) {
	t.Parallel()

	store := storeWithAuths(t, `{"identitytoken":"refresh-me-SUPERSECRET"}`)
	client, err := New()
	if err != nil {
		t.Fatal(err)
	}
	client.settings.resolved = store

	_, err = client.Fetch(t.Context(), "registry.example/os/example:v1")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unsupported stored token = %v, want ErrUnauthorized", err)
	}
	if !errors.Is(err, auth.ErrAuth) {
		t.Fatalf("unsupported stored token = %v, want wrapped auth.ErrAuth", err)
	}
	if strings.Contains(err.Error(), "refresh-me-SUPERSECRET") {
		t.Fatalf("the token stays out of the message: %v", err)
	}
}

// TestPortsForStaticCredentialsStayNonblocking pins that a static credential
// lookup cannot block construction, even when the caller context is already
// cancelled.
func TestPortsForStaticCredentialsStayNonblocking(t *testing.T) {
	t.Parallel()

	client, err := New(WithCredentials("user", "secret"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	started := time.Now()
	_, err = client.portsFor(ctx, "registry.example", "os/example")
	took := time.Since(started)
	if err != nil {
		t.Fatalf("static credentials must not fail construction: %v", err)
	}
	if took >= time.Second {
		t.Fatalf("static credential construction waited %s", took)
	}
}

// blockingCredentials waits for the lookup context to end, then returns that
// cancellation, so cancelled adapter construction proves the caller context
// reached the credential source.
type blockingCredentials struct{}

func (blockingCredentials) Credential(ctx context.Context, _ string) (auth.Credential, error) {
	<-ctx.Done()
	return auth.Credential{}, ctx.Err()
}

// storeWithAuths returns a Docker credential store whose only entry is for
// registry.example and holds the given JSON object.
func storeWithAuths(t *testing.T, entry string) *auth.Store {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	config := fmt.Sprintf(`{"auths":{"registry.example":%s}}`, entry)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := auth.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// assertMissingDockerConfigIsAnonymous requires that WithDockerCredentials on a
// directory holding no configuration still builds a client whose store resolves
// empty credentials.
func assertMissingDockerConfigIsAnonymous(t *testing.T) {
	t.Helper()

	t.Setenv("DOCKER_CONFIG", t.TempDir())

	client, err := New(WithDockerCredentials())
	if err != nil {
		t.Fatalf("missing config must stay anonymous: %v", err)
	}
	if client == nil {
		t.Fatal("New must return a client")
	}
	if client.settings.resolved == nil {
		t.Fatal("a missing config still installs a store that answers anonymously")
	}
	got, err := client.settings.resolved.Credential(t.Context(), "registry.example")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Empty() {
		t.Fatalf("missing config must resolve anonymously, got %+v", got)
	}
}

// assertMalformedDockerConfigFailsNew requires that WithDockerCredentials
// reports a configuration that cannot be read while the client is built,
// naming the directory that held it.
func assertMalformedDockerConfigFailsNew(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dir)

	_, err := New(WithDockerCredentials())
	if err == nil {
		t.Fatal("a malformed configuration must be reported before any transfer starts")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("the failure must name the file it could not read: %v", err)
	}
}

// assertUnusedDockerConfigIsIgnored requires that New without
// WithDockerCredentials does not read DOCKER_CONFIG, even when the file
// there is not a configuration.
func assertUnusedDockerConfigIsIgnored(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dir)

	client, err := New()
	if err != nil {
		t.Fatalf("a broken unused config must not fail New: %v", err)
	}
	if client.settings.resolved != nil {
		t.Fatal("New without WithDockerCredentials must stay anonymous")
	}
}
