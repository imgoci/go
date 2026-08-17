//go:build e2e

package e2e

import (
	"errors"
	"path/filepath"
	"testing"

	imgoci "github.com/imgoci/go"
)

// e2eCompressionNames returns the v1 compression matrix the consumer and
// producer both implement.
func e2eCompressionNames() []string {
	return []string{"none", "gzip", "xz", "zstd"}
}

// TestTruncatedStoredFile fails FetchFiles when the stored blob is a
// prefix of a well-formed compression unit. gzip, xz, and zstd surface
// [ErrDecode]; none surfaces [ErrDigestMismatch] because the identity
// decoder yields fewer bytes than the declared content size.
func TestTruncatedStoredFile(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	for _, compression := range e2eCompressionNames() {
		t.Run(compression, func(t *testing.T) {
			t.Parallel()
			repo := testRepo(t)
			file := seedTruncatedStored(t, host, repo, compression)
			client := newE2EClient(t, e2eCreds{})
			rel := mustFetch(t, client, tagRef(host, repo))
			sel := mustResolve(t, client, rel, imgoci.ResolveQuery{
				Architecture:   "amd64",
				Target:         "qemu",
				Representation: "qcow2",
				Compressions:   []string{compression},
			})

			dir := t.TempDir()
			err := client.FetchFiles(t.Context(), rel, sel, imgoci.ToDir(dir))
			want := imgoci.ErrDecode
			if compression == "none" {
				want = imgoci.ErrDigestMismatch
			}
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
			assertNoFile(t, filepath.Join(dir, file.filename))
		})
	}
}

// TestDecodeBombCeiling aborts when decoded output would pass the index
// content size. [decomp.ErrSizeExceeded] maps to public [ErrDigestMismatch].
func TestDecodeBombCeiling(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	for _, compression := range e2eCompressionNames() {
		t.Run(compression, func(t *testing.T) {
			t.Parallel()
			repo := testRepo(t)
			file := seedDecodeBombCeiling(t, host, repo, compression)
			client := newE2EClient(t, e2eCreds{})
			rel := mustFetch(t, client, tagRef(host, repo))
			sel := mustResolve(t, client, rel, imgoci.ResolveQuery{
				Architecture:   "amd64",
				Target:         "qemu",
				Representation: "qcow2",
				Compressions:   []string{compression},
			})

			dir := t.TempDir()
			err := client.FetchFiles(t.Context(), rel, sel, imgoci.ToDir(dir))
			if !errors.Is(err, imgoci.ErrDigestMismatch) {
				t.Fatalf("err = %v, want ErrDigestMismatch", err)
			}
			assertNoFile(t, filepath.Join(dir, file.filename))
		})
	}
}
