//go:build e2e

// Client fixtures: references, client construction, the shared queries, and
// the fetch and filesystem assertions.

package e2e

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	imgoci "github.com/imgoci/go"
)

// identityErrorText is the unexported identityTransport error message.
const identityErrorText = "the response is not identity coded"

// testRepo is a distribution-spec repository unique to this test name.
func testRepo(t *testing.T) string {
	t.Helper()
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, t.Name())
	return "e2e/" + strings.ToLower(name)
}

// tagRef is registry/repo:tag for the consumer Reference grammar.
func tagRef(registry, repo string) imgoci.Reference {
	return imgoci.Reference(registry + "/" + repo + ":" + e2eTag)
}

// digestRef is registry/repo@sha256:... for the consumer Reference grammar.
func digestRef(registry, repo string, dgst digest.Digest) imgoci.Reference {
	return imgoci.Reference(registry + "/" + repo + "@" + dgst.String())
}

// newE2EClient builds a plaintext client, optionally with static credentials.
func newE2EClient(t *testing.T, cred e2eCreds) *imgoci.Client {
	t.Helper()
	opts := []imgoci.Option{imgoci.WithPlainHTTP()}
	if cred.user != "" {
		opts = append(opts, imgoci.WithCredentials(cred.user, cred.pass))
	}
	client, err := imgoci.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// resolveQEMU selects the qemu/qcow2 disk deliverable.
func resolveQEMU(t *testing.T, client *imgoci.Client, rel *imgoci.Release) *imgoci.Resolved {
	t.Helper()
	return mustResolve(t, client, rel, imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"gzip", "none"},
	})
}

// resolveMetal selects the metal/raw disk deliverable.
func resolveMetal(t *testing.T, client *imgoci.Client, rel *imgoci.Release) *imgoci.Resolved {
	t.Helper()
	return mustResolve(t, client, rel, imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "raw",
		Compressions:   []string{"none", "gzip"},
	})
}

// resolveIncus selects both incus-vm roles.
func resolveIncus(t *testing.T, client *imgoci.Client, rel *imgoci.Release) *imgoci.Resolved {
	t.Helper()
	return mustResolve(t, client, rel, imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "incus",
		Representation: "incus-vm",
		Compressions:   []string{"none", "gzip"},
	})
}

// mustResolve fails the test when Resolve does.
func mustResolve(t *testing.T, client *imgoci.Client, rel *imgoci.Release, q imgoci.ResolveQuery) *imgoci.Resolved {
	t.Helper()
	sel, err := client.Resolve(rel, q)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

// mustFetch fails the test when Fetch does.
func mustFetch(t *testing.T, client *imgoci.Client, ref imgoci.Reference) *imgoci.Release {
	t.Helper()
	rel, err := client.Fetch(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

// mustFetchFiles fails the test when FetchFiles does.
func mustFetchFiles(t *testing.T, client *imgoci.Client, rel *imgoci.Release, sel *imgoci.Resolved, dest imgoci.Dest) {
	t.Helper()
	if err := client.FetchFiles(t.Context(), rel, sel, dest); err != nil {
		t.Fatal(err)
	}
}

// assertFileContent requires path to contain want.
func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %d bytes, want %d identical to seed", path, len(got), len(want))
	}
}

// assertNoFile requires path not to exist.
func assertNoFile(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		t.Fatalf("committed file exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// assertIdentityError requires err to wrap the identity-enforcement failure.
//
// Manifest Get surfaces the message on Error(); go-oci-blob wraps blob Get
// in a requestError whose Error string is "registry request failed", so the
// identity text is only visible on the unwrapped chain.
func assertIdentityError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected identity-enforcement error")
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), identityErrorText) {
			return
		}
	}
	t.Fatalf("err = %v, want chain containing %q", err, identityErrorText)
}
