//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	imgoci "github.com/imgoci/go"
)

// TestGzippedManifestFetch fails Fetch when a reverse proxy gzip-codes
// the index GET.
func TestGzippedManifestFetch(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	seedCanonicalRelease(t, backend, repo, e2eCreds{})
	front := startGzipProxy(t, backend, gzipManifestRequest)
	client := newE2EClient(t, e2eCreds{})
	_, err := client.Fetch(t.Context(), tagRef(front, repo))
	assertIdentityError(t, err)
	assertNotRetried(t, err)
}

// TestGzippedBlobFetchFiles fails FetchFiles on a gzip-coded blob GET
// after Fetch of the index has already succeeded through the same proxy.
func TestGzippedBlobFetchFiles(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	seeded := seedCanonicalRelease(t, backend, repo, e2eCreds{})
	front := startGzipProxy(t, backend, gzipBlobRequest)
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(front, repo))
	sel := resolveQEMU(t, client, rel)
	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, imgoci.ToDir(dir))
	assertIdentityError(t, err)
	assertNotRetried(t, err)
	assertNoFile(t, filepath.Join(dir, seeded.qemu.filename))
}

// assertNotRetried requires err not to carry retry bookkeeping. A coded
// response is terminal; asking again produces the same body.
func assertNotRetried(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "after") && strings.Contains(err.Error(), "attempts") {
		t.Fatalf("coded response was retried: %v", err)
	}
}

// TestGzippedTokenRealm completes Fetch and FetchFiles when the bearer
// realm gzips its token document.
//
// An in-process 401-challenge reverse proxy fronts an anonymous registry:2. The
// test does not drive distribution's token authenticator. [Client.Fetch] and
// [Client.FetchFiles] run against the proxy.
func TestGzippedTokenRealm(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	seeded := seedCanonicalRelease(t, backend, repo, e2eCreds{})
	realm := startGzipTokenRealm(t, e2eBearerToken)
	front := startBearerProxy(t, backend, e2eBearerToken, realm)
	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(front, repo))
	sel := resolveQEMU(t, client, rel)
	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, imgoci.ToDir(dir))
	assertFileContent(t, filepath.Join(dir, seeded.qemu.filename), seeded.qemu.content)
}
