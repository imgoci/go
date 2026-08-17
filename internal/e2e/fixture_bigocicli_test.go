//go:build e2e

// External bigoci CLI interop fixtures: resolving, cloning, and running the
// upstream CLI so the interop tests exercise a real second implementation.

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// bigociCLIDir returns the bigoci CLI module directory used by interop
// tests. Resolution order:
//
//  1. IMGOCI_BIGOCI_CLI_DIR, when set.
//  2. ~/code/imgoci/bigoci/cli, when that checkout exists.
//  3. A shallow clone of github.com/imgoci/bigoci at the go.mod pin, into
//     t.TempDir() once per test run.
//
// IMGOCI_BIGOCI_FORCE_CLONE=1 skips (1) and (2) and always takes (3). A failed
// clone is fatal; this helper never skips.
func bigociCLIDir(t *testing.T) string {
	t.Helper()
	bigociCLIOnce.Do(func() {
		bigociCLIPath, bigociCLISource, bigociCLIFail = resolveBigociCLI(t)
	})
	if bigociCLIFail != "" {
		t.Fatalf("bigoci CLI: %s", bigociCLIFail)
	}
	t.Logf("bigoci CLI source: %s (%s)", bigociCLISource, bigociCLIPath)
	return bigociCLIPath
}

var (
	bigociCLIOnce   sync.Once
	bigociCLIPath   string
	bigociCLISource string
	bigociCLIFail   string
)

func resolveBigociCLI(t *testing.T) (dir, source, fail string) {
	t.Helper()
	if os.Getenv("IMGOCI_BIGOCI_FORCE_CLONE") == "1" {
		return cloneBigociCLI(t)
	}
	if override := os.Getenv("IMGOCI_BIGOCI_CLI_DIR"); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", "", "IMGOCI_BIGOCI_CLI_DIR " + override + ": " + err.Error()
		}
		return override, "IMGOCI_BIGOCI_CLI_DIR", ""
	}
	if local, version, ok := localBigociCLI(); ok {
		return local, "local sibling " + version, ""
	}
	return cloneBigociCLI(t)
}

func localBigociCLI() (dir, version string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	root := filepath.Join(home, "code", "imgoci", "bigoci")
	dir = filepath.Join(root, "cli")
	if _, err = os.Stat(dir); err != nil {
		return "", "", false
	}
	out, err := exec.Command("git", "-C", root, "describe", "--tags").Output()
	version = strings.TrimSpace(string(out))
	if err != nil || version == "" {
		version = "unknown"
	}
	return dir, version, true
}

func cloneBigociCLI(t *testing.T) (dir, source, fail string) {
	t.Helper()
	ver, fail := pinnedBigociVersion(t)
	if fail != "" {
		return "", "", fail
	}
	dest := filepath.Join(t.TempDir(), "bigoci")
	cmd := exec.CommandContext(t.Context(), "git", "clone", "--depth", "1", "--branch", ver,
		"https://github.com/imgoci/bigoci", dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", "git clone github.com/imgoci/bigoci@" + ver + ": " + err.Error() + "\n" + string(out)
	}
	dir = filepath.Join(dest, "cli")
	if _, err := os.Stat(dir); err != nil {
		return "", "", "cloned bigoci CLI directory " + dir + ": " + err.Error()
	}
	return dir, "clone " + ver, ""
}

func pinnedBigociVersion(t *testing.T) (string, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Version}}", "github.com/imgoci/bigoci")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "go list -m github.com/imgoci/bigoci: " + err.Error() + "\n" + string(out)
	}
	ver := strings.TrimSpace(string(out))
	if ver == "" {
		return "", "go list -m github.com/imgoci/bigoci returned an empty version"
	}
	return ver, ""
}

// runBigociCLI runs `go run .` in the bigoci CLI module with args and
// returns stdout. DOCKER_CONFIG points at an empty directory so the CLI's
// always-on Docker credential helper cannot pick up unrelated logins.
func runBigociCLI(t *testing.T, args ...string) string {
	t.Helper()
	stdout, _ := runBigociCLIOutput(t, args...)
	return strings.TrimSpace(stdout)
}

// runBigociCLIPull is [runBigociCLI] for pull, which writes nothing to stdout.
func runBigociCLIPull(t *testing.T, args ...string) {
	t.Helper()
	_, _ = runBigociCLIOutput(t, args...)
}

// runBigociCLIOutput executes the CLI and returns stdout and stderr.
func runBigociCLIOutput(t *testing.T, args ...string) (string, string) {
	t.Helper()
	cliDir := bigociCLIDir(t)
	dockerConfig := t.TempDir()
	cmdArgs := append([]string{"run", "."}, args...)
	cmd := exec.CommandContext(t.Context(), "go", cmdArgs...)
	cmd.Dir = cliDir
	cmd.Env = append(os.Environ(), "DOCKER_CONFIG="+dockerConfig)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bigoci CLI %v: %v\nstderr:\n%s\nstdout:\n%s", args, err, stderr.String(), stdout.String())
	}
	return stdout.String(), stderr.String()
}
