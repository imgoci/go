package multipart

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/imgoci/bigoci"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/imgoci/go/internal/transfer"
)

// Config is the construction-time settings for the BigOCI adapter.
type Config struct {
	// HTTPClient is an optional injected client. Nil leaves bigoci to build
	// its own default verified stack. A non-nil client is passed via
	// [bigoci.WithHTTPClient], which implements the documented
	// BigociExternalBase/BigociWrapExternal seam (ARCHITECTURE.md §6.6.3).
	HTTPClient *http.Client
	// PlainHTTP selects http:// registry URLs instead of https://. Meant for
	// local registries served without TLS.
	PlainHTTP bool
	// Username is the static registry username. Empty, with empty Secret, is
	// anonymous.
	Username string
	// Secret is the static registry password or token.
	Secret string
}

// Client is the BigOCI adapter. It implements [transfer.Multipart].
//
// The adapter owns its retry budget (bigoci's internal loop). internal/retry
// must never wrap [Client.Push] or [Client.PullTo].
type Client struct {
	// inner is the public bigoci client, or a test fake of that surface.
	inner bigociAPI
}

// New builds a [Client] for cfg.
func New(cfg Config) (*Client, error) {
	inner, err := bigoci.New(clientOptions(cfg)...)
	if err != nil {
		return nil, err
	}
	return &Client{inner: inner}, nil
}

// Push publishes path into repo by digest and returns the manifest descriptor.
//
// repo is passed to [bigoci.Client.PushByDigest] unchanged: a repository-only
// reference, never a tag. partSize zero (or negative) selects the bigoci
// default. The adapter owns retries; the caller must not wrap this call.
func (c *Client) Push(ctx context.Context, repo, path string, partSize int64) (ocispec.Descriptor, error) {
	desc, err := c.inner.PushByDigest(ctx, bigoci.Reference(repo), bigoci.FromFile(path), c.pushOptions(partSize)...)
	if err != nil {
		return ocispec.Descriptor{}, classify(err)
	}
	return desc, nil
}

// PullTo writes the artifact dgst names in repo onto path.
//
// The pull reference is repo@dgst. Resume semantics are bigoci's: a partial
// file lives beside path as path+".bigoci-partial". The adapter owns retries;
// the caller must not wrap this call.
func (c *Client) PullTo(ctx context.Context, repo string, dgst digest.Digest, path string) error {
	ref := bigoci.Reference(repo + "@" + dgst.String())
	if err := c.inner.Pull(ctx, ref, bigoci.ToFile(path), c.pullOptions()...); err != nil {
		return classify(err)
	}
	return nil
}

// clientOptions maps cfg onto public bigoci options. The unverified
// external-transport option is never included: a nil HTTPClient leaves
// bigoci's default verified stack in place, and a non-nil client is passed
// only through [bigoci.WithHTTPClient].
func clientOptions(cfg Config) []bigoci.Option {
	var opts []bigoci.Option
	if cfg.PlainHTTP {
		opts = append(opts, bigoci.WithPlainHTTP())
	}
	if cfg.Username != "" || cfg.Secret != "" {
		opts = append(opts, bigoci.WithCredentials(cfg.Username, cfg.Secret))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, bigoci.WithHTTPClient(cfg.HTTPClient))
	}
	return opts
}

// pushOptions is the per-call option list [Client.Push] hands to bigoci.
func (c *Client) pushOptions(partSize int64) []bigoci.PushOption {
	if partSize > 0 {
		return []bigoci.PushOption{bigoci.WithPartSize(bigoci.PartSize(partSize))}
	}
	return nil
}

// pullOptions is the per-call option list [Client.PullTo] hands to bigoci.
func (c *Client) pullOptions() []bigoci.PullOption {
	return nil
}

// classify maps public bigoci sentinels onto transfer sentinels. Unknown
// errors, including [bigoci.ErrPartTooLarge] and [bigoci.ErrNotBigociArtifact],
// pass through.
func classify(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bigoci.ErrNotFound):
		return fmt.Errorf("%w: %w", transfer.ErrNotFound, err)
	case errors.Is(err, bigoci.ErrUnauthorized):
		return fmt.Errorf("%w: %w", transfer.ErrUnauthorized, err)
	case errors.Is(err, bigoci.ErrDigestMismatch):
		return fmt.Errorf("%w: %w", transfer.ErrDigestMismatch, err)
	default:
		return err
	}
}

// bigociAPI is the public bigoci.Client surface this adapter calls. Tests
// substitute a fake; mockery is reserved for transfer ports.
type bigociAPI interface {
	PushByDigest(
		ctx context.Context,
		repo bigoci.Reference,
		src bigoci.FileSource,
		opts ...bigoci.PushOption,
	) (ocispec.Descriptor, error)
	Pull(ctx context.Context, ref bigoci.Reference, dest bigoci.FileDest, opts ...bigoci.PullOption) error
}
