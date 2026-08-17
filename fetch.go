package imgoci

import (
	"context"
	"errors"
	"fmt"

	"github.com/imgoci/go/internal/transfer"
)

// Fetch retrieves and fully validates the release index ref names.
//
// The reference must include a tag, a digest, or both. Name-only references
// are a caller error, not [ErrInvalidSpec]. A digest, when present, pins the
// retrieved bytes; a tag beside it is a claim. Subsequent [Client.FetchFiles]
// calls address the same host and repository and name file manifests by
// digest, so a tag mutation after Fetch cannot redirect retrieval.
//
// Fetch GETs the index with Accept set to the release-index media type,
// requires the response Content-Type to identify that same type, hashes the
// original bytes, checks a digest pin when the reference named one, and runs
// [ParseIndex]. A Content-Type mismatch is [ErrInvalidIndex]. [Index] has no
// MediaType accessor: [ParseIndex] already validated the JSON member against
// the spec index type (rule 1), and the HTTP Content-Type is required to
// identify that same type.
//
// Spec section 7.1 requires a consumer to validate the query before fetching
// the release. This API is fetch-once, query-many: Fetch takes a [Reference]
// and no query, so it validates none. A query first reaches the library at
// [Index.List], [Index.Resolve], or [Client.Resolve], each of which validates
// it completely before inspecting any index entry — the first method that
// receives the query, but necessarily after this fetch. The consequence is one
// wasted manifest round trip for an invalid query; an invalid query never
// yields a result.
func (c *Client) Fetch(ctx context.Context, ref Reference) (*Release, error) {
	if c == nil {
		return nil, errors.New("fetch: nil client")
	}

	parsed, err := ref.parse()
	if err != nil {
		return nil, err
	}
	if parsed.tag == "" && parsed.digest == "" {
		return nil, fmt.Errorf(
			"reference %q must name a tag or a digest, for example repo:v1 or repo@sha256:<hex>",
			ref,
		)
	}

	ports, err := c.portsFor(ctx, parsed.host, parsed.repository)
	if err != nil {
		return nil, err
	}

	body, err := transfer.FetchIndex(ctx, ports.Manifests, parsed.manifestRef(), parsed.digest)
	if err != nil {
		return nil, mapFetchError(err)
	}

	idx, err := ParseIndex(body.Bytes)
	if err != nil {
		return nil, err
	}

	return &Release{
		digest:     idx.Digest(),
		index:      idx,
		host:       parsed.host,
		repository: parsed.repository,
	}, nil
}

// Resolve selects one deliverable from rel according to spec section 7.3.
//
// It is identical to [Index.Resolve] except a zero q.Capabilities defaults to
// [Client.Capabilities], so selection can never outrun retrieval.
func (c *Client) Resolve(rel *Release, q ResolveQuery) (*Resolved, error) {
	if c == nil {
		return nil, errors.New("resolve: nil client")
	}
	if rel == nil {
		return nil, errors.New("resolve: nil release")
	}
	idx := rel.Index()
	if idx == nil {
		return nil, errors.New("resolve: nil index")
	}
	if len(q.Capabilities.types) == 0 {
		q.Capabilities = c.Capabilities()
	}

	return idx.Resolve(q)
}
