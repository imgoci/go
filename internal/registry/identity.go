package registry

import (
	"net/http"
	"strings"
)

const (
	// headerAcceptEncoding is the request header that asks the far end not to
	// apply a content coding. Setting it also stops [net/http] from adding
	// gzip on its own, so the bytes hashed above this adapter are the stored
	// bytes.
	headerAcceptEncoding = "Accept-Encoding"

	// headerContentEncoding is the response header that names the content
	// coding applied to the body.
	headerContentEncoding = "Content-Encoding"

	// codingIdentity is the only content-coding token a manifest or blob
	// read accepts. It means the body is the stored bytes, untransformed.
	codingIdentity = "identity"

	// pathPrefixV2 is the distribution-spec API prefix identity scoping
	// matches against.
	pathPrefixV2 = "/v2/"

	// pathManifests is the path segment that names a manifest GET or HEAD.
	pathManifests = "/manifests/"

	// pathBlobs is the path segment that names a blob GET or HEAD.
	pathBlobs = "/blobs/"

	// pathUploads is the blob-upload session prefix, which is not a blob
	// GET and must not be identity-enforced on the path-scoped wrapper.
	pathUploads = "uploads"
)

// identityTransport is an [http.RoundTripper] decorator that asks for and
// requires identity content coding.
//
// When pathScoped is true, only GET and HEAD of /v2/…/manifests/… and
// /v2/…/blobs/… are enforced. Token realms, the /v2/ ping, blob-upload
// sessions, and any other URL pass through untouched. Identity is a property of
// stored manifest and blob bytes, not of every HTTP conversation the adapter
// happens to have.
//
// When pathScoped is false, every request is enforced. The manifest client uses
// that so a 302 onto an off-path URL is still identity-coded. go-oci-blob's
// off-origin client carries only redirected blob traffic, so "external means
// blob" is true and a path filter would miss object-store URLs.
type identityTransport struct {
	// base is the next RoundTripper. Nil is [http.DefaultTransport].
	base http.RoundTripper
	// pathScoped limits enforcement to distribution manifest and blob
	// GET/HEAD. False means every request.
	pathScoped bool
}

// newIdentityTransport wraps base with path-and-method-scoped identity
// enforcement for go-oci-blob's registry-origin manifest and blob GET/HEAD.
func newIdentityTransport(base http.RoundTripper) *identityTransport {
	return &identityTransport{base: base, pathScoped: true}
}

// newStorageIdentityTransport wraps base with identity enforcement on every
// request. The manifest client uses it so redirect hops stay in scope; the
// storage transport uses it because every request is a blob GET.
func newStorageIdentityTransport(base http.RoundTripper) *identityTransport {
	return &identityTransport{base: base, pathScoped: false}
}

// RoundTrip applies identity enforcement when the request is in scope, then
// forwards it to the base transport. Accept-Encoding: identity is set on the
// real request so [net/http.Client] copies it onto redirect hops.
func (t *identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.pathScoped && !identityApplies(req) {
		return t.next().RoundTrip(req)
	}

	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set(headerAcceptEncoding, codingIdentity)
	outbound := req.Clone(req.Context())

	resp, err := t.next().RoundTrip(outbound)
	if err != nil {
		return nil, err
	}
	if err := checkIdentityEncoding(resp); err != nil {
		closeResponseBody(resp)

		return nil, err
	}

	return resp, nil
}

// next is the base RoundTripper, or [http.DefaultTransport] when base is nil.
func (t *identityTransport) next() http.RoundTripper {
	if t.base != nil {
		return t.base
	}

	return http.DefaultTransport
}

// identityApplies reports whether req is a distribution manifest or blob GET
// or HEAD. HEAD is included because existence checks use it and a coded HEAD
// would still hide the stored bytes' coding.
func identityApplies(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}

	return identityPath(req.URL.EscapedPath())
}

// identityPath reports whether path is a distribution manifest or blob
// object path. Blob-upload sessions under /blobs/uploads are excluded:
// they are not stored-blob GETs.
func identityPath(path string) bool {
	if !strings.HasPrefix(path, pathPrefixV2) {
		return false
	}
	if i := strings.LastIndex(path, pathManifests); i >= 0 {
		return path[i+len(pathManifests):] != ""
	}
	i := strings.LastIndex(path, pathBlobs)
	if i < 0 {
		return false
	}
	rest := path[i+len(pathBlobs):]

	return rest != "" && !strings.HasPrefix(rest, pathUploads)
}

// contentCodingError reports a manifest or blob response that arrived under
// a content coding other than identity.
//
// It matches no public sentinel and is not marked transient: asking again
// produces the same coded body. The peer-controlled Content-Encoding value
// never appears in the message, because a registry or middlebox can put
// anything there, including a reflected credential.
type contentCodingError struct{}

// Error names the identity-coding rule without quoting the peer's header.
func (*contentCodingError) Error() string {
	return "the response is not identity coded"
}

// checkIdentityEncoding reports whether a response arrived identity-coded.
//
// Every Content-Encoding field is inspected, including repeated header
// lines, and each field is split on commas the way RFC 9110 joins list
// values. Tokens are compared without regard to ASCII case. An absent
// header, a blank value, and the token "identity" are accepted; any other
// token is refused.
func checkIdentityEncoding(resp *http.Response) error {
	if resp == nil {
		return nil
	}
	for _, field := range resp.Header.Values(headerContentEncoding) {
		for token := range strings.SplitSeq(field, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if !asciiEqualFold(token, codingIdentity) {
				return &contentCodingError{}
			}
		}
	}

	return nil
}

// asciiEqualFold reports whether a and b are equal after folding ASCII
// A–Z to a–z. Other bytes, including UTF-8 for U+017F and U+212A, are
// unchanged, so this is not [strings.EqualFold].
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if asciiFold(a[i]) != asciiFold(b[i]) {
			return false
		}
	}

	return true
}

// asciiFold returns c with ASCII 'A'..'Z' folded to 'a'..'z'.
func asciiFold(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}

	return c
}

// closeResponseBody closes resp's body if it has one. Close errors are
// discarded: the caller is already returning a different diagnosis.
func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_ = resp.Body.Close()
}
