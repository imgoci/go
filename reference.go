package imgoci

import (
	"github.com/imgoci/go/internal/ociref"
)

// Reference names one release: a registry, a repository on it, and a tag or a
// digest, written registry/repo[:tag][@sha256:...].
//
// The grammar is the one container tooling uses, parsed by
// github.com/distribution/reference. Four rules follow from it. The registry
// is required, so "team/image:v1" is not a reference and no short name is
// quietly expanded to Docker Hub. The name must be canonical, which means
// lowercase. A fetch must carry a tag or a digest, because every retrieval
// names one index. And a digest must be sha256 — the only algorithm the v1
// format uses — so a reference naming another one is refused before a request
// is made.
//
// A reference that carries both a tag and a digest is bound to its digest:
// the digest names one index exactly, while the tag beside it is a claim
// about where that tag pointed. Fetching by digest also makes the client
// check the index it fetched against it.
//
// A malformed reference is a caller error. It is not [ErrInvalidIndex] (that
// sentinel is for a retrieved document that failed spec section 6) and it is
// not [ErrInvalidSpec] (that sentinel is producer-only, including the
// tag-only Publish contract). The error is descriptive and wraps the
// underlying parse failure when there is one.
type Reference string

// parse delegates to [ociref.Parse].
func (r Reference) parse() (ociref.Parsed, error) { return ociref.Parse(string(r)) }
