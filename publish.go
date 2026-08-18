package imgoci

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/ociref"
	"github.com/imgoci/go/internal/transfer"
)

// ReleaseSpec is the producer input to [Client.Publish].
type ReleaseSpec struct {
	// Name is io.imgoci.name. It must be a basic token: 1 to 128 ASCII
	// bytes matching ^[a-z0-9]+([._-][a-z0-9]+)*$ (spec §5.1 and §5.3).
	Name string
	// Version is org.opencontainers.image.version. It must contain 1 to 128
	// printable ASCII characters and no whitespace or control characters
	// (spec §5.1).
	Version string
	// Annotations are extra root annotations. Keys in the io.imgoci.*
	// namespace are reserved and rejected.
	Annotations map[string]string
	// Files are the stored files to publish.
	Files []FileSpec
}

// FileSpec is one stored file in a [ReleaseSpec].
type FileSpec struct {
	// Source is the path-backed stored file. It must not change during
	// Publish; see [FromFile].
	Source Source
	// Selector is the six-field identity. Compression declares what Source
	// already is; the library does not compress on the caller's behalf.
	Selector Selector
	// Filename is io.imgoci.filename.
	Filename string
	// Annotations are extra descriptor annotations. Keys in the io.imgoci.*
	// namespace are reserved and rejected; selector, content, and filename
	// fields are filled by the library.
	Annotations map[string]string
	// Multipart requests BigOCI publication. Nil is the standard form.
	Multipart *MultipartSpec
}

// MultipartSpec selects BigOCI part size. Zero means the bigoci default
// (512 MiB). A negative PartSize is [ErrInvalidSpec].
type MultipartSpec struct {
	// PartSize is the BigOCI part size in bytes. Zero means the bigoci default
	// (512 MiB). Must not be negative.
	PartSize int64
}

// PublishOption configures one [Client.Publish] call.
//
// The interface is sealed by an unexported method: the options are the ones
// this package ships.
type PublishOption interface {
	applyPublish(*publishSettings)
}

// publishSettings are the knobs one Publish call runs with.
type publishSettings struct {
	// workers is how many unique blobs upload at once. Zero means the
	// orchestrator default.
	workers int
	// workersSet reports whether [WithWorkers] was named.
	workersSet bool
	// progress receives absolute snapshots, nil when omitted.
	progress func(Progress)
}

// Publish publishes spec as a standard-form imgoci release at ref and
// returns the canonical index digest.
//
// Publish is tag-only. Digest-only is rejected because nothing would name the
// index. Tag+digest is rejected because it is a read binding with no defined
// write meaning; silently dropping the digest would be worse. Name-only (no
// tag) is rejected for the same reason as digest-only: there is no tag to PUT.
// These checks run before any I/O.
//
// The spec is validated against producer rules 1–8 before network: Name
// grammar (spec §5.1 and §5.3), Version grammar (spec §5.1), UTF-8 of every
// caller string, reserved io.imgoci.* keys, selector and filename grammar,
// duplicate six-field tuples, required representation roles, incus-vm→incus,
// filename collisions, and shared-source consistency.
// Content digest/size/filename annotations are computed from Source; two
// FileSpecs naming the same Source path cannot carry different content
// annotations because callers cannot supply those annotations.
// MultipartSpec.PartSize must be >= 0; negative is [ErrInvalidSpec].
func (c *Client) Publish(
	ctx context.Context,
	ref Reference,
	spec ReleaseSpec,
	opts ...PublishOption,
) (digest.Digest, error) {
	if c == nil {
		return "", errors.New("publish: nil client")
	}
	settings, parsed, err := preparePublish(ref, spec, opts)
	if err != nil {
		return "", err
	}
	ports, err := c.portsFor(ctx, parsed.Host, parsed.Repository)
	if err != nil {
		return "", err
	}
	dgst, err := transfer.Publish(ctx, transfer.Ports{
		Manifests: ports.Manifests,
		Blobs:     ports.Blobs,
		Multipart: ports.Multipart,
	}, toPublishRequest(parsed.Host+"/"+parsed.Repository, parsed.Tag, spec, settings, c.settings.decoderMaxWindow))
	return dgst, mapPublishError(err)
}

// preparePublish applies options, the tag-only reference contract, and spec
// validation. Adapter construction must not run until this returns.
func preparePublish(ref Reference, spec ReleaseSpec, opts []PublishOption) (publishSettings, ociref.Parsed, error) {
	var settings publishSettings
	if err := applyPublishOptions(&settings, opts); err != nil {
		return publishSettings{}, ociref.Parsed{}, err
	}
	parsed, err := ref.parse()
	if err != nil {
		return publishSettings{}, ociref.Parsed{}, err
	}
	if err := ociref.RequireTagOnly(string(ref), parsed); err != nil {
		return publishSettings{}, ociref.Parsed{}, fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	if err := validateReleaseSpec(spec); err != nil {
		return publishSettings{}, ociref.Parsed{}, err
	}
	return settings, parsed, nil
}

// applyPublishOptions records opts onto settings and rejects a non-positive
// worker count before any I/O.
func applyPublishOptions(settings *publishSettings, opts []PublishOption) error {
	for _, opt := range opts {
		if opt != nil {
			opt.applyPublish(settings)
		}
	}
	if settings.workersSet && settings.workers <= 0 {
		return fmt.Errorf("worker count must be positive, got %d", settings.workers)
	}
	return nil
}

// validateReleaseSpec applies producer rules 1–8 before any network I/O.
func validateReleaseSpec(spec ReleaseSpec) error {
	in := toProducerInput(spec)
	sources := toPublishSources(spec)
	if err := index.ValidateProducerFields(&in); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	if err := transfer.ValidatePublishSources(sources); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	if err := index.ValidateProducerRules(&in); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	return nil
}

// toProducerInput maps a public release spec onto the index producer input.
func toProducerInput(spec ReleaseSpec) index.ProducerInput {
	files := make([]index.ProducerFile, len(spec.Files))
	for i, file := range spec.Files {
		files[i] = index.ProducerFile{
			Selector:    toIndexSelector(file.Selector),
			Filename:    file.Filename,
			Annotations: file.Annotations,
		}
	}
	return index.ProducerInput{
		Name:        spec.Name,
		Version:     spec.Version,
		Annotations: spec.Annotations,
		Files:       files,
	}
}

// toPublishSources maps the spec's files onto the transfer source-validation view.
func toPublishSources(spec ReleaseSpec) []transfer.PublishSource {
	sources := make([]transfer.PublishSource, len(spec.Files))
	for i, file := range spec.Files {
		src := transfer.PublishSource{
			Path:        file.Source.path,
			Compression: file.Selector.Compression,
		}
		if file.Multipart != nil {
			src.PartSize = file.Multipart.PartSize
			src.Multipart = true
		}
		sources[i] = src
	}
	return sources
}

// toIndexSelector copies a public selector onto the index model type.
func toIndexSelector(s Selector) index.Selector {
	return index.Selector{
		Architecture:   s.Architecture,
		Target:         s.Target,
		Representation: s.Representation,
		Usage:          s.Usage.String(),
		Role:           s.Role,
		Compression:    s.Compression,
	}
}

// toPublishRequest maps a validated spec onto the transfer request.
// Annotation maps are cloned so later mutation of the caller's spec cannot
// change what was validated. maxWindow is the client-wide decoder ceiling that
// pass-1 strict decode applies, so a producer cannot publish a release this
// client refuses to read back.
func toPublishRequest(
	repo, tag string,
	spec ReleaseSpec,
	settings publishSettings,
	maxWindow uint64,
) transfer.PublishRequest {
	entries := make([]transfer.PublishEntry, len(spec.Files))
	for i, file := range spec.Files {
		entry := transfer.PublishEntry{
			SourcePath:  file.Source.path,
			Selector:    toIndexSelector(file.Selector),
			Filename:    file.Filename,
			Annotations: maps.Clone(file.Annotations),
		}
		if file.Multipart != nil {
			entry.Multipart = &transfer.MultipartPlan{PartSize: file.Multipart.PartSize}
		}
		entries[i] = entry
	}
	return transfer.PublishRequest{
		Tag:              tag,
		Name:             spec.Name,
		Version:          spec.Version,
		Annotations:      maps.Clone(spec.Annotations),
		Entries:          entries,
		Workers:          settings.workers,
		Progress:         convertProgress(settings.progress),
		Repo:             repo,
		DecoderMaxWindow: maxWindow,
	}
}

// mapPublishError maps orchestrator sentinels onto the public surface.
//
// [transfer.ErrDigestMismatch] includes the upload-divergence and source-
// mutation cases. Decompression failures follow the FetchFiles mapping.
// [index.ErrRule] from caller-derived [index.Build]/[index.Validate] and
// [transfer.ErrSharedBlob] (rule 8 on shared stored bytes) become
// [ErrInvalidSpec], never [ErrInvalidIndex]. Index self-oracle failures
// do not wrap [index.ErrRule] and stay unclassified.
func mapPublishError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, index.ErrRule) || errors.Is(err, transfer.ErrSharedBlob) {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	return mapFetchError(err)
}
