package ociref

import (
	"fmt"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
)

// Parsed is the host, repository path, and optional tag and digest a
// Reference named.
type Parsed struct {
	// Host is the registry domain, including a port when the reference named
	// one.
	Host string
	// Repository is the path under /v2, without a leading slash.
	Repository string
	// Tag is the tag when the reference named one, otherwise empty.
	Tag string
	// Digest is the digest when the reference named one, otherwise empty.
	Digest digest.Digest
}

// Parse splits raw into host, repository, optional tag, and optional digest.
//
// Name-only references (registry/repo with neither tag nor digest) parse
// successfully; the caller rejects them. Fetch must name one index; Publish
// is tag-only.
func Parse(raw string) (Parsed, error) {
	named, err := reference.ParseNamed(raw)
	if err != nil {
		return Parsed{}, fmt.Errorf("parse reference %q: %w", raw, err)
	}

	out := Parsed{
		Host:       reference.Domain(named),
		Repository: reference.Path(named),
	}
	if tagged, ok := named.(reference.NamedTagged); ok {
		out.Tag = tagged.Tag()
	}
	if digested, ok := named.(reference.Digested); ok {
		dgst := digested.Digest()
		if dgst.Algorithm() != digest.SHA256 {
			return Parsed{}, fmt.Errorf(
				"parse reference %q: digest algorithm %s is not sha256",
				raw, dgst.Algorithm(),
			)
		}
		out.Digest = dgst
	}

	return out, nil
}

// ManifestRef is the tag or digest string transfer.FetchIndex addresses
// within the bound repository. A digest, when present, wins over a tag.
func (p Parsed) ManifestRef() string {
	if p.Digest != "" {
		return p.Digest.String()
	}

	return p.Tag
}

// RequireTagOnly enforces the tag-only publish contract. display is the
// reference as the caller wrote it, used in the error text.
func RequireTagOnly(display string, p Parsed) error {
	switch {
	case p.Tag != "" && p.Digest == "":
		return nil
	case p.Tag == "" && p.Digest != "":
		return fmt.Errorf(
			"digest-only reference %q cannot name a published index",
			display,
		)
	case p.Tag != "" && p.Digest != "":
		return fmt.Errorf(
			"tag+digest reference %q has no defined write meaning",
			display,
		)
	default:
		return fmt.Errorf(
			"publish reference %q must be tag-only",
			display,
		)
	}
}
