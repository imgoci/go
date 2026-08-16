package imgoci

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/transfer"
)

const (
	// reservedAnnotationPrefix is the spec-reserved annotation namespace.
	reservedAnnotationPrefix = "io.imgoci."
)

// ReleaseSpec is the producer input to [Client.Publish].
type ReleaseSpec struct {
	// Name is io.imgoci.name.
	Name string
	// Version is org.opencontainers.image.version.
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
	// Selector is the five-field identity. Compression declares what Source
	// already is; v1 does not compress on the caller's behalf.
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
	// progress receives absolute snapshots, nil when nobody is watching.
	progress func(Progress)
}

// Publish publishes spec as a standard-form imgoci release at ref and
// returns the canonical index digest.
//
// Step 0 is the reference contract, before any I/O: Publish is tag-only.
// Digest-only is rejected because nothing would name the index.
// Tag+digest is rejected because it is a read binding with no defined write
// meaning; silently dropping the digest would be worse. Name-only (no tag)
// is rejected for the same reason as digest-only: there is no tag to PUT.
// An optimistic "publish only if the tag still points at X" precondition is
// a possible future feature, not smuggled into v1.
//
// Step 1 validates the spec against producer rules 1–8 before network:
// non-empty Name/Version, UTF-8 of every caller string, reserved io.imgoci.*
// keys, selector and filename grammar, duplicate five-tuples, required
// representation roles, incus-vm→incus, filename collisions, and shared-
// source consistency. Content digest/size/filename annotations are computed
// from Source; two FileSpecs naming the same Source path cannot carry
// different content annotations in v1 because callers cannot supply those
// annotations. MultipartSpec.PartSize must be >= 0; negative is
// [ErrInvalidSpec].
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
	ports, err := c.portsFor(ctx, parsed.host, parsed.repository)
	if err != nil {
		return "", err
	}
	dgst, err := transfer.Publish(ctx, transfer.Ports{
		Manifests: ports.manifests,
		Blobs:     ports.blobs,
		Multipart: ports.multipart,
	}, toPublishRequest(parsed.host+"/"+parsed.repository, parsed.tag, spec, settings))
	return dgst, mapPublishError(err)
}

// preparePublish applies options, the tag-only reference contract, and spec
// validation. Adapter construction must not run until this returns.
func preparePublish(ref Reference, spec ReleaseSpec, opts []PublishOption) (publishSettings, parsedRef, error) {
	var settings publishSettings
	if err := applyPublishOptions(&settings, opts); err != nil {
		return publishSettings{}, parsedRef{}, err
	}
	parsed, err := ref.parse()
	if err != nil {
		return publishSettings{}, parsedRef{}, err
	}
	if err := checkPublishRef(ref, parsed); err != nil {
		return publishSettings{}, parsedRef{}, err
	}
	if err := validateReleaseSpec(spec); err != nil {
		return publishSettings{}, parsedRef{}, err
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

// checkPublishRef enforces the tag-only publish contract.
func checkPublishRef(ref Reference, parsed parsedRef) error {
	switch {
	case parsed.tag != "" && parsed.digest == "":
		return nil
	case parsed.tag == "" && parsed.digest != "":
		return fmt.Errorf(
			"%w: digest-only reference %q cannot name a published index",
			ErrInvalidSpec, ref,
		)
	case parsed.tag != "" && parsed.digest != "":
		return fmt.Errorf(
			"%w: tag+digest reference %q has no defined write meaning",
			ErrInvalidSpec, ref,
		)
	default:
		return fmt.Errorf(
			"%w: publish reference %q must be tag-only",
			ErrInvalidSpec, ref,
		)
	}
}

// validateReleaseSpec applies producer rules 1–8 before any network I/O.
func validateReleaseSpec(spec ReleaseSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidSpec)
	}
	if spec.Version == "" {
		return fmt.Errorf("%w: version is empty", ErrInvalidSpec)
	}
	if err := checkUTF8Spec(spec); err != nil {
		return err
	}
	if err := checkReservedAnnotations(spec); err != nil {
		return err
	}
	if err := checkMultipartAndSources(spec); err != nil {
		return err
	}
	if err := checkProducerRules(spec); err != nil {
		return err
	}
	return nil
}

// checkUTF8Spec requires [utf8.ValidString] on every caller string.
func checkUTF8Spec(spec ReleaseSpec) error {
	if err := requireUTF8("name", spec.Name); err != nil {
		return err
	}
	if err := requireUTF8("version", spec.Version); err != nil {
		return err
	}
	if err := checkUTF8Map("root annotation", spec.Annotations); err != nil {
		return err
	}
	for i, file := range spec.Files {
		prefix := fmt.Sprintf("files[%d]", i)
		if err := requireUTF8(prefix+" filename", file.Filename); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" architecture", file.Selector.Architecture); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" target", file.Selector.Target); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" representation", file.Selector.Representation); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" role", file.Selector.Role); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" compression", file.Selector.Compression); err != nil {
			return err
		}
		if err := checkUTF8Map(prefix+" annotation", file.Annotations); err != nil {
			return err
		}
	}
	return nil
}

// checkUTF8Map requires UTF-8 keys and values.
func checkUTF8Map(label string, m map[string]string) error {
	for k, v := range m {
		if err := requireUTF8(label+" key", k); err != nil {
			return err
		}
		if err := requireUTF8(label+" value", v); err != nil {
			return err
		}
	}
	return nil
}

// requireUTF8 reports [ErrInvalidSpec] when s is not valid UTF-8.
func requireUTF8(field, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidSpec, field)
	}
	return nil
}

// checkReservedAnnotations rejects io.imgoci.* keys in caller maps.
func checkReservedAnnotations(spec ReleaseSpec) error {
	for k := range spec.Annotations {
		if strings.HasPrefix(k, reservedAnnotationPrefix) {
			return fmt.Errorf("%w: reserved annotation %q", ErrInvalidSpec, k)
		}
	}
	for i, file := range spec.Files {
		for k := range file.Annotations {
			if strings.HasPrefix(k, reservedAnnotationPrefix) {
				return fmt.Errorf("%w: files[%d] reserved annotation %q", ErrInvalidSpec, i, k)
			}
		}
	}
	return nil
}

// checkMultipartAndSources rejects a negative MultipartSpec.PartSize and
// inconsistent shared sources.
func checkMultipartAndSources(spec ReleaseSpec) error {
	byPath := make(map[string]string)
	for i, file := range spec.Files {
		if file.Multipart != nil && file.Multipart.PartSize < 0 {
			return fmt.Errorf("%w: files[%d]: multipart part size must be >= 0", ErrInvalidSpec, i)
		}
		path := file.Source.path
		if path == "" {
			return fmt.Errorf("%w: files[%d]: empty source", ErrInvalidSpec, i)
		}
		if prev, ok := byPath[path]; ok && prev != file.Selector.Compression {
			return fmt.Errorf(
				"%w: shared source %q has conflicting compression %q and %q",
				ErrInvalidSpec, path, prev, file.Selector.Compression,
			)
		}
		byPath[path] = file.Selector.Compression
	}
	return nil
}

// checkProducerRules runs [index.Build] on a placeholder model so selector
// grammar, required roles, duplicate five-tuples, incus-vm→incus, filename
// collisions, and rule 6's filename agreement surface as [ErrInvalidSpec]
// rather than [ErrInvalidIndex].
//
// Placeholder content digest and size are identical for every entry that
// shares (architecture, target, representation, role). That lets
// [index.Build] enforce rule 6's filename component without pretending to
// know content identity. Real content digest and size are checked after
// pass-1 hashing, before any network write.
func checkProducerRules(spec ReleaseSpec) error {
	entries := make([]index.ModelEntry, len(spec.Files))
	for i, file := range spec.Files {
		identityKey := strings.Join([]string{
			file.Selector.Architecture,
			file.Selector.Target,
			file.Selector.Representation,
			file.Selector.Role,
		}, "/")
		entries[i] = index.ModelEntry{
			Digest:        digest.FromBytes([]byte("manifest:" + strconv.Itoa(i))),
			Size:          1,
			Selector:      toIndexSelector(file.Selector),
			ContentDigest: digest.FromBytes([]byte("content:" + identityKey)),
			ContentSize:   0,
			Filename:      file.Filename,
			Annotations:   file.Annotations,
		}
	}
	_, err := index.Build(&index.Model{
		Name:        spec.Name,
		Version:     spec.Version,
		Annotations: spec.Annotations,
		Entries:     entries,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, err)
	}
	return nil
}

// toIndexSelector copies a public selector onto the index model type.
func toIndexSelector(s Selector) index.Selector {
	return index.Selector{
		Architecture:   s.Architecture,
		Target:         s.Target,
		Representation: s.Representation,
		Role:           s.Role,
		Compression:    s.Compression,
	}
}

// toPublishRequest maps a validated spec onto the transfer request.
// Annotation maps are cloned so later mutation of the caller's spec cannot
// change what was validated.
func toPublishRequest(repo, tag string, spec ReleaseSpec, settings publishSettings) transfer.PublishRequest {
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
		Tag:         tag,
		Name:        spec.Name,
		Version:     spec.Version,
		Annotations: maps.Clone(spec.Annotations),
		Entries:     entries,
		Workers:     settings.workers,
		Progress:    convertProgress(settings.progress),
		Repo:        repo,
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
