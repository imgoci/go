package registry

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/transfer"
)

func TestNewRequiresHostAndRepository(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Repository: testRepo}); err == nil {
		t.Fatal("expected error for empty host")
	}
	if _, err := New(Config{Host: "registry.example"}); err == nil {
		t.Fatal("expected error for empty repository")
	}
}

func TestPutIsSliceThreeStub(t *testing.T) {
	t.Parallel()

	client := mustClient(t, Config{
		Host:       "registry.example",
		Repository: testRepo,
	})
	err := client.Put(t.Context(), testTag, testAccept, []byte("{}"))
	requireErrorIs(t, err, errors.ErrUnsupported)
}

func TestClientImplementsManifests(t *testing.T) {
	t.Parallel()

	var (
		_ transfer.Manifests = (*Client)(nil)
		_ transfer.Blobs     = (*blobAdapter)(nil)
	)
	client := mustClient(t, Config{
		Host:       "registry.example",
		Repository: testRepo,
	})
	if client.Manifests() != transfer.Manifests(client) {
		t.Fatal("Manifests() did not return the client")
	}
	if client.Blobs() == nil {
		t.Fatal("Blobs() is nil")
	}
}

func TestRealmIsolationByConstruction(t *testing.T) {
	t.Parallel()

	client := mustClient(t, Config{
		Host:       "registry.example",
		Repository: testRepo,
	})
	outer, ok := client.http.Transport.(*identityTransport)
	if !ok {
		t.Fatalf("manifest transport is %T, want *identityTransport", client.http.Transport)
	}
	if outer.pathScoped {
		t.Fatal("manifest identity wrapper is path-scoped; redirect hops would skip enforcement")
	}
	authTransport, ok := outer.base.(*auth.Transport)
	if !ok {
		t.Fatalf("identity base is %T, want *auth.Transport", outer.base)
	}
	inner, ok := authTransport.Base.(*identityTransport)
	if !ok {
		t.Fatalf("auth.Base is %T, want *identityTransport", authTransport.Base)
	}
	if !inner.pathScoped {
		t.Fatal("go-oci-blob registry identity wrapper is not path-scoped")
	}
	if authTransport.RealmClient == nil {
		t.Fatal("RealmClient is nil")
	}
	if _, isIdentity := authTransport.RealmClient.Transport.(*identityTransport); isIdentity {
		t.Fatal("RealmClient is wrapped in identityTransport")
	}
}

func TestOpaqueStorageTransportRequiresOption(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		Host:       "registry.example",
		Repository: testRepo,
		HTTPClient: &http.Client{Transport: roundTripFunc(nil)},
	})
	if err == nil {
		t.Fatal("expected error for opaque transport")
	}
	if !strings.Contains(err.Error(), "WithUnverifiedExternalTransport") {
		t.Fatalf("error = %q, want it to name WithUnverifiedExternalTransport", err)
	}
}

func TestOpaqueStorageTransportAuthorized(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		Host:                        "registry.example",
		Repository:                  testRepo,
		UnverifiedExternalTransport: true,
		HTTPClient:                  &http.Client{Transport: roundTripFunc(nil)},
	})
	requireNoError(t, err)
}
