package imgoci

import (
	"fmt"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
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

// parsedRef is the host, repository path, and optional tag and digest a
// [Reference] named.
type parsedRef struct {
	// host is the registry domain, including a port when the reference named
	// one.
	host string
	// repository is the path under /v2, without a leading slash.
	repository string
	// tag is the tag when the reference named one, otherwise empty.
	tag string
	// digest is the digest when the reference named one, otherwise empty.
	digest digest.Digest
}

// parse splits r into host, repository, optional tag, and optional digest.
//
// Name-only references (registry/repo with neither tag nor digest) parse
// successfully; [Client.Fetch] rejects them because a fetch must name one
// index. Publish later accepts name-only for digest writes.
func (r Reference) parse() (parsedRef, error) {
	named, err := reference.ParseNamed(string(r))
	if err != nil {
		return parsedRef{}, fmt.Errorf("parse reference %q: %w", r, err)
	}

	out := parsedRef{
		host:       reference.Domain(named),
		repository: reference.Path(named),
	}
	if tagged, ok := named.(reference.NamedTagged); ok {
		out.tag = tagged.Tag()
	}
	if digested, ok := named.(reference.Digested); ok {
		dgst := digested.Digest()
		if dgst.Algorithm() != digest.SHA256 {
			return parsedRef{}, fmt.Errorf(
				"parse reference %q: digest algorithm %s is not sha256",
				r, dgst.Algorithm(),
			)
		}
		out.digest = dgst
	}

	return out, nil
}

// manifestRef is the tag or digest string [transfer.FetchIndex] addresses
// within the bound repository. A digest, when present, wins over a tag.
func (p parsedRef) manifestRef() string {
	if p.digest != "" {
		return p.digest.String()
	}

	return p.tag
}
