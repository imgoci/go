//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"

	imgoci "github.com/imgoci/go"
)

// TestProgressRoundTripBothForms publishes and fetches a standard file and
// a BigOCI file, requiring one serialized absolute stream for each call.
//
// Fetch ends in commit exactly once. Publish ends in index exactly once.
// WireBytes is positive on both forms: standard counts the stored blob, and
// BigOCI counts the multipart transfer's latest-absolute wire total.
func TestProgressRoundTripBothForms(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eRegistries()[0].image)
	client := newE2EClient(t, e2eCreds{})

	t.Run("standard", func(t *testing.T) {
		t.Parallel()
		repo := testRepo(t)
		spec, query, files, _ := roundTripSpec(t, "none", "single-role")
		var pub progressSink
		if _, err := client.Publish(t.Context(), tagRef(host, repo), spec, imgoci.WithProgress(pub.fn())); err != nil {
			t.Fatal(err)
		}
		assertE2EPublishProgress(t, pub.snapshots(), 1)

		rel := mustFetch(t, client, tagRef(host, repo))
		sel := mustResolve(t, client, rel, query)
		dir := t.TempDir()
		var fetch progressSink
		err := client.FetchFiles(
			t.Context(), rel, sel, imgoci.ToDir(dir), imgoci.WithProgress(fetch.fn()),
		)
		if err != nil {
			t.Fatal(err)
		}
		assertE2EFetchProgress(t, fetch.snapshots(), 1, int64(len(files[0].content)))
		assertFileContent(t, filepath.Join(dir, files[0].filename), files[0].content)
	})

	t.Run("bigoci", func(t *testing.T) {
		t.Parallel()
		repo := testRepo(t)
		content := randomBytes(t, e2eBigOCIFileSize)
		spec, _, filename := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
		var pub progressSink
		if _, err := client.Publish(t.Context(), tagRef(host, repo), spec, imgoci.WithProgress(pub.fn())); err != nil {
			t.Fatal(err)
		}
		assertE2EPublishProgress(t, pub.snapshots(), 1)

		rel := mustFetch(t, client, tagRef(host, repo))
		sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
		dir := t.TempDir()
		var fetch progressSink
		err := client.FetchFiles(
			t.Context(), rel, sel, imgoci.ToDir(dir), imgoci.WithProgress(fetch.fn()),
		)
		if err != nil {
			t.Fatal(err)
		}
		assertE2EFetchProgress(t, fetch.snapshots(), 1, int64(len(content)))
		assertFileContent(t, filepath.Join(dir, filename), content)
	})
}

// assertE2EPublishProgress requires a monotone publish stream that ends in
// index exactly once and reports a positive wire total.
func assertE2EPublishProgress(t *testing.T, snaps []imgoci.Progress, files int) {
	t.Helper()
	if len(snaps) < 3 {
		t.Fatalf("got %d publish snapshots, want at least hashing+upload+index", len(snaps))
	}
	if snaps[0].Direction != "publish" || snaps[0].Phase != "hashing" {
		t.Fatalf("initial publish %+v", snaps[0])
	}
	indexN := 0
	var completed int
	var completedBytes int64
	var wire int64
	var retries int
	var fallbacks int
	for i, s := range snaps {
		if s.Direction != "publish" {
			t.Fatalf("snap %d direction %q", i, s.Direction)
		}
		if s.CompletedFiles < completed || s.CompletedBytes < completedBytes ||
			s.WireBytes < wire || s.Retries < retries || s.Fallbacks < fallbacks {
			t.Fatalf("snap %d not monotone: %+v", i, s)
		}
		if s.TotalFiles != files {
			t.Fatalf("snap %d TotalFiles = %d, want %d", i, s.TotalFiles, files)
		}
		completed = s.CompletedFiles
		completedBytes = s.CompletedBytes
		wire = s.WireBytes
		retries = s.Retries
		fallbacks = s.Fallbacks
		if s.Phase == "index" {
			indexN++
		}
	}
	last := snaps[len(snaps)-1]
	if last.Phase != "index" || last.CompletedFiles != files {
		t.Fatalf("terminal publish %+v", last)
	}
	if indexN != 1 {
		t.Fatalf("index-phase snapshots %d, want 1", indexN)
	}
	if last.WireBytes <= 0 {
		t.Fatalf("publish WireBytes = %d, want > 0", last.WireBytes)
	}
}

// assertE2EFetchProgress requires a monotone fetch stream that ends in
// commit exactly once and reports a positive wire total.
func assertE2EFetchProgress(t *testing.T, snaps []imgoci.Progress, files int, contentBytes int64) {
	t.Helper()
	if len(snaps) < 3 {
		t.Fatalf("got %d fetch snapshots, want at least staging+verified+commit", len(snaps))
	}
	if snaps[0].Direction != "fetch" || snaps[0].Phase != "staging" ||
		snaps[0].TotalFiles != files || snaps[0].TotalBytes != contentBytes {
		t.Fatalf("initial fetch %+v", snaps[0])
	}
	commitN := 0
	var completed int
	var completedBytes int64
	var wire int64
	var retries int
	for i, s := range snaps {
		if s.Direction != "fetch" {
			t.Fatalf("snap %d direction %q", i, s.Direction)
		}
		if s.TotalFiles != files || s.TotalBytes != contentBytes {
			t.Fatalf("snap %d totals changed: %+v", i, s)
		}
		if s.CompletedFiles < completed || s.CompletedBytes < completedBytes ||
			s.WireBytes < wire || s.Retries < retries {
			t.Fatalf("snap %d not monotone: %+v", i, s)
		}
		completed = s.CompletedFiles
		completedBytes = s.CompletedBytes
		wire = s.WireBytes
		retries = s.Retries
		if s.Phase == "commit" {
			commitN++
		}
	}
	last := snaps[len(snaps)-1]
	if last.Phase != "commit" || last.CompletedFiles != files || last.CompletedBytes != contentBytes {
		t.Fatalf("terminal fetch %+v", last)
	}
	if commitN != 1 {
		t.Fatalf("commit-phase snapshots %d, want 1", commitN)
	}
	if last.WireBytes <= 0 {
		t.Fatalf("fetch WireBytes = %d, want > 0", last.WireBytes)
	}
}
