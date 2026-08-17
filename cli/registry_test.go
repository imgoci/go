//go:build e2e

package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// registryImage is the CNCF Distribution image the smoke test publishes to.
	registryImage = "registry:2"
	// registryPort is the distribution-spec listen port the image exposes.
	registryPort = "5000/tcp"
	// registryStartupTimeout is long enough for a first-time image pull.
	registryStartupTimeout = 3 * time.Minute
)

// TestRegistryPublishListResolveFetch drives the four commands against a real
// local registry and covers argument parsing, library calls, and the stream
// contract together.
func TestRegistryPublishListResolveFetch(t *testing.T) {
	t.Parallel()

	host := startLocalRegistry(t)
	dir := t.TempDir()
	payload := []byte("imgoci-cli-registry-smoke")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "disk.bin"), payload, 0o600))
	spec := filepath.Join(dir, "spec.json")
	require.NoError(t, os.WriteFile(spec, []byte(`{
  "name": "cli-smoke",
  "version": "1",
  "files": [{
    "path": "disk.bin",
    "filename": "disk.bin",
    "architecture": "amd64",
    "target": "qemu",
    "representation": "qcow2",
    "role": "disk",
    "compression": "none"
  }]
}`), 0o600))

	ref := host + "/cli/smoke:v1"
	dest := filepath.Join(dir, "out")
	require.NoError(t, os.Mkdir(dest, 0o700))

	published := runCLI(t, "publish", "-plain-http", spec, ref)
	assert.Equal(t, exitOK, published.code, published.stderr)
	digest := strings.TrimSpace(published.stdout)
	assert.True(t, strings.HasPrefix(digest, "sha256:"), published.stdout)
	assert.Equal(t, digest+"\n", published.stdout)
	assert.NotContains(t, published.stdout, "imgoci:")
	assert.Contains(t, published.stderr, "imgoci: publish ")

	listed := runCLI(t, "list", "-plain-http", "-architecture", "amd64", ref)
	assert.Equal(t, exitOK, listed.code, listed.stderr)
	assert.Equal(t, "amd64\tqemu\tqcow2\t\tdisk\tnone\tapplication/vnd.imgoci.file.v1\n", listed.stdout)
	assert.NotContains(t, listed.stdout, "imgoci:")
	assert.Contains(t, listed.stderr, "imgoci: list ")

	resolved := runCLI(
		t, "resolve", "-plain-http",
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-compression", "none",
		ref,
	)
	assert.Equal(t, exitOK, resolved.code, resolved.stderr)
	assert.True(
		t,
		strings.HasPrefix(
			resolved.stdout,
			"amd64\tqemu\tqcow2\t\tdisk\tnone\tdisk.bin\tapplication/vnd.imgoci.file.v1\tsha256:",
		),
	)
	assert.Contains(t, resolved.stdout, "\t25\n")
	assert.NotContains(t, resolved.stdout, "imgoci:")
	assert.Contains(t, resolved.stderr, "imgoci: resolve ")

	fetched := runCLI(
		t, "fetch", "-plain-http",
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-compression", "none",
		ref, dest,
	)
	assert.Equal(t, exitOK, fetched.code, fetched.stderr)
	assert.Empty(t, fetched.stdout)
	assert.Contains(t, fetched.stderr, "imgoci: fetch ")
	got, err := os.ReadFile(filepath.Join(dest, "disk.bin"))
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestRegistryUsageRoundTrip(t *testing.T) {
	t.Parallel()

	host := startLocalRegistry(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.bin"), []byte("empty-usage"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install.bin"), []byte("install-offline-set"), 0o600))
	spec := filepath.Join(dir, "spec.json")
	require.NoError(t, os.WriteFile(spec, []byte(`{
  "name": "cli-usage",
  "version": "1",
  "files": [
    {
      "path": "empty.bin",
      "filename": "empty.bin",
      "architecture": "amd64",
      "target": "qemu",
      "representation": "qcow2",
      "role": "disk",
      "compression": "none"
    },
    {
      "path": "install.bin",
      "filename": "install.bin",
      "architecture": "amd64",
      "target": "qemu",
      "representation": "qcow2",
      "usage": ["install-offline", "install"],
      "role": "disk",
      "compression": "none"
    }
  ]
}`), 0o600))

	ref := host + "/cli/usage:v1"
	published := runCLI(t, "publish", "-plain-http", spec, ref)
	assert.Equal(t, exitOK, published.code, published.stderr)

	listed := runCLI(t, "list", "-plain-http", ref)
	assert.Equal(t, exitOK, listed.code, listed.stderr)
	assert.Equal(t, ""+
		"amd64\tqemu\tqcow2\t\tdisk\tnone\tapplication/vnd.imgoci.file.v1\n"+
		"amd64\tqemu\tqcow2\tinstall,install-offline\tdisk\tnone\tapplication/vnd.imgoci.file.v1\n",
		listed.stdout,
	)

	empty := runCLI(
		t, "resolve", "-plain-http",
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-compression", "none",
		ref,
	)
	assert.Equal(t, exitOK, empty.code, empty.stderr)
	assert.True(t, strings.HasPrefix(empty.stdout, "amd64\tqemu\tqcow2\t\tdisk\tnone\tempty.bin\t"), empty.stdout)

	compound := runCLI(
		t, "resolve", "-plain-http",
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-usage", "install",
		"-usage", "install-offline",
		"-compression", "none",
		ref,
	)
	assert.Equal(t, exitOK, compound.code, compound.stderr)
	assert.True(
		t,
		strings.HasPrefix(compound.stdout, "amd64\tqemu\tqcow2\tinstall,install-offline\tdisk\tnone\tinstall.bin\t"),
		compound.stdout,
	)

	subset := runCLI(
		t, "resolve", "-plain-http",
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-usage", "install",
		"-compression", "none",
		ref,
	)
	assert.Equal(t, exitFailure, subset.code, subset.stderr)
	assert.Contains(t, subset.stderr, `usage="install"`)
}

// startLocalRegistry launches registry:2 and returns host:port for plain HTTP.
func startLocalRegistry(t *testing.T) string {
	t.Helper()

	ctx := t.Context()
	container, err := testcontainers.Run(ctx, registryImage,
		testcontainers.WithExposedPorts(registryPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/v2/").WithPort(registryPort).
				WithStartupTimeout(registryStartupTimeout).
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }),
		),
	)
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, registryPort)
	require.NoError(t, err)

	return net.JoinHostPort(host, port.Port())
}
