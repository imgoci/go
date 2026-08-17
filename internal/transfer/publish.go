package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sync"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
)

const (
	// defaultBigOCIPartSize is github.com/imgoci/bigoci.DefaultPartSize: 512 MiB.
	// Copied so this package does not import the adapter library. Zero
	// [MultipartPlan.PartSize] resolves to this value when counting planned parts;
	// planned parts below two fall back to the standard path.
	defaultBigOCIPartSize int64 = 512 << 20
	// minMultipartParts is the imgoci BigOCI profile floor (spec §8).
	minMultipartParts int64 = 2
	// maxBigOCIParts is github.com/imgoci/bigoci/internal/plan.MaxParts: 4096.
	// Copied so this package does not import the adapter library, matching
	// [defaultBigOCIPartSize]. Planned parts above this ceiling are a caller-input
	// error before any network I/O.
	maxBigOCIParts int64 = 4096
)

// PublishRequest is the input to [Publish]. Root validation (reference form
// and producer rules 1–8) runs before this is built.
type PublishRequest struct {
	// Tag is the repository tag the index is published under. Required.
	Tag string
	// Name is io.imgoci.name.
	Name string
	// Version is org.opencontainers.image.version.
	Version string
	// Annotations are extra root annotations. Reserved io.imgoci.* keys are
	// rejected by the root package before this request is built.
	Annotations map[string]string
	// Entries are the files to publish. Order is caller order; [index.Build]
	// sorts by the five-field tuple.
	Entries []PublishEntry
	// Workers is the maximum concurrent unique-blob uploads. Values <= 0
	// become 4. The effective count never exceeds the number of unique
	// stored digests.
	Workers int
	// Progress receives serialized absolute snapshots. It may be nil.
	Progress func(Progress)
	// Repo is the repository-only reference (registry/name) passed to
	// [Multipart.Push]. Required when any entry takes the multipart path.
	// Manifests and Blobs are already bound to this repository at
	// construction; Multipart is not.
	Repo string
	// DecoderMaxWindow caps the working set the pass-1 strict decode may
	// allocate, the same ceiling a fetch of this release will apply. Zero
	// means [decomp.DefaultDecoderMaxWindow]; the root package always sets
	// it from the client option.
	DecoderMaxWindow uint64
}

// PublishEntry is one file-entry [Publish] hashes, uploads, and indexes.
type PublishEntry struct {
	// SourcePath is the path-backed stored file. A Source must not change
	// during Publish; pass-1 stat is re-checked before upload.
	SourcePath string
	// Selector is the five-field transport-alternative identity. Compression
	// declares what the stored file already is.
	Selector index.Selector
	// Filename is io.imgoci.filename.
	Filename string
	// Annotations are extra descriptor annotations. Selector, content, and
	// filename fields overwrite the corresponding keys in [index.Build].
	Annotations map[string]string
	// Multipart requests BigOCI publication. Nil is the standard form.
	// A non-nil plan with fewer than two planned parts falls back to the
	// standard path and increments [Progress.Fallbacks].
	Multipart *MultipartPlan
}

// MultipartPlan selects BigOCI part size for one [PublishEntry].
type MultipartPlan struct {
	// PartSize is the split size in bytes. Zero selects the bigoci default
	// ([defaultBigOCIPartSize], 512 MiB).
	PartSize int64
}

// hashedFile is pass-1 output for one unique source path.
type hashedFile struct {
	// path is the source path that was hashed.
	path string
	// size is os.Stat size captured at pass 1.
	size int64
	// mtime is os.Stat mtime captured at pass 1.
	mtime time.Time
	// storedDigest is SHA-256 of the stored bytes.
	storedDigest digest.Digest
	// storedSize is the stored byte length.
	storedSize int64
	// contentDigest is SHA-256 of the decoded bytes.
	contentDigest digest.Digest
	// contentSize is the decoded byte length.
	contentSize int64
	// compression is the decoder applied during pass 1.
	compression string
}

// uniqueBlob is one stored digest to upload, covering every entry that
// hashed to it.
type uniqueBlob struct {
	// storedDigest is the blob digest.
	storedDigest digest.Digest
	// storedSize is the blob size.
	storedSize int64
	// contentDigest is the decoded digest shared by every entry on this blob.
	contentDigest digest.Digest
	// contentSize is the decoded size shared by every entry on this blob.
	contentSize int64
	// compression is the decoder name shared by every entry on this blob.
	compression string
	// paths are the unique source paths that produced this digest.
	paths []string
	// stats are pass-1 stats keyed by path, used for the mutation re-check.
	stats map[string]hashedFile
	// entryIdx are request entry indexes that share this blob and form.
	entryIdx []int
	// multipart is the BigOCI plan when this unit takes the multipart path.
	// Nil means the standard blob+manifest path.
	multipart *MultipartPlan
	// mediaType is the index descriptor mediaType after upload. Empty means
	// [index.MediaTypeManifest].
	mediaType string
	// artifactType is the index descriptor artifactType after upload. Empty
	// means [index.ArtifactTypeFile].
	artifactType string
	// manifestDigest is the file-manifest digest after BuildStandard or
	// multipart Push.
	manifestDigest digest.Digest
	// manifestSize is the file-manifest byte length after BuildStandard or
	// multipart Push.
	manifestSize int64
}

// hashCounter hashes and counts written bytes with no content-size ceiling.
type hashCounter struct {
	// h is the running SHA-256.
	h hash.Hash
	// n is how many bytes have been written.
	n int64
}

// Write hashes p and counts it.
func (h *hashCounter) Write(p []byte) (int, error) {
	n, err := h.h.Write(p)
	h.n += int64(n)
	return n, err
}

// digest is SHA-256 of every successfully written byte.
func (h *hashCounter) digest() digest.Digest {
	return digest.NewDigest(digest.SHA256, h.h)
}

// Publish hashes unique sources, uploads unique stored blobs and file
// manifests, then PUTs the release index by tag last. Reference-form and spec
// validation belong to the root package and must already have passed.
//
// Pass 1 reads each unique SourcePath once, hashing stored bytes while teeing
// into [decomp.Decoder] so decoded bytes are hashed and counted with the same
// strictness as fetch. Unique stored digests share one blob push and one
// manifest PUT, even when they came from different paths. After pass 1,
// entries that share (architecture, target, representation, role) must agree
// on content digest, content size, and filename (spec §6 rule 6) before any
// network write. Unique stored digests that disagree on decoded content or
// compression fail as [ErrSharedBlob] (spec §6 rule 8), also before any
// network write.
//
// Upload uses bounded workers. Each unique stored digest re-checks pass-1 stat
// (size and mtime); a change is [ErrDigestMismatch]. A non-nil [MultipartPlan]
// takes the BigOCI path when planned parts are at least two and at most
// [maxBigOCIParts]: [Multipart.Push] (no tag), then Manifests.Get of the
// returned digest, then [filemanifest.ValidateBigOCI] requiring
// io.bigoci.file.digest and io.bigoci.file.size to equal pass-1 stored
// identity. Fewer than two planned parts falls back to the standard path and
// increments [Progress.Fallbacks]. More than [maxBigOCIParts] planned parts is
// a caller-input error wrapping [index.ErrRule] before any network write.
//
// On the standard path, Blobs.Exists skips a push; otherwise Blobs.Push gets a
// fresh file handle. The OCI empty-config blob
// ([filemanifest.EmptyConfigDigest]) is ensured once before any standard
// Manifests.Put. BigOCI pushes its own config. Manifests land after their
// blobs. The index PUT by tag is last, so nothing references the index until
// every manifest and blob has landed.
func Publish(ctx context.Context, p Ports, req PublishRequest) (digest.Digest, error) {
	if err := checkPublishPorts(p, req); err != nil {
		return "", err
	}
	if err := checkMultipartPartCeiling(req.Entries, statSize); err != nil {
		return "", err
	}
	progress := newPublishReporter(len(req.Entries), req.Progress)
	ctx = progress.bindContext(ctx)

	hashed, err := hashSources(req.Entries, decoderMaxWindow(req.DecoderMaxWindow))
	if err != nil {
		return "", err
	}
	if err = checkFileIdentity(req.Entries, hashed); err != nil {
		return "", err
	}
	blobs, err := groupBlobs(req.Entries, hashed)
	if err != nil {
		return "", err
	}
	progress.setTotalBytes(totalContentBytes(req.Entries, hashed))
	progress.addFallbacks(applyMultipartFallback(blobs))
	progress.setPhase(PhaseUpload)

	if hasStandardUpload(blobs) {
		if err := ensureEmptyConfig(ctx, p.Blobs); err != nil {
			return "", err
		}
	}
	if err := uploadBlobs(ctx, p, req.Repo, req.Workers, blobs, progress); err != nil {
		return "", err
	}
	return putIndex(ctx, p, req, hashed, blobs, progress)
}

// checkPublishPorts reports missing adapters, tag, or entries.
func checkPublishPorts(p Ports, req PublishRequest) error {
	if p.Manifests == nil || p.Blobs == nil {
		return errors.New("publish: ports are incomplete")
	}
	if req.Tag == "" {
		return errors.New("publish: tag is required")
	}
	if len(req.Entries) == 0 {
		return errors.New("publish: no entries")
	}
	return nil
}

// hashSources runs pass 1 once per unique SourcePath.
func hashSources(entries []PublishEntry, maxWindow uint64) (map[string]hashedFile, error) {
	out := make(map[string]hashedFile)
	for i, entry := range entries {
		if entry.SourcePath == "" {
			return nil, fmt.Errorf("entries[%d]: empty source path", i)
		}
		if _, ok := out[entry.SourcePath]; ok {
			continue
		}
		got, err := hashOne(entry, maxWindow)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", entry.SourcePath, err)
		}
		out[entry.SourcePath] = got
	}
	return out, nil
}

// checkFileIdentity enforces spec §6 rule 6 against pass-1 content identity.
// Entries sharing (architecture, target, representation, role) must agree on
// content digest, content size, and filename. This runs after hashing and
// before any network write. The error wraps [index.ErrRule] so the public
// mapper can classify it as caller input.
func checkFileIdentity(entries []PublishEntry, hashed map[string]hashedFile) error {
	type fileID struct {
		architecture, target, representation, role string
	}
	type fileContent struct {
		digest   digest.Digest
		size     int64
		filename string
	}
	seen := make(map[fileID]fileContent)
	for _, entry := range entries {
		h := hashed[entry.SourcePath]
		id := fileID{
			architecture:   entry.Selector.Architecture,
			target:         entry.Selector.Target,
			representation: entry.Selector.Representation,
			role:           entry.Selector.Role,
		}
		got := fileContent{digest: h.contentDigest, size: h.contentSize, filename: entry.Filename}
		prev, ok := seen[id]
		if !ok {
			seen[id] = got
			continue
		}
		if prev.digest != got.digest || prev.size != got.size || prev.filename != got.filename {
			return fmt.Errorf(
				"transport alternatives for file %s, %s, %s, %s must have the same content digest, content size, and filename: %w",
				id.architecture,
				id.target,
				id.representation,
				id.role,
				index.ErrRule,
			)
		}
	}
	return nil
}

// hashOne stats, reads, and strictly decodes one source path.
func hashOne(entry PublishEntry, maxWindow uint64) (hashedFile, error) {
	fi, err := os.Stat(entry.SourcePath)
	if err != nil {
		return hashedFile{}, err
	}
	f, err := os.Open(entry.SourcePath)
	if err != nil {
		return hashedFile{}, err
	}
	defer f.Close()

	stored := &hashCounter{h: digest.SHA256.Hash()}
	dec, err := decomp.Decoder(entry.Selector.Compression, maxWindow)(io.TeeReader(f, stored))
	if err != nil {
		return hashedFile{}, err
	}
	content := &hashCounter{h: digest.SHA256.Hash()}
	if _, copyErr := io.Copy(content, dec); copyErr != nil {
		_ = dec.Close()
		return hashedFile{}, copyErr
	}
	if err := dec.Close(); err != nil {
		return hashedFile{}, err
	}
	return hashedFile{
		path:          entry.SourcePath,
		size:          fi.Size(),
		mtime:         fi.ModTime(),
		storedDigest:  stored.digest(),
		storedSize:    stored.n,
		contentDigest: content.digest(),
		contentSize:   content.n,
		compression:   entry.Selector.Compression,
	}, nil
}

// groupBlobs collapses hashed paths onto unique stored digests.
func groupBlobs(entries []PublishEntry, hashed map[string]hashedFile) ([]uniqueBlob, error) {
	order := make([]digest.Digest, 0)
	byDigest := make(map[digest.Digest]*uniqueBlob)
	for i, entry := range entries {
		h := hashed[entry.SourcePath]
		blob, ok := byDigest[h.storedDigest]
		if !ok {
			blob = &uniqueBlob{
				storedDigest:  h.storedDigest,
				storedSize:    h.storedSize,
				contentDigest: h.contentDigest,
				contentSize:   h.contentSize,
				compression:   h.compression,
				paths:         []string{h.path},
				stats:         map[string]hashedFile{h.path: h},
			}
			byDigest[h.storedDigest] = blob
			order = append(order, h.storedDigest)
		}
		if err := blob.accept(h, i); err != nil {
			return nil, err
		}
	}
	out := make([]uniqueBlob, 0, len(order))
	for _, dgst := range order {
		out = append(out, splitByForm(*byDigest[dgst], entries)...)
	}
	return out, nil
}

// formID distinguishes standard vs BigOCI upload units that share stored bytes.
type formID struct {
	// multipart is true when the entry requested BigOCI publication.
	multipart bool
	// partSize is [MultipartPlan.PartSize]; ignored when multipart is false.
	partSize int64
}

// entryForm is the upload-form identity of one request entry.
func entryForm(entry PublishEntry) formID {
	if entry.Multipart == nil {
		return formID{}
	}
	return formID{multipart: true, partSize: entry.Multipart.PartSize}
}

// splitByForm expands one digest-grouped blob into one upload unit per form.
// Rule 8 already ran across every path that hashed to the stored digest.
func splitByForm(blob uniqueBlob, entries []PublishEntry) []uniqueBlob {
	groups := make(map[formID][]int)
	order := make([]formID, 0)
	for _, idx := range blob.entryIdx {
		id := entryForm(entries[idx])
		if _, ok := groups[id]; !ok {
			order = append(order, id)
		}
		groups[id] = append(groups[id], idx)
	}
	out := make([]uniqueBlob, 0, len(order))
	for _, id := range order {
		out = append(out, blobForEntries(blob, groups[id], entries))
	}
	return out
}

// blobForEntries copies src into an upload unit covering idxs.
func blobForEntries(src uniqueBlob, idxs []int, entries []PublishEntry) uniqueBlob {
	out := uniqueBlob{
		storedDigest:  src.storedDigest,
		storedSize:    src.storedSize,
		contentDigest: src.contentDigest,
		contentSize:   src.contentSize,
		compression:   src.compression,
		entryIdx:      idxs,
		stats:         make(map[string]hashedFile, len(idxs)),
	}
	if entries[idxs[0]].Multipart != nil {
		plan := *entries[idxs[0]].Multipart
		out.multipart = &plan
	}
	seen := make(map[string]struct{}, len(idxs))
	for _, idx := range idxs {
		path := entries[idx].SourcePath
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out.paths = append(out.paths, path)
		out.stats[path] = src.stats[path]
	}
	return out
}

// applyMultipartFallback clears [uniqueBlob.multipart] when planned parts
// are fewer than [minMultipartParts] and returns how many units fell back.
func applyMultipartFallback(blobs []uniqueBlob) int {
	n := 0
	for i := range blobs {
		if blobs[i].multipart == nil {
			continue
		}
		if plannedParts(blobs[i].storedSize, blobs[i].multipart.PartSize) >= minMultipartParts {
			continue
		}
		blobs[i].multipart = nil
		n++
	}
	return n
}

// checkMultipartPartCeiling rejects a multipart entry whose planned part count
// exceeds [maxBigOCIParts]. sizeOf supplies the stored size; tests inject a
// synthetic size so an 8 GiB fixture is not materialized. The check runs before
// pass-1 hashing. The error wraps [index.ErrRule] so the public mapper
// classifies it as caller input.
func checkMultipartPartCeiling(entries []PublishEntry, sizeOf func(string) (int64, error)) error {
	for _, entry := range entries {
		if entry.Multipart == nil {
			continue
		}
		size, err := sizeOf(entry.SourcePath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", entry.SourcePath, err)
		}
		n := plannedParts(size, entry.Multipart.PartSize)
		if n > maxBigOCIParts {
			return fmt.Errorf(
				"multipart part count %d exceeds %d for stored size %d: %w",
				n,
				maxBigOCIParts,
				size,
				index.ErrRule,
			)
		}
	}
	return nil
}

// statSize returns the on-disk size of path.
func statSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// hasStandardUpload reports whether any unit will use the standard blob path.
func hasStandardUpload(blobs []uniqueBlob) bool {
	for _, blob := range blobs {
		if blob.multipart == nil {
			return true
		}
	}
	return false
}

// effectivePartSize resolves a [MultipartPlan.PartSize] of 0 to
// [defaultBigOCIPartSize] (bigoci.DefaultPartSize, 512 MiB).
func effectivePartSize(partSize int64) int64 {
	if partSize > 0 {
		return partSize
	}
	return defaultBigOCIPartSize
}

// plannedParts is ceil(storedSize / effectivePartSize). A zero stored size
// is zero parts.
func plannedParts(storedSize, partSize int64) int64 {
	size := effectivePartSize(partSize)
	if storedSize <= 0 {
		return 0
	}
	return (storedSize + size - 1) / size
}

// accept records one entry onto a unique stored digest, requiring content
// identity and compression to agree across paths that share the bytes.
func (b *uniqueBlob) accept(h hashedFile, idx int) error {
	if h.storedDigest != b.storedDigest ||
		h.contentDigest != b.contentDigest ||
		h.contentSize != b.contentSize ||
		h.compression != b.compression {
		return fmt.Errorf(
			"%w: stored digest %s disagrees on content digest, size, or compression",
			ErrSharedBlob,
			h.storedDigest,
		)
	}
	if _, ok := b.stats[h.path]; !ok {
		b.paths = append(b.paths, h.path)
		b.stats[h.path] = h
	}
	b.entryIdx = append(b.entryIdx, idx)
	return nil
}

// totalContentBytes sums decoded sizes across request entries.
func totalContentBytes(entries []PublishEntry, hashed map[string]hashedFile) int64 {
	var n int64
	for _, entry := range entries {
		n += hashed[entry.SourcePath].contentSize
	}
	return n
}

// ensureEmptyConfig pushes the OCI empty-config blob once when the repository
// does not already hold it. Standard file manifests reference this digest, so
// it must exist before any standard Manifests.Put. BigOCI pushes its own
// config; this helper is standard-path-only.
func ensureEmptyConfig(ctx context.Context, blobs Blobs) error {
	exists, err := blobs.Exists(ctx, filemanifest.EmptyConfigDigest)
	if err != nil {
		return fmt.Errorf("empty-config exists %s: %w", filemanifest.EmptyConfigDigest, err)
	}
	if exists {
		return nil
	}
	if err := blobs.Push(
		ctx,
		filemanifest.EmptyConfigDigest,
		filemanifest.EmptyConfigSize,
		bytes.NewReader([]byte("{}")),
	); err != nil {
		return fmt.Errorf("push empty-config %s: %w", filemanifest.EmptyConfigDigest, err)
	}
	return nil
}

// uploadBlobs pushes unique stored blobs then their manifests, with bounded
// workers. Manifests are never PUT before their blob is present.
func uploadBlobs(
	ctx context.Context,
	p Ports,
	repo string,
	workers int,
	blobs []uniqueBlob,
	progress *reporter,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		firstErr = make(chan error, 1)
	)
	record := func(err error) {
		if err == nil {
			return
		}
		cancel()
		select {
		case firstErr <- err:
		default:
		}
	}

	sem := make(chan struct{}, workerCount(workers, len(blobs)))
	for i := range blobs {
		blob := &blobs[i]
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			record(uploadOne(ctx, p, repo, blob, progress))
		})
	}
	wg.Wait()
	select {
	case err := <-firstErr:
		return err
	default:
		return nil
	}
}

// uploadOne re-checks source stability, then publishes via the multipart or
// standard path.
func uploadOne(ctx context.Context, p Ports, repo string, blob *uniqueBlob, progress *reporter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkSourcesStable(blob); err != nil {
		return err
	}
	if blob.multipart != nil {
		return uploadMultipart(ctx, p, repo, blob, progress)
	}
	return uploadStandard(ctx, p, blob, progress)
}

// uploadStandard pushes the stored blob unless it already exists, then PUTs
// the standard file manifest at its digest.
func uploadStandard(ctx context.Context, p Ports, blob *uniqueBlob, progress *reporter) error {
	exists, err := p.Blobs.Exists(ctx, blob.storedDigest)
	if err != nil {
		return fmt.Errorf("blob exists %s: %w", blob.storedDigest, err)
	}
	if !exists {
		if pushErr := pushStored(ctx, p.Blobs, blob); pushErr != nil {
			return pushErr
		}
		progress.addWire(blob.storedSize)
	}
	man, err := filemanifest.BuildStandard(filemanifest.BuildInput{
		LayerDigest: blob.storedDigest,
		LayerSize:   blob.storedSize,
	})
	if err != nil {
		return fmt.Errorf("build file manifest: %w", err)
	}
	blob.mediaType = index.MediaTypeManifest
	blob.artifactType = index.ArtifactTypeFile
	blob.manifestDigest = digest.FromBytes(man)
	blob.manifestSize = int64(len(man))
	if err := p.Manifests.Put(ctx, blob.manifestDigest.String(), index.MediaTypeManifest, man); err != nil {
		return fmt.Errorf("put file manifest %s: %w", blob.manifestDigest, err)
	}
	for range blob.entryIdx {
		progress.entryVerified(blob.contentSize)
	}
	return nil
}

// uploadMultipart publishes via [Multipart.Push] (PushByDigest; no tag) and
// re-fetches the returned manifest by descriptor digest, requiring the BigOCI
// whole-file digest and size to equal pass-1 stored identity.
func uploadMultipart(ctx context.Context, p Ports, repo string, blob *uniqueBlob, progress *reporter) error {
	if p.Multipart == nil {
		return errors.New("publish: multipart adapter is required")
	}
	if repo == "" {
		return errors.New("publish: repository is required for multipart")
	}
	desc, err := p.Multipart.Push(ctx, repo, blob.paths[0], blob.multipart.PartSize, progress.multipartReport())
	if err != nil {
		return fmt.Errorf("multipart push %s: %w", blob.paths[0], err)
	}
	accept := desc.MediaType
	if accept == "" {
		accept = index.MediaTypeManifest
	}
	raw, _, err := p.Manifests.Get(ctx, desc.Digest.String(), accept)
	if err != nil {
		return fmt.Errorf("get bigoci manifest %s: %w", desc.Digest, err)
	}
	if digest.FromBytes(raw) != desc.Digest {
		return fmt.Errorf("bigoci manifest %s: %w", desc.Digest, ErrDigestMismatch)
	}
	profile, err := filemanifest.ValidateBigOCI(raw)
	if err != nil {
		return fmt.Errorf("bigoci profile %s: %w: %w", desc.Digest, ErrInvalidDocument, err)
	}
	if profile.FileDigest != blob.storedDigest || profile.FileSize != blob.storedSize {
		return fmt.Errorf(
			"bigoci file digest %s size %d != stored %s size %d: %w",
			profile.FileDigest,
			profile.FileSize,
			blob.storedDigest,
			blob.storedSize,
			ErrDigestMismatch,
		)
	}
	blob.mediaType = index.MediaTypeManifest
	blob.artifactType = index.ArtifactTypeBigOCI
	blob.manifestDigest = desc.Digest
	// raw is the verified document; do not trust desc.Size.
	blob.manifestSize = int64(len(raw))
	for range blob.entryIdx {
		progress.entryVerified(blob.contentSize)
	}
	return nil
}

// checkSourcesStable re-stats every path that produced blob and fails when
// size or mtime diverged from pass 1.
func checkSourcesStable(blob *uniqueBlob) error {
	for _, path := range blob.paths {
		want := blob.stats[path]
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		if fi.Size() != want.size || !fi.ModTime().Equal(want.mtime) {
			return fmt.Errorf(
				"source %q mutated between hash and upload: %w",
				path,
				ErrDigestMismatch,
			)
		}
	}
	return nil
}

// pushStored opens a fresh handle and pushes the stored blob.
func pushStored(ctx context.Context, blobs Blobs, blob *uniqueBlob) error {
	path := blob.paths[0]
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := blobs.Push(ctx, blob.storedDigest, blob.storedSize, f); err != nil {
		return fmt.Errorf("push blob %s: %w", blob.storedDigest, err)
	}
	return nil
}

// putIndex builds the canonical release index, self-oracles it, and PUTs it
// by tag last.
func putIndex(
	ctx context.Context,
	p Ports,
	req PublishRequest,
	hashed map[string]hashedFile,
	blobs []uniqueBlob,
	progress *reporter,
) (digest.Digest, error) {
	model := indexModel(req, hashed, blobs)
	raw, err := index.Build(model)
	if err != nil {
		return "", fmt.Errorf("build index: %w", err)
	}
	if err := oracleIndex(raw); err != nil {
		return "", err
	}
	if err := p.Manifests.Put(ctx, req.Tag, index.MediaTypeIndex, raw); err != nil {
		return "", fmt.Errorf("put index: %w", err)
	}
	progress.finishIndex()
	return digest.FromBytes(raw), nil
}

// indexModel fills [index.ModelEntry] values from hashed sources and uploaded
// manifests.
func indexModel(req PublishRequest, hashed map[string]hashedFile, blobs []uniqueBlob) *index.Model {
	type uploaded struct {
		digest       digest.Digest
		size         int64
		mediaType    string
		artifactType string
	}
	byEntry := make(map[int]uploaded, len(req.Entries))
	for _, blob := range blobs {
		for _, idx := range blob.entryIdx {
			byEntry[idx] = uploaded{
				digest:       blob.manifestDigest,
				size:         blob.manifestSize,
				mediaType:    blob.mediaType,
				artifactType: blob.artifactType,
			}
		}
	}
	entries := make([]index.ModelEntry, len(req.Entries))
	for i, entry := range req.Entries {
		h := hashed[entry.SourcePath]
		man := byEntry[i]
		entries[i] = index.ModelEntry{
			MediaType:     man.mediaType,
			ArtifactType:  man.artifactType,
			Digest:        man.digest,
			Size:          man.size,
			Selector:      entry.Selector,
			ContentDigest: h.contentDigest,
			ContentSize:   h.contentSize,
			Filename:      entry.Filename,
			Annotations:   entry.Annotations,
		}
	}
	return &index.Model{
		Name:        req.Name,
		Version:     req.Version,
		Annotations: req.Annotations,
		Entries:     entries,
	}
}

// oracleError is a library invariant: encoded index bytes failed the
// consumer path. It does not unwrap [index.ErrRule], so mapPublishError
// cannot mistake a self-oracle defect for caller input.
type oracleError struct {
	// op is the consumer-path seam that failed: decode, validate, or canonical.
	op string
	// msg is the inner error text, not an unwrap target.
	msg string
}

// Error renders the self-oracle diagnosis.
func (e oracleError) Error() string {
	return "index self-oracle " + e.op + ": " + e.msg
}

// oracleIndex checks that encoded index bytes would pass the consumer path.
//
// [index.Build] already ran [index.Validate] (rules 1–9) on the model.
// Decode+Validate+VerifyCanonical is a self-oracle that the canonical bytes
// survive the same three seams the consumer path uses, including rule 10.
// Failures are [oracleError]: library defects, not caller input.
func oracleIndex(raw []byte) error {
	v, err := index.Decode(raw)
	if err != nil {
		return oracleError{op: "decode", msg: err.Error()}
	}
	if err := index.Validate(v); err != nil {
		return oracleError{op: "validate", msg: err.Error()}
	}
	if err := index.VerifyCanonical(raw); err != nil {
		return oracleError{op: "canonical", msg: err.Error()}
	}
	return nil
}
