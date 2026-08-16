//go:build unix

package file

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPlanSymlinkedParentAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}

	_, err := NewPlan(map[string]string{
		"disk":   filepath.Join(realDir, "out"),
		"kernel": filepath.Join(link, "out"),
	})
	if err == nil {
		t.Fatal("expected duplicate resolved path")
	}
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("error %v is not ErrInvalidPlan", err)
	}
}

func TestSecureReopenSymlinkIsAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "disk")
	p, err := NewPlan(map[string]string{"disk": dest})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Cleanup() })
	stageWrite(t, p, "disk", "payload")

	rs := p.roles["disk"]
	if rs == nil || rs.staged == "" {
		t.Fatal("missing staged file")
	}
	target := filepath.Join(dir, "other")
	if writeErr := os.WriteFile(target, []byte("not-payload"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if removeErr := os.Remove(rs.staged); removeErr != nil {
		t.Fatal(removeErr)
	}
	if linkErr := os.Symlink(target, rs.staged); linkErr != nil {
		t.Fatal(linkErr)
	}

	f, err := reopenSecure(rs.staged)
	if f != nil {
		_ = f.Close()
		t.Fatal("reopen returned a handle for a symlink")
	}
	if !errors.Is(err, errAbsent) {
		t.Fatalf("reopen error %v is not errAbsent", err)
	}

	err = p.Commit([]string{"disk"})
	var ce *CommitError
	if !errors.As(err, &ce) {
		t.Fatalf("error %T %v is not *CommitError", err, err)
	}
	if ce.Role != "disk" {
		t.Fatalf("Role %q", ce.Role)
	}
	if !errors.Is(ce.Err, errAbsent) {
		t.Fatalf("Err %v is not errAbsent", ce.Err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		body, _ := os.ReadFile(dest)
		t.Fatalf("destination published from symlink: %q", body)
	}
	if got := readFile(t, target); got != "not-payload" {
		t.Fatalf("symlink target mutated: %q", got)
	}
}

func TestStagingRejectsWorldWritableDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "disk")
	stage := filepath.Join(dir, stageEntryName)
	if err := os.Mkdir(stage, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stage, 0o777); err != nil {
		t.Fatal(err)
	}

	p, err := NewPlan(map[string]string{"disk": dest})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Stage("disk")
	if err == nil {
		t.Fatal("expected unusable staging directory")
	}
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("error %v is not ErrInvalidPlan", err)
	}
	if !strings.Contains(err.Error(), stageEntryName) {
		t.Fatalf("error %v does not name %s", err, stageEntryName)
	}
	entries, readErr := os.ReadDir(stage)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("took over foreign staging dir: %v", entries)
	}
}

func TestStagingReusesPrivateDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "disk")
	stage := filepath.Join(dir, stageEntryName)
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stage, 0o700); err != nil {
		t.Fatal(err)
	}

	p, err := NewPlan(map[string]string{"disk": dest})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Cleanup() })
	stageWrite(t, p, "disk", "payload")
}
