package file

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func TestNewPlanPreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) map[string]string
		wantErr bool
	}{
		{
			name: "two roles distinct files",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				dir := t.TempDir()
				return map[string]string{
					"disk":   filepath.Join(dir, "disk.img"),
					"kernel": filepath.Join(dir, "kernel"),
				}
			},
		},
		{
			name: "imgoci-cache style tree is legal",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				dir := t.TempDir()
				cache := filepath.Join(dir, ".imgoci-cache")
				if err := os.Mkdir(cache, 0o755); err != nil {
					t.Fatal(err)
				}
				return map[string]string{"disk": filepath.Join(cache, "output")}
			},
		},
		{
			name: "duplicate lexical paths",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				shared := filepath.Join(t.TempDir(), "out")

				return map[string]string{"disk": shared, "kernel": shared}
			},
			wantErr: true,
		},
		{
			name: "duplicate via cleaned lexical alias",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				dir := t.TempDir()
				a := filepath.Join(dir, "out")
				b := filepath.Join(dir, ".", "out")

				return map[string]string{"disk": a, "kernel": b}
			},
			wantErr: true,
		},
		{
			name: "existing directory target",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				return map[string]string{"disk": t.TempDir()}
			},
			wantErr: true,
		},
		{
			name: "shadows reserved staging entry",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				return map[string]string{"disk": filepath.Join(t.TempDir(), stageEntryName)}
			},
			wantErr: true,
		},
		{
			name: "empty path",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				return map[string]string{"disk": ""}
			},
			wantErr: true,
		},
		{
			name: "empty role",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				return map[string]string{"": filepath.Join(t.TempDir(), "out")}
			},
			wantErr: true,
		},
		{
			name: "existing file destination is allowed",
			setup: func(t *testing.T) map[string]string {
				t.Helper()
				dir := t.TempDir()
				dest := filepath.Join(dir, "out")
				if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
				return map[string]string{"disk": dest}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNewPlan(t, tc.setup(t), tc.wantErr)
		})
	}
}

func assertNewPlan(t *testing.T, byRole map[string]string, wantErr bool) {
	t.Helper()
	p, err := NewPlan(byRole)
	if wantErr {
		if err == nil {
			t.Fatal("expected preflight error")
		}
		if !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("error %v is not ErrInvalidPlan", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("nil plan")
	}
}

func TestWorkspaceIsolation(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	var mu sync.Mutex
	workspaces := map[string]string{}

	t.Cleanup(func() {
		if t.Failed() {
			return
		}
		if workspaces["a"] == "" || workspaces["b"] == "" {
			t.Errorf("missing workspace paths: %v", workspaces)
		}
		if workspaces["a"] == workspaces["b"] {
			t.Error("concurrent plans shared a workspace")
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"a", "b"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p, err := NewPlan(map[string]string{"disk": filepath.Join(parent, name)})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = p.Cleanup() })
				stageWrite(t, p, "disk", name)
				ws := firstWorkspace(p)
				if ws == "" {
					t.Fatal("no workspace")
				}
				mu.Lock()
				workspaces[name] = ws
				mu.Unlock()
			})
		}
	})
}

// firstWorkspace returns one staging workspace path, or empty if none.
func firstWorkspace(p *Plan) string {
	for _, dir := range p.workspaces {
		return dir
	}

	return ""
}

func TestCommitOrderRespected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dests := map[string]string{
		"kernel": filepath.Join(dir, "kernel"),
		"disk":   filepath.Join(dir, "disk"),
		"initrd": filepath.Join(dir, "initrd"),
	}
	p, err := NewPlan(dests)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Cleanup() })
	stageWrite(t, p, "kernel", "K")
	stageWrite(t, p, "disk", "D")
	stageWrite(t, p, "initrd", "I")

	order := []string{"disk", "initrd", "kernel"}
	var got []string
	p.rename = func(oldpath, newpath string) error {
		got = append(got, filepath.Base(newpath))

		return os.Rename(oldpath, newpath)
	}
	if err := p.Commit(order); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, order) {
		t.Fatalf("rename order %v, want %v", got, order)
	}
}

func TestCommitOrderAndRenameFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dests := map[string]string{
		"kernel": filepath.Join(dir, "kernel"),
		"disk":   filepath.Join(dir, "disk"),
		"initrd": filepath.Join(dir, "initrd"),
	}
	p, err := NewPlan(dests)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Cleanup() })
	stageWrite(t, p, "kernel", "K")
	stageWrite(t, p, "disk", "D")
	stageWrite(t, p, "initrd", "I")

	if mkdirErr := os.Mkdir(dests["initrd"], 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}

	err = p.Commit([]string{"kernel", "disk", "initrd"})
	var ce *CommitError
	if !errors.As(err, &ce) {
		t.Fatalf("error %T %v is not *CommitError", err, err)
	}
	if ce.Role != "initrd" {
		t.Fatalf("Role %q, want initrd", ce.Role)
	}
	if !slices.Equal(ce.Committed, []string{"kernel", "disk"}) {
		t.Fatalf("Committed %v, want [kernel disk]", ce.Committed)
	}
	if got := readFile(t, dests["kernel"]); got != "K" {
		t.Fatalf("kernel %q", got)
	}
	if got := readFile(t, dests["disk"]); got != "D" {
		t.Fatalf("disk %q", got)
	}
	info, err := os.Stat(dests["initrd"])
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("initrd directory was replaced")
	}
}

func TestCommitHappyOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dests := map[string]string{
		"kernel": filepath.Join(dir, "kernel"),
		"disk":   filepath.Join(dir, "disk"),
	}
	p, err := NewPlan(dests)
	if err != nil {
		t.Fatal(err)
	}
	stageWrite(t, p, "kernel", "K")
	stageWrite(t, p, "disk", "D")
	if err := p.Commit([]string{"kernel", "disk"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dests["kernel"]); got != "K" {
		t.Fatalf("kernel %q", got)
	}
	if got := readFile(t, dests["disk"]); got != "D" {
		t.Fatalf("disk %q", got)
	}
	if err := p.Cleanup(); err != nil {
		t.Fatal(err)
	}
	stageRoot := filepath.Join(dir, stageEntryName)
	if _, err := os.Stat(stageRoot); err == nil {
		entries, readErr := os.ReadDir(stageRoot)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("workspace leftover %v", entries)
		}
	}
}

func TestCommitDoesNotFoldCleanup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "disk")
	p, err := NewPlan(map[string]string{"disk": dest})
	if err != nil {
		t.Fatal(err)
	}
	stageWrite(t, p, "disk", "payload")
	ws := firstWorkspace(p)
	if ws == "" {
		t.Fatal("no workspace")
	}
	if err := p.Commit([]string{"disk"}); err != nil {
		t.Fatalf("Commit returned %v; leftover staging must not fail Commit", err)
	}
	if got := readFile(t, dest); got != "payload" {
		t.Fatalf("disk %q", got)
	}
	if _, err := os.Stat(ws); err != nil {
		t.Fatalf("workspace removed by Commit: %v", err)
	}
	if err := p.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRemovesWorkspaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "disk")
	p, err := NewPlan(map[string]string{"disk": dest})
	if err != nil {
		t.Fatal(err)
	}
	stageWrite(t, p, "disk", "payload")
	var ws string
	for _, d := range p.workspaces {
		ws = d
	}
	if _, err := os.Stat(ws); err != nil {
		t.Fatalf("workspace missing before cleanup: %v", err)
	}
	if err := p.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ws); !os.IsNotExist(err) {
		t.Fatalf("workspace still present: %v", err)
	}
	if err := p.Cleanup(); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func TestRetryAfterPartialCommitOverwrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dests := map[string]string{
		"a": filepath.Join(dir, "a"),
		"b": filepath.Join(dir, "b"),
		"c": filepath.Join(dir, "c"),
	}
	p, err := NewPlan(dests)
	if err != nil {
		t.Fatal(err)
	}
	stageWrite(t, p, "a", "v1-a")
	stageWrite(t, p, "b", "v1-b")
	stageWrite(t, p, "c", "v1-c")
	if mkdirErr := os.Mkdir(dests["c"], 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	err = p.Commit([]string{"a", "b", "c"})
	var ce *CommitError
	if !errors.As(err, &ce) {
		t.Fatalf("error %T %v is not *CommitError", err, err)
	}
	if !slices.Equal(ce.Committed, []string{"a", "b"}) {
		t.Fatalf("Committed %v", ce.Committed)
	}
	if cleanupErr := p.Cleanup(); cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	if removeErr := os.Remove(dests["c"]); removeErr != nil {
		t.Fatal(removeErr)
	}

	p2, err := NewPlan(dests)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p2.Cleanup() })
	stageWrite(t, p2, "a", "v2-a")
	stageWrite(t, p2, "b", "v2-b")
	stageWrite(t, p2, "c", "v2-c")
	if err := p2.Commit([]string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dests["a"]); got != "v2-a" {
		t.Fatalf("a %q", got)
	}
	if got := readFile(t, dests["b"]); got != "v2-b" {
		t.Fatalf("b %q", got)
	}
	if got := readFile(t, dests["c"]); got != "v2-c" {
		t.Fatalf("c %q", got)
	}
}

func TestStageUnknownRole(t *testing.T) {
	t.Parallel()
	p, err := NewPlan(map[string]string{"disk": filepath.Join(t.TempDir(), "disk")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stage("kernel"); err == nil {
		t.Fatal("expected unknown role error")
	}
}

func TestWriteAfterClose(t *testing.T) {
	t.Parallel()
	p, err := NewPlan(map[string]string{"disk": filepath.Join(t.TempDir(), "disk")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Cleanup() })
	sf, err := p.Stage("disk")
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sf.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write after Close: %v", err)
	}
}

func stageWrite(t *testing.T, p *Plan, role, content string) {
	t.Helper()
	sf, err := p.Stage(role)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(sf, content); err != nil {
		t.Fatal(err)
	}
	if err := sf.Close(); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
