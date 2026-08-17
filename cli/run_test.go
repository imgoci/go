package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	imgoci "github.com/imgoci/go"
)

// TestMain points DOCKER_CONFIG at an empty directory so WithDockerCredentials
// stays anonymous for every in-process run.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "imgoci-docker-config-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.Setenv("DOCKER_CONFIG", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// result is what one in-process run produced: the exit code and both streams,
// byte for byte as the real program would have written them.
type result struct {
	// code is the exit code run returned.
	code int
	// stdout is everything the run wrote to standard output.
	stdout string
	// stderr is everything the run wrote to standard error.
	stderr string
}

// runCLI runs one command line in process. Nothing here touches a registry:
// every case is answered by argument parsing or by the library's own validation.
func runCLI(t *testing.T, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := run(t.Context(), env{args: args, stdout: &stdout, stderr: &stderr}, nil)

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// interruptedBy returns an interrupt record that has already seen s.
func interruptedBy(s os.Signal) *interrupts {
	sig := &interrupts{}
	sig.record(s)

	return sig
}

// twoLayers wraps err the way a real failure arrives.
func twoLayers(err error) error {
	return fmt.Errorf("publish spec.json to reg/repo:v1: %w", fmt.Errorf("file disk: %w", err))
}

func TestRunHelpGoesToStdout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		wants []string
	}{
		{name: "help", args: []string{"help"}, wants: []string{"usage:", "imgoci help"}},
		{name: "short flag", args: []string{"-h"}, wants: []string{"usage:"}},
		{name: "long flag", args: []string{"--help"}, wants: []string{"usage:"}},
		{
			name:  "help for publish",
			args:  []string{"help", "publish"},
			wants: []string{"usage: imgoci publish", "-workers", "-plain-http", "-timeout", "-progress"},
		},
		{
			name:  "help for list",
			args:  []string{"help", "list"},
			wants: []string{"usage: imgoci list", "-architecture", "-role", "-plain-http"},
		},
		{
			name:  "help for resolve",
			args:  []string{"help", "resolve"},
			wants: []string{"usage: imgoci resolve", "-compression", "-capability"},
		},
		{
			name:  "help for fetch",
			args:  []string{"help", "fetch"},
			wants: []string{"usage: imgoci fetch", "-workers", "-progress", "-compression"},
		},
		{
			name:  "help asked for by flag on a command",
			args:  []string{"publish", "-h"},
			wants: []string{"usage: imgoci publish"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := runCLI(t, tt.args...)
			assert.Equal(t, exitOK, got.code)
			assert.Empty(t, got.stderr)
			for _, want := range tt.wants {
				assert.Contains(t, got.stdout, want)
			}
		})
	}
}

func TestRunVersionGoesToStdout(t *testing.T) {
	t.Parallel()

	got := runCLI(t, "version")
	assert.Equal(t, exitOK, got.code)
	assert.Empty(t, got.stderr)
	assert.Equal(t, versionLine, got.stdout)
}

func TestRunHelpForListOmitsTransferFlags(t *testing.T) {
	t.Parallel()

	got := runCLI(t, "help", "list")
	assert.Equal(t, exitOK, got.code)
	assert.NotContains(t, got.stdout, "-workers")
	assert.NotContains(t, got.stdout, "-progress")
	assert.NotContains(t, got.stdout, "-compression")
}

func TestRunHelpDistinguishesUsageMatching(t *testing.T) {
	t.Parallel()

	list := runCLI(t, "help", "list")
	assert.Equal(t, exitOK, list.code)
	assert.Contains(t, list.stdout, "-usage")
	assert.Contains(t, list.stdout, "match every usage set")
	assert.Contains(t, list.stdout, "<representation>\t<usage>\t<role>")
	assert.NotContains(t, list.stdout, "empty usage set")

	resolve := runCLI(t, "help", "resolve")
	assert.Equal(t, exitOK, resolve.code)
	assert.Contains(t, resolve.stdout, "-usage")
	assert.Contains(t, resolve.stdout, "complete exact usage set")
	assert.Contains(t, resolve.stdout, "empty usage set")
	assert.Contains(t, resolve.stdout, "<representation>\t<usage>\t<role>")
	assert.Contains(t, resolve.stdout, "Unset -usage selects the")
	assert.NotContains(t, resolve.stdout, "match every usage set")

	fetch := runCLI(t, "help", "fetch")
	assert.Equal(t, exitOK, fetch.code)
	assert.Contains(t, fetch.stdout, "complete exact usage set")
	assert.Contains(t, fetch.stdout, "empty usage set")
	assert.Contains(t, fetch.stdout, "Unset -usage selects the")
	assert.NotContains(t, fetch.stdout, "match every usage set")
}

func TestQueryFlagsParseRepeatedUsage(t *testing.T) {
	t.Parallel()

	var list queryFlags
	listFS := newFlagSet(cmdList)
	list.registerList(listFS)
	require.NoError(t, listFS.Parse([]string{"-usage", "install", "-usage", "install-offline"}))
	assert.Equal(t, []string{"install", "install-offline"}, list.listQuery().Usage)

	var unsetList queryFlags
	unsetListFS := newFlagSet(cmdList)
	unsetList.registerList(unsetListFS)
	require.NoError(t, unsetListFS.Parse(nil))
	assert.Nil(t, unsetList.listQuery().Usage)

	var resolve queryFlags
	resolveFS := newFlagSet(cmdResolve)
	resolve.registerResolve(resolveFS)
	require.NoError(t, resolveFS.Parse([]string{
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-compression", "none",
		"-usage", "install-offline",
		"-usage", "install",
	}))
	got, err := resolve.resolveQuery()
	require.NoError(t, err)
	assert.Equal(t, []string{"install-offline", "install"}, got.Usage)

	var unsetResolve queryFlags
	unsetResolveFS := newFlagSet(cmdResolve)
	unsetResolve.registerResolve(unsetResolveFS)
	require.NoError(t, unsetResolveFS.Parse([]string{
		"-architecture", "amd64",
		"-target", "qemu",
		"-representation", "qcow2",
		"-compression", "none",
	}))
	got, err = unsetResolve.resolveQuery()
	require.NoError(t, err)
	assert.Nil(t, got.Usage)
}

func TestRunUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		wants []string
	}{
		{name: "no command", args: nil, wants: []string{`no command given; run "imgoci help" for the commands`}},
		{name: "unknown command", args: []string{"shove"}, wants: []string{`unknown command "shove"`}},
		{name: "unknown command for help", args: []string{"help", "shove"}, wants: []string{`unknown command "shove"`}},
		{name: "too much for help", args: []string{"help", "publish", "list"}, wants: []string{"at most one command"}},
		{name: "publish with no operands", args: []string{"publish"}, wants: []string{"exactly 2 operands", "got 0"}},
		{name: "list with no operands", args: []string{"list"}, wants: []string{"exactly one operand", "got 0"}},
		{name: "empty operand", args: []string{"list", ""}, wants: []string{"operand 1 is empty"}},
		{
			name:  "flag after the operands",
			args:  []string{"list", "reg/repo:v1", "-plain-http"},
			wants: []string{"flags must come before the operands", `move "-plain-http" before "reg/repo:v1"`},
		},
		{
			name: "resolve missing architecture",
			args: []string{
				"resolve",
				"-target",
				"qemu",
				"-representation",
				"qcow2",
				"-compression",
				"none",
				"reg/repo:v1",
			},
			wants: []string{"resolve requires -architecture"},
		},
		{
			name: "resolve missing compression",
			args: []string{
				"resolve",
				"-architecture",
				"amd64",
				"-target",
				"qemu",
				"-representation",
				"qcow2",
				"reg/repo:v1",
			},
			wants: []string{"at least one -compression"},
		},
		{
			name: "fetch missing target",
			args: []string{
				"fetch",
				"-architecture",
				"amd64",
				"-representation",
				"qcow2",
				"-compression",
				"none",
				"reg/repo:v1",
				"out",
			},
			wants: []string{"fetch requires -target"},
		},
		{
			name:  "negative timeout",
			args:  []string{"list", "-timeout", "-1s", "reg/repo:v1"},
			wants: []string{"list: -timeout must not be negative, got -1s"},
		},
		{
			name:  "unknown JSON member",
			args:  writeSpecArgs(t, `{"name":"n","version":"1","extra":true,"files":[]}`),
			wants: []string{"unknown field", "extra"},
		},
		{
			name: "publish spec missing name",
			args: writeSpecArgs(
				t,
				`{"version":"1","files":[{"path":"a","architecture":"amd64","target":"qemu","representation":"qcow2","role":"disk","compression":"none"}]}`,
			),
			wants: []string{"name is required"},
		},
		{
			name: "publish spec invalid usage names the file",
			args: writeSpecArgs(
				t,
				`{"name":"n","version":"1","files":[{"path":"a","filename":"a.img","architecture":"amd64","target":"qemu","representation":"qcow2","usage":["INSTALL"],"role":"disk","compression":"none"}]}`,
			),
			wants: []string{"files[0]", "INSTALL"},
		},
		{
			name: "publish spec install-offline without install names the file",
			args: writeSpecArgs(
				t,
				`{"name":"n","version":"1","files":[{"path":"a","filename":"a.img","architecture":"amd64","target":"qemu","representation":"qcow2","usage":["install-offline"],"role":"disk","compression":"none"}]}`,
			),
			wants: []string{"files[0]", "install-offline"},
		},
		{name: "version with an operand", args: []string{"version", "extra"}, wants: []string{"takes no operands"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := runCLI(t, tt.args...)
			assert.Equal(t, exitUsage, got.code)
			assert.Empty(t, got.stdout)
			assert.Contains(t, got.stderr, "usage:", "a usage error must print the usage block")
			for _, want := range tt.wants {
				assert.Contains(t, got.stderr, want)
			}
		})
	}
}

func TestRunMalformedReferenceIsAPlainFailure(t *testing.T) {
	t.Parallel()

	got := runCLI(t, "list", "NOT A REFERENCE")
	assert.Equal(t, exitFailure, got.code)
	assert.Empty(t, got.stdout)
	assert.Contains(t, got.stderr, "no sentinel matched")
}

func TestRunPublishDigestRefIsInvalidSpec(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	payload := filepath.Join(dir, "disk.bin")
	require.NoError(t, os.WriteFile(payload, []byte("payload"), 0o600))
	spec := filepath.Join(dir, "spec.json")
	require.NoError(t, os.WriteFile(spec, []byte(`{
  "name": "example",
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

	got := runCLI(
		t,
		"publish",
		spec,
		"example.com/os/example@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	assert.Equal(t, exitInvalidSpec, got.code)
	assert.Empty(t, got.stdout)
	assert.Contains(t, got.stderr, "imgoci.ErrInvalidSpec")
	assert.NotContains(t, got.stdout, "sha256:")
}

func TestReportErrorExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		sig  *interrupts
		code int
		want string
	}{
		{
			name: "not found",
			err:  twoLayers(imgoci.ErrNotFound),
			code: exitNotFound,
			want: "imgoci.ErrNotFound (exit 3)",
		},
		{
			name: "unauthorized",
			err:  twoLayers(imgoci.ErrUnauthorized),
			code: exitUnauthorized,
			want: "imgoci.ErrUnauthorized (exit 4)",
		},
		{
			name: "invalid index",
			err:  twoLayers(imgoci.ErrInvalidIndex),
			code: exitInvalidIndex,
			want: "imgoci.ErrInvalidIndex (exit 5)",
		},
		{
			name: "invalid spec",
			err:  twoLayers(imgoci.ErrInvalidSpec),
			code: exitInvalidSpec,
			want: "imgoci.ErrInvalidSpec (exit 6)",
		},
		{
			name: "invalid dest",
			err:  twoLayers(imgoci.ErrInvalidDest),
			code: exitInvalidDest,
			want: "imgoci.ErrInvalidDest (exit 7)",
		},
		{
			name: "digest mismatch",
			err:  twoLayers(imgoci.ErrDigestMismatch),
			code: exitDigestMismatch,
			want: "imgoci.ErrDigestMismatch (exit 8)",
		},
		{
			name: "unsupported type",
			err:  twoLayers(imgoci.ErrUnsupportedType),
			code: exitUnsupportedType,
			want: "imgoci.ErrUnsupportedType (exit 9)",
		},
		{
			name: "selection mismatch",
			err:  twoLayers(imgoci.ErrSelectionMismatch),
			code: exitSelectionMismatch,
			want: "imgoci.ErrSelectionMismatch (exit 10)",
		},
		{name: "decode", err: twoLayers(imgoci.ErrDecode), code: exitDecode, want: "imgoci.ErrDecode (exit 11)"},
		{
			name: "unclassified",
			err:  twoLayers(errors.New("connection reset")),
			code: exitFailure,
			want: "no sentinel matched (exit 1)",
		},
		{
			name: "signal outranks sentinel",
			err:  twoLayers(imgoci.ErrNotFound),
			sig:  interruptedBy(os.Interrupt),
			code: exitInterrupted,
			want: "interrupted by SIGINT (exit 130)",
		},
		{
			name: "sigterm",
			err:  twoLayers(errors.New("canceled")),
			sig:  interruptedBy(syscall.SIGTERM),
			code: exitTerminated,
			want: "terminated by SIGTERM (exit 143)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := reportError(env{stdout: &stdout, stderr: &stderr}, tt.err, tt.sig)
			assert.Equal(t, tt.code, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), tt.want)
			assert.True(t, strings.HasPrefix(stderr.String(), "imgoci: "))
		})
	}
}

func TestReportErrorEscapesControlRunes(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := reportError(env{stdout: &stdout, stderr: &stderr}, errors.New("bad\nline\x1b[31m"), nil)
	assert.Equal(t, exitFailure, code)
	assert.Empty(t, stdout.String())
	assert.NotContains(t, stderr.String(), "\nline")
	assert.Contains(t, stderr.String(), `\n`)
	assert.Contains(t, stderr.String(), `\x1b`)
}

func TestWithDeadlineUnsetAddsNoBound(t *testing.T) {
	t.Parallel()

	called := false
	err := withDeadline(context.Background(), 0, func(ctx context.Context) error {
		called = true
		_, hasDeadline := ctx.Deadline()
		assert.False(t, hasDeadline)

		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestMisplacedFlag(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, misplacedFlag([]string{"reg/repo:v1", "-plain-http"}))
	assert.Equal(t, -1, misplacedFlag([]string{"reg/repo:v1"}))
}

func TestTopUsageTeachesTheTerminator(t *testing.T) {
	t.Parallel()

	usage := topUsage()
	assert.Contains(t, usage, `"--"`)
	assert.Contains(t, usage, "begins with a dash")
}

// writeSpecArgs writes a publish spec into a temp dir and returns publish args.
func writeSpecArgs(t *testing.T, body string) []string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "spec.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return []string{"publish", path, "example.com/os/example:v1"}
}

func TestRunRejectsNonpositiveWorkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "zero workers on fetch",
			args: []string{"fetch", "-workers", "0", "reg/repo:v1", "out"},
			want: "fetch: -workers must be positive, got 0",
		},
		{
			name: "negative workers on fetch",
			args: []string{"fetch", "-workers", "-1", "reg/repo:v1", "out"},
			want: "fetch: -workers must be positive, got -1",
		},
		{
			name: "zero workers on publish",
			args: []string{"publish", "-workers", "0", "spec.json", "reg/repo:v1"},
			want: "publish: -workers must be positive, got 0",
		},
		{
			name: "negative workers on publish",
			args: []string{"publish", "-workers", "-1", "spec.json", "reg/repo:v1"},
			want: "publish: -workers must be positive, got -1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := runCLI(t, tt.args...)
			assert.Equal(t, exitUsage, got.code)
			assert.Empty(t, got.stdout)
			assert.Contains(t, got.stderr, tt.want)
			assert.Contains(t, got.stderr, "usage:")
			assert.NotContains(t, got.stderr, " -> ")
		})
	}
}

func TestRunProgressLifecycleUsesInjectedSeams(t *testing.T) {
	t.Parallel()

	ticks := make(chan time.Time)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), env{
		args: []string{
			"fetch", "-progress", "1s",
			"-architecture", "amd64",
			"-target", "qemu",
			"-representation", "qcow2",
			"-compression", "none",
			"NOT A REFERENCE", "out",
		},
		stdout: &stdout,
		stderr: &stderr,
		ticks:  ticks,
		now:    func() time.Time { return time.Unix(0, 0).UTC() },
	}, nil)

	assert.Equal(t, exitFailure, code)
	assert.Empty(t, stdout.String())
	got := stderr.String()
	assert.Contains(t, got, "imgoci: fetch ")
	assert.Contains(t, got, "no sentinel matched")
	assert.NotContains(t, got, "imgoci: progress ")
	for _, line := range strings.SplitAfter(got, "\n") {
		if line == "" {
			continue
		}
		assert.True(t, strings.HasPrefix(line, "imgoci: ") || strings.Contains(line, "usage:"), line)
	}
}

func TestProgressLifecycleSerializesLines(t *testing.T) {
	t.Parallel()

	ticks := make(chan time.Time)
	started := time.Unix(0, 0).UTC()
	var stdout, stderr bytes.Buffer
	e := env{
		stdout: &stdout,
		stderr: guardStderr(&stderr),
		ticks:  ticks,
		now:    func() time.Time { return started },
	}

	watch := startProgress(e, time.Second)
	require.NotNil(t, watch)

	snapshot := imgoci.Progress{
		Direction:      "publish",
		Phase:          "upload",
		TotalFiles:     2,
		CompletedFiles: 1,
		TotalBytes:     100,
		CompletedBytes: 40,
		WireBytes:      41,
		Retries:        3,
		Fallbacks:      1,
	}
	watch.record(snapshot)

	wantProgress := renderProgress(snapshot, 0)
	diagnostic := "imgoci: publish spec.json -> reg/repo:v1 (workers=4)\n"
	signalLine := "imgoci: interrupted (SIGINT), stopping; press Ctrl-C again to force quit\n"

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 20 {
			ticks <- started
		}
	}()
	go func() {
		defer wg.Done()
		for range 20 {
			fmt.Fprint(e.stderr, diagnostic)
			fmt.Fprint(e.stderr, signalLine)
		}
	}()
	wg.Wait()
	watch.stop()

	code := reportError(e, errors.New("connection reset"), nil)
	assert.Equal(t, exitFailure, code)
	assert.Empty(t, stdout.String())

	got := stderr.String()
	assert.True(t, strings.HasPrefix(got, "imgoci: "))
	allowed := map[string]bool{
		wantProgress:                             true,
		diagnostic:                               true,
		signalLine:                               true,
		"imgoci: connection reset\n":             true,
		"imgoci: no sentinel matched (exit 1)\n": true,
	}
	for _, line := range strings.SplitAfter(got, "\n") {
		if line == "" {
			continue
		}
		assert.True(t, allowed[line], line)
	}
	assert.Contains(t, got, wantProgress)
	assert.Contains(t, got, diagnostic)
	assert.Contains(t, got, signalLine)
	assert.Contains(t, got, "imgoci: no sentinel matched (exit 1)\n")
}

func TestWritePublishedStdoutFailureSkipsSuccessLine(t *testing.T) {
	t.Parallel()

	errClosed := errors.New("stdout closed")
	var stderr bytes.Buffer
	err := writePublished(errWriter{err: errClosed}, &stderr, "sha256:abc", time.Second)
	require.ErrorIs(t, err, errClosed)
	assert.Empty(t, stderr.String())
}

// errWriter fails every write with err.
type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}

var _ io.Writer = errWriter{}
