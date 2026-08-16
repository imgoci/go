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
	// entryIdx are request entry indexes that share this blob.
	entryIdx []int
	// manifestDigest is the file-manifest digest after BuildStandard.
	manifestDigest digest.Digest
	// manifestSize is the file-manifest byte length after BuildStandard.
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

// Publish performs ARCHITECTURE.md §5.1 steps 2–6: pass-1 hash, upload,
// index build, tag PUT last. Steps 0–1 (reference form and spec validation)
// belong to the root package and must already have passed.
//
// Pass 1 reads each unique SourcePath once, hashing stored bytes while
// teeing into [decomp.Decoder] so decoded bytes are hashed and counted.
// Producer strictness equals consumer strictness: a two-member gzip fails
// here. Unique stored digests then share one blob push and one manifest
// PUT, even when they came from different paths.
//
// Upload uses bounded workers. Each unique stored digest re-checks pass-1
// stat (size and mtime); a change is [ErrDigestMismatch]. Blobs.Exists
// skips a push; otherwise Blobs.Push gets a fresh file handle — the
// adapter's re-verifying reader is the net. The OCI empty-config blob
// ([filemanifest.EmptyConfigDigest]) is ensured once per call, before any
// Manifests.Put, because every file manifest references it.
// [filemanifest.BuildStandard] runs after the blob is present, then
// Manifests.Put of that digest-ref. Manifests always land after their blobs.
// The index PUT by tag is last, so nothing references the index until every
// manifest and blob has landed.
//
// After pass 1, entries that share (architecture, target, representation,
// role) must agree on real content digest, content size, and filename
// (spec §6 rule 6) before any network write. Unique stored digests that
// disagree on decoded content or compression fail as [ErrSharedBlob]
// (spec §6 rule 8) still before upload.
//
// [index.Build] already runs [index.Validate] (rules 1–9). Publish then
// Decode+Validate+VerifyCanonical on the encoded bytes as a cheap
// self-oracle that the published index would pass the consumer path,
// including rule 10. A self-oracle failure is a library defect: it does
// not wrap [index.ErrRule], so the public mapper cannot treat it as
// caller input.
func Publish(ctx context.Context, p Ports, req PublishRequest) (digest.Digest, error) {
	if err := checkPublishPorts(p, req); err != nil {
		return "", err
	}
	progress := newPublishReporter(len(req.Entries), req.Progress)

	hashed, err := hashSources(req.Entries)
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
	progress.setPhase(PhaseUpload)

	if err := ensureEmptyConfig(ctx, p.Blobs); err != nil {
		return "", err
	}
	if err := uploadBlobs(ctx, p, req.Workers, blobs, progress); err != nil {
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
func hashSources(entries []PublishEntry) (map[string]hashedFile, error) {
	out := make(map[string]hashedFile)
	for i, entry := range entries {
		if entry.SourcePath == "" {
			return nil, fmt.Errorf("entries[%d]: empty source path", i)
		}
		if _, ok := out[entry.SourcePath]; ok {
			continue
		}
		got, err := hashOne(entry)
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
func hashOne(entry PublishEntry) (hashedFile, error) {
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
	dec, err := decomp.Decoder(entry.Selector.Compression)(io.TeeReader(f, stored))
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
		out = append(out, *byDigest[dgst])
	}
	return out, nil
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
// does not already hold it. File manifests reference this digest, so it must
// exist before any Manifests.Put.
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
			record(uploadOne(ctx, p, blob, progress))
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

// uploadOne re-checks source stability, pushes the stored blob unless it
// already exists, then PUTs the standard file manifest at its digest.
func uploadOne(ctx context.Context, p Ports, blob *uniqueBlob, progress *reporter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkSourcesStable(blob); err != nil {
		return err
	}
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
	manifestOf := make(map[digest.Digest]digest.Digest, len(blobs))
	manifestSize := make(map[digest.Digest]int64, len(blobs))
	for _, blob := range blobs {
		manifestOf[blob.storedDigest] = blob.manifestDigest
		manifestSize[blob.storedDigest] = blob.manifestSize
	}
	entries := make([]index.ModelEntry, len(req.Entries))
	for i, entry := range req.Entries {
		h := hashed[entry.SourcePath]
		entries[i] = index.ModelEntry{
			Digest:        manifestOf[h.storedDigest],
			Size:          manifestSize[h.storedDigest],
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
// Decode+Validate+VerifyCanonical is a cheap self-oracle that the canonical
// bytes survive the same three seams [ParseIndex] uses, including rule 10.
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
