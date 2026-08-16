package auth

import (
	"context"
	"net/http"
	"strings"
)

// Credential is what this package presents to one registry. The zero value is
// the anonymous credential, which is a credential and not the absence of one:
// the bearer exchange still runs, because registries that require a bearer
// token for public reads answer an unauthenticated token request with a
// public-access token.
type Credential struct {
	// Username is the account name presented to the token endpoint.
	Username string
	// Password is the secret, or the personal access token, that goes with
	// Username.
	Password string
}

// Empty reports whether the credential carries nothing to present, which is
// the anonymous credential.
func (c Credential) Empty() bool {
	return c.Username == "" && c.Password == ""
}

// Credentials resolves the credential presented to one registry.
//
// A registry the resolver has no credential for is the zero [Credential] and a
// nil error: anonymous is an answer, not a failure. An error means the lookup
// itself could not be performed and it ends the request, because a request that
// quietly fell back to anonymous would fail later and somewhere less obvious.
//
// Implementations must be safe for concurrent use and must not retry: a
// lookup runs inside an attempt the orchestrator is already counting.
type Credentials interface {
	Credential(ctx context.Context, registry string) (Credential, error)
}

// Static answers every registry with one credential the caller supplied.
//
// It is the direct source, for a caller who already holds the secret: a CI
// job with a registry token in its environment, or a program that reads its
// own configuration. Nothing is looked up, no file is read, and no program is
// run.
//
// Every registry is deliberate. A Static credential goes to whatever host the
// caller named, so the caller — who chose both the secret and the reference
// — is the one deciding who sees it. [Store] is the other shape: it answers
// only for the host a credential was stored under.
type Static struct {
	// cred is what every lookup answers with.
	cred Credential
}

// NewStatic returns a source that answers every registry with cred.
func NewStatic(cred Credential) *Static {
	return &Static{cred: cred}
}

// Credential returns the fixed credential, whichever registry asks.
//
// It cannot fail and it cannot block, so the context is the port's shape
// rather than anything this implementation needs.
func (s *Static) Credential(_ context.Context, _ string) (Credential, error) {
	return s.cred, nil
}

// requestHost is the host a request is going to, including any port.
func requestHost(req *http.Request) string {
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}

	return host
}

// sameRegistry reports whether host is registry, compared without regard to
// case. A differing host is off-origin: Authorization is stripped and no
// challenge is answered.
func sameRegistry(registry, host string) bool {
	return strings.EqualFold(registry, host)
}
