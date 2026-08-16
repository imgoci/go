package registry

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckIdentityEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  []string
		wantErr bool
	}{
		{name: "absent header is identity", header: nil},
		{name: "empty value is identity", header: []string{""}},
		{name: "identity", header: []string{"identity"}},
		{name: "Identity ASCII case", header: []string{"Identity"}},
		{name: "IDENTITY ASCII case", header: []string{"IDENTITY"}},
		{name: "identity with surrounding spaces", header: []string{" identity "}},
		{name: "repeated identity tokens", header: []string{"identity , identity"}},
		{name: "blank list elements", header: []string{", identity ,"}},
		{name: "gzip is refused", header: []string{"gzip"}, wantErr: true},
		{name: "GZIP ASCII case is refused", header: []string{"GZIP"}, wantErr: true},
		{name: "multi-token gzip then identity is refused", header: []string{"gzip, identity"}, wantErr: true},
		{name: "multi-token identity then gzip is refused", header: []string{"identity, gzip"}, wantErr: true},
		{name: "repeated header lines gzip then identity", header: []string{"identity", "gzip"}, wantErr: true},
		{name: "deflate is refused", header: []string{"deflate"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{Header: make(http.Header)}
			for _, value := range tt.header {
				resp.Header.Add(headerContentEncoding, value)
			}
			err := checkIdentityEncoding(resp)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}

				return
			}
			var coding *contentCodingError
			if !errors.As(err, &coding) {
				t.Fatalf("err = %v, want contentCodingError", err)
			}
			if coding.Error() == "gzip" || coding.Error() == "GZIP" {
				t.Fatalf("error quoted the peer header: %q", coding.Error())
			}
		})
	}
}

func TestIdentityTransportScope(t *testing.T) {
	t.Parallel()

	tests := []identityScopeCase{
		{
			name:    "manifest GET is enforced",
			method:  http.MethodGet,
			rawURL:  "https://registry.example/v2/os/example/manifests/latest",
			scoped:  true,
			wantAE:  true,
			wantErr: false,
		},
		{
			name:     "manifest GET gzip is refused",
			method:   http.MethodGet,
			rawURL:   "https://registry.example/v2/os/example/manifests/latest",
			encoding: "gzip",
			scoped:   true,
			wantAE:   true,
			wantErr:  true,
		},
		{
			name:    "blob HEAD is enforced",
			method:  http.MethodHead,
			rawURL:  "https://registry.example/v2/os/example/blobs/sha256:abc",
			scoped:  true,
			wantAE:  true,
			wantErr: false,
		},
		{
			name:     "blob GET gzip is refused",
			method:   http.MethodGet,
			rawURL:   "https://registry.example/v2/os/example/blobs/sha256:abc",
			encoding: "gzip",
			scoped:   true,
			wantAE:   true,
			wantErr:  true,
		},
		{
			name:     "token realm URL is untouched",
			method:   http.MethodGet,
			rawURL:   "https://auth.example/token?service=registry.example",
			encoding: "gzip",
			scoped:   true,
			wantAE:   false,
			wantErr:  false,
		},
		{
			name:     "non-/v2 path is untouched",
			method:   http.MethodGet,
			rawURL:   "https://registry.example/healthz",
			encoding: "gzip",
			scoped:   true,
			wantAE:   false,
			wantErr:  false,
		},
		{
			name:     "blob upload session is untouched",
			method:   http.MethodGet,
			rawURL:   "https://registry.example/v2/os/example/blobs/uploads/uuid",
			encoding: "gzip",
			scoped:   true,
			wantAE:   false,
			wantErr:  false,
		},
		{
			name:     "manifest PUT is untouched",
			method:   http.MethodPut,
			rawURL:   "https://registry.example/v2/os/example/manifests/latest",
			encoding: "gzip",
			scoped:   true,
			wantAE:   false,
			wantErr:  false,
		},
		{
			name:     "unconditional wrap enforces a storage URL",
			method:   http.MethodGet,
			rawURL:   "https://s3.example/object/blob",
			encoding: "gzip",
			scoped:   false,
			wantAE:   true,
			wantErr:  true,
		},
		{
			name:    "unconditional wrap enforces a storage HEAD",
			method:  http.MethodHead,
			rawURL:  "https://s3.example/object/blob",
			scoped:  false,
			wantAE:  true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkIdentityScope(t, tt)
		})
	}
}

// identityScopeCase is one row of [TestIdentityTransportScope].
type identityScopeCase struct {
	name     string
	method   string
	rawURL   string
	encoding string
	scoped   bool
	wantAE   bool
	wantErr  bool
}

// checkIdentityScope runs one identity-scoping case.
func checkIdentityScope(t *testing.T, tt identityScopeCase) {
	t.Helper()
	var (
		sawAE  string
		closed closeProbe
	)
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawAE = req.Header.Get(headerAcceptEncoding)

		return okResponse(req, tt.encoding, &closed), nil
	})
	wrapper := &identityTransport{base: base, pathScoped: tt.scoped}
	req := httptest.NewRequest(tt.method, tt.rawURL, nil)
	resp, err := wrapper.RoundTrip(req)
	if tt.wantErr {
		if err == nil {
			t.Fatal("expected identity error")
		}
		if !closed.closed {
			t.Fatal("rejected body was not closed")
		}
		if resp != nil {
			t.Fatal("rejected response was returned")
		}

		return
	}
	requireNoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	if tt.wantAE && sawAE != codingIdentity {
		t.Fatalf("Accept-Encoding = %q, want identity", sawAE)
	}
	if !tt.wantAE && sawAE == codingIdentity {
		t.Fatal("unscoped request was given Accept-Encoding: identity")
	}
	if closed.closed {
		t.Fatal("accepted body was closed by the wrapper")
	}
}

func TestIdentityTransportClosesRejectedBody(t *testing.T) {
	t.Parallel()

	var closed closeProbe
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return okResponse(req, "gzip", &closed), nil
	})
	wrapper := newIdentityTransport(base)
	req := httptest.NewRequest(
		http.MethodGet,
		"https://registry.example/v2/os/example/manifests/latest",
		nil,
	)
	_, err := wrapper.RoundTrip(req)
	var coding *contentCodingError
	if !errors.As(err, &coding) {
		t.Fatalf("err = %v, want contentCodingError", err)
	}
	if !closed.closed {
		t.Fatal("rejected body was not closed")
	}
}
