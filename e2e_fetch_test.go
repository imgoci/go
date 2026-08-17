//go:build e2e

package imgoci

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestE2EFetchRoundTrip fetches a seeded release from zot and CNCF
// Distribution, resolves qemu and metal, and writes files that match the seeded
// bytes.
func TestE2EFetchRoundTrip(t *testing.T) {
	t.Parallel()
	for _, reg := range e2eRegistries() {
		t.Run(reg.name, func(t *testing.T) {
			t.Parallel()
			host := startRegistry(t, reg.image)
			repo := testRepo(t)
			seeded := seedCanonicalRelease(t, host, repo, e2eCreds{})
			client := newE2EClient(t, e2eCreds{})
			rel := mustFetch(t, client, tagRef(host, repo))
			if rel.Digest() != seeded.indexDigest {
				t.Fatalf("tag fetch digest %s, want %s", rel.Digest(), seeded.indexDigest)
			}

			qemu := resolveQEMU(t, client, rel)
			metal := resolveMetal(t, client, rel)

			dir := t.TempDir()
			mustFetchFiles(t, client, rel, qemu, ToDir(dir))
			assertFileContent(t, filepath.Join(dir, seeded.qemu.filename), seeded.qemu.content)

			dir = t.TempDir()
			mustFetchFiles(t, client, rel, metal, ToDir(dir))
			assertFileContent(t, filepath.Join(dir, seeded.metal.filename), seeded.metal.content)

			custom := filepath.Join(t.TempDir(), "custom.qcow2")
			mustFetchFiles(t, client, rel, qemu, ToFiles(map[string]string{
				"disk": custom,
			}))
			assertFileContent(t, custom, seeded.qemu.content)

			pinned := mustFetch(t, client, digestRef(host, repo, seeded.indexDigest))
			if pinned.Digest() != seeded.indexDigest {
				t.Fatalf("digest fetch %s, want %s", pinned.Digest(), seeded.indexDigest)
			}
			dir = t.TempDir()
			mustFetchFiles(t, client, pinned, resolveQEMU(t, client, pinned), ToDir(dir))
			assertFileContent(t, filepath.Join(dir, seeded.qemu.filename), seeded.qemu.content)
		})
	}
}

// TestE2ETagMutationPinsDigest keeps FetchFiles on the originally fetched
// bytes after the tag is repointed at a different index.
func TestE2ETagMutationPinsDigest(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	seeded := seedCanonicalRelease(t, host, repo, e2eCreds{})
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(host, repo))
	sel := resolveQEMU(t, client, rel)
	seedAlternateIndex(t, seeded)

	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, ToDir(dir))
	assertFileContent(t, filepath.Join(dir, seeded.qemu.filename), seeded.qemu.content)
}

// TestE2EBitflippedLayer reports ErrDigestMismatch and commits nothing when
// the index content digest names different decoded bytes than the layer.
func TestE2EBitflippedLayer(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	file := seedBitflippedLayer(t, host, repo)
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(host, repo))
	sel := resolveQEMU(t, client, rel)

	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	assertNoFile(t, filepath.Join(dir, file.filename))
}

// TestE2EOverlongLayer aborts a stored stream that exceeds the declared
// layer size and leaves the destination uncommitted.
func TestE2EOverlongLayer(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	file := seedOverlongLayer(t, host, repo)
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(host, repo))
	sel := resolveQEMU(t, client, rel)

	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	assertNoFile(t, filepath.Join(dir, file.filename))
}

// TestE2EStandardLayerCorruptedAfterPublish fails FetchFiles when a standard
// file layer blob is corrupted after publication. Content-addressed registries
// refuse a digest-mismatched overwrite, so a reverse proxy bit-flips the layer
// GET body, keeping its length: a short read would be retried via Range and
// still verify, while a same-length change is terminal. The metal role is
// compression=none, so the flipped byte is decoded content and both the
// declared layer digest and the index content digest name other bytes. The
// consumer reports [ErrDigestMismatch] and writes nothing to the destination.
func TestE2EStandardLayerCorruptedAfterPublish(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	seeded := seedCanonicalRelease(t, backend, repo, e2eCreds{})
	client := newE2EClient(t, e2eCreds{})
	front := startTruncatingBlobProxy(t, backend)
	rel := mustFetch(t, client, tagRef(front, repo))
	sel := resolveMetal(t, client, rel)

	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	assertNoFile(t, filepath.Join(dir, seeded.metal.filename))
	assertNoDestFile(t, dir)
}

// TestE2ESecondRoleCorrupt commits neither incus-vm role when metadata fails
// verification after disk has already been eligible to stage.
func TestE2ESecondRoleCorrupt(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	disk, metadata := seedCorruptSecondRole(t, host, repo)
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(host, repo))
	sel := resolveIncus(t, client, rel)

	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	assertNoFile(t, filepath.Join(dir, disk.filename))
	assertNoFile(t, filepath.Join(dir, metadata.filename))
}

// TestE2ERetryOverwritesAll replaces every selected file on a second
// FetchFiles after one committed path was corrupted.
//
// Commit-phase rename failure (a directory planted at a final path after
// preflight) is covered by internal/file.TestCommitOrderAndRenameFailure.
// Injecting that from FetchFiles is racy because commit runs inside the
// call. This test covers the retry-overwrites-all contract instead.
func TestE2ERetryOverwritesAll(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	seeded := seedCanonicalRelease(t, host, repo, e2eCreds{})
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(host, repo))
	sel := resolveIncus(t, client, rel)

	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, ToDir(dir))
	diskPath := filepath.Join(dir, seeded.disk.filename)
	metaPath := filepath.Join(dir, seeded.metadata.filename)
	if err := os.WriteFile(diskPath, []byte("corrupted-on-disk"), 0o600); err != nil {
		t.Fatal(err)
	}

	mustFetchFiles(t, client, rel, sel, ToDir(dir))
	assertFileContent(t, diskPath, seeded.disk.content)
	assertFileContent(t, metaPath, seeded.metadata.content)
}

// TestE2EHtpasswdAuth checks correct credentials round-trip and that wrong
// or missing credentials surface as ErrUnauthorized.
func TestE2EHtpasswdAuth(t *testing.T) {
	t.Parallel()
	host := startHtpasswdRegistry(t)
	repo := testRepo(t)
	cred := e2eCreds{user: e2eUser, pass: e2ePass}
	seeded := seedCanonicalRelease(t, host, repo, cred)
	ref := tagRef(host, repo)

	t.Run("correct", func(t *testing.T) {
		t.Parallel()
		client := newE2EClient(t, cred)
		rel := mustFetch(t, client, ref)
		sel := resolveQEMU(t, client, rel)
		dir := t.TempDir()
		mustFetchFiles(t, client, rel, sel, ToDir(dir))
		assertFileContent(t, filepath.Join(dir, seeded.qemu.filename), seeded.qemu.content)
	})
	t.Run("wrong", func(t *testing.T) {
		t.Parallel()
		client := newE2EClient(t, e2eCreds{user: e2eUser, pass: e2eWrongPass})
		_, err := client.Fetch(t.Context(), ref)
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("wrong creds: err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("anonymous", func(t *testing.T) {
		t.Parallel()
		client := newE2EClient(t, e2eCreds{})
		_, err := client.Fetch(t.Context(), ref)
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("anonymous: err = %v, want ErrUnauthorized", err)
		}
	})
}

// assertNoDestFile requires dir to hold no file after a failed fetch. An empty
// reserved staging directory may remain: internal/file removes the per-call
// workspace and unlinks the staging root best-effort. A committed file may not.
func assertNoDestFile(t *testing.T, dir string) {
	t.Helper()
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			t.Errorf("destination holds %s after a failed fetch", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
