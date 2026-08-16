package imgoci

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/file"
	"github.com/imgoci/go/internal/transfer"
)

func testReleaseAndSelection(artifactType string) (*Release, *Resolved) {
	entry := testEntry("amd64", "x-test-target", "x-test-format", "x-test-file", "none", artifactType)
	idx := testIndex(entry)
	rel := &Release{
		digest:     idx.Digest(),
		index:      idx,
		host:       testHost,
		repository: testRepository,
	}
	sel := &Resolved{digest: idx.Digest(), entries: []FileEntry{entry}}

	return rel, sel
}

func TestFetchFilesPreconditionsNoAdapter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*Release, *Resolved) (*Release, *Resolved, Dest)
		want  error
	}{
		{
			name: "digest_mismatch",
			setup: func(rel *Release, sel *Resolved) (*Release, *Resolved, Dest) {
				// testIndex identity is sha256:bbbb…; this must be a different
				// digest so the selection-mismatch gate fires before any
				// adapter construction.
				sel.digest = digest.Digest("sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

				return rel, sel, ToDir(t.TempDir())
			},
			want: ErrSelectionMismatch,
		},
		{
			name: "wrong_capability",
			setup: func(*Release, *Resolved) (*Release, *Resolved, Dest) {
				rel, sel := testReleaseAndSelection("application/vnd.example.unsupported.v1")

				return rel, sel, ToDir(t.TempDir())
			},
			want: ErrUnsupportedType,
		},
		{
			name: "tofiles_missing_role",
			setup: func(rel *Release, sel *Resolved) (*Release, *Resolved, Dest) {
				return rel, sel, ToFiles(map[string]string{})
			},
			want: ErrInvalidDest,
		},
		{
			name: "tofiles_extra_role",
			setup: func(rel *Release, sel *Resolved) (*Release, *Resolved, Dest) {
				return rel, sel, ToFiles(map[string]string{
					"x-test-file": filepath.Join(t.TempDir(), "a"),
					"extra":       filepath.Join(t.TempDir(), "b"),
				})
			},
			want: ErrInvalidDest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rel, sel := testReleaseAndSelection(standardFileMediaType)
			rel, sel, dest := tt.setup(rel, sel)

			var constructed int
			c := clientWithPorts(t, &stubManifests{}, &constructed)
			err := c.FetchFiles(t.Context(), rel, sel, dest)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if constructed != 0 {
				t.Fatal("adapter must not be constructed when a precondition fails")
			}
		})
	}
}

func TestToDirJoinsFilename(t *testing.T) {
	t.Parallel()
	entry := testEntry("amd64", "x-test-target", "x-test-format", "x-test-file", "none", standardFileMediaType)
	got, err := ToDir("/out").mapByRole([]FileEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/out", entry.Filename)
	if got[entry.Selector.Role] != want {
		t.Fatalf("got %q, want %q", got[entry.Selector.Role], want)
	}
}

func TestToFilesClonesMap(t *testing.T) {
	t.Parallel()
	entry := testEntry("amd64", "x-test-target", "x-test-format", "x-test-file", "none", standardFileMediaType)
	orig := map[string]string{"x-test-file": "/out/a"}
	dest := ToFiles(orig)
	orig["x-test-file"] = "/mutated"
	orig["extra"] = "/extra"
	got, err := dest.mapByRole([]FileEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if got["x-test-file"] != "/out/a" {
		t.Fatalf("cloned map saw mutation: %v", got)
	}
}

func TestMapFetchError(t *testing.T) {
	t.Parallel()
	commit := &file.CommitError{
		Committed: []string{"disk"},
		Role:      "metadata",
		Err:       errors.New("rename failed"),
	}
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "invalid_plan", err: fmt.Errorf("plan: %w", file.ErrInvalidPlan), want: ErrInvalidDest},
		{
			name: "invalid_index_document",
			err:  fmt.Errorf("index: %w", transfer.ErrInvalidDocument),
			want: ErrInvalidIndex,
		},
		{
			name: "invalid_manifest_document",
			err:  fmt.Errorf("role disk: %w", transfer.ErrInvalidDocument),
			want: ErrInvalidIndex,
		},
		{name: "digest", err: fmt.Errorf("blob: %w", transfer.ErrDigestMismatch), want: ErrDigestMismatch},
		{name: "size_exceeded", err: fmt.Errorf("bounded: %w", decomp.ErrSizeExceeded), want: ErrDigestMismatch},
		{name: "decode", err: fmt.Errorf("gzip: %w", decomp.ErrDecode), want: ErrDecode},
		{name: "not_found", err: fmt.Errorf("get: %w", transfer.ErrNotFound), want: ErrNotFound},
		{name: "unauthorized", err: fmt.Errorf("get: %w", transfer.ErrUnauthorized), want: ErrUnauthorized},
		{
			name: "bigoci_profile",
			err:  fmt.Errorf("role disk: %w", transfer.ErrInvalidDocument),
			want: ErrInvalidIndex,
		},
		{
			name: "bigoci_stored_digest",
			err:  fmt.Errorf("role disk: stored file: %w", transfer.ErrDigestMismatch),
			want: ErrDigestMismatch,
		},
		{name: "commit", err: commit},
		{name: "bigoci_not_configured", err: errors.New("role disk: bigoci retrieval not configured")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapFetchError(tt.err)
			if tt.want != nil && !errors.Is(got, tt.want) {
				t.Fatalf("err = %v, want %v", got, tt.want)
			}
			assertMapFetchSpecial(t, tt.name, tt.err, got)
		})
	}
}

// assertMapFetchSpecial checks the two cases with expectations beyond a
// sentinel match: commit errors keep their type and named roles, and the
// nil-multipart wiring error stays unmapped.
func assertMapFetchSpecial(t *testing.T, name string, in, got error) {
	t.Helper()
	switch name {
	case "commit":
		if !strings.Contains(got.Error(), "disk") {
			t.Fatalf("commit error must name committed roles: %v", got)
		}
		var gotCommit *file.CommitError
		if !errors.As(got, &gotCommit) {
			t.Fatalf("errors.As CommitError failed: %v", got)
		}
	case "bigoci_not_configured":
		if errors.Is(got, ErrUnsupportedType) {
			t.Fatalf("nil multipart must not map to ErrUnsupportedType: %v", got)
		}
		if got.Error() != in.Error() {
			t.Fatalf("plain wiring error remapped: %v", got)
		}
	}
}

func TestZeroDestIsInvalid(t *testing.T) {
	t.Parallel()
	_, err := Dest{}.mapByRole(nil)
	if !errors.Is(err, ErrInvalidDest) {
		t.Fatalf("err = %v, want ErrInvalidDest", err)
	}
}
