package transfer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/mock"

	"github.com/imgoci/go/internal/decomp"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

// gzipMTimeOffset is the byte offset of the gzip header MTIME field, which the
// stdlib gzip decoder reads but never validates.
const gzipMTimeOffset = 4

// flipGzipMTime flips one bit of the gzip header MTIME. The result is the same
// length and still decodes to the same content, so it is only detectable as a
// layer-digest mismatch.
func flipGzipMTime(stored []byte) []byte {
	out := bytes.Clone(stored)
	if len(out) > gzipMTimeOffset {
		out[gzipMTimeOffset] ^= 0x01
	}
	return out
}

// flipFirstStoredByte flips the first stored byte, keeping the declared length.
func flipFirstStoredByte(stored []byte) []byte {
	out := bytes.Clone(stored)
	if len(out) > 0 {
		out[0] ^= 0xff
	}
	return out
}

// portBlobReader models the [Blobs] port contract that Pull returns a
// digest-verifying stream: the registry adapter wraps go-oci-blob's verified
// reader and maps a mismatch onto [ErrDigestMismatch]. Bytes are hashed as
// they are read and the digest is checked when the source runs out, which is
// the EOF [decomp.BoundedReader] probes for at the declared layer size.
type portBlobReader struct {
	// r serves the blob bytes the registry returned.
	r *bytes.Reader
	// want is the digest the layer descriptor declared.
	want digest.Digest
	// digester hashes the bytes actually handed to the caller.
	digester digest.Digester
	// mismatched records that the digest check ran and rejected the blob.
	mismatched *atomic.Bool
}

// newPortBlobReader returns a digest-verifying reader over served that reports
// a mismatch against want in place of the terminal [io.EOF].
func newPortBlobReader(served []byte, want digest.Digest, mismatched *atomic.Bool) io.ReadCloser {
	return &portBlobReader{
		r:          bytes.NewReader(served),
		want:       want,
		digester:   digest.Canonical.Digester(),
		mismatched: mismatched,
	}
}

// Read hashes what it returns and substitutes a layer-digest mismatch for the
// terminal [io.EOF].
func (p *portBlobReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		_, _ = p.digester.Hash().Write(b[:n])
	}
	if errors.Is(err, io.EOF) {
		if got := p.digester.Digest(); got != p.want {
			p.mismatched.Store(true)
			return n, fmt.Errorf("layer blob %s is %s: %w", p.want, got, ErrDigestMismatch)
		}
	}
	return n, err
}

// Close releases the in-memory stream.
func (p *portBlobReader) Close() error {
	return nil
}

// standardIntegrityResult is what one row of the standard-path integrity table
// observed while fetching.
type standardIntegrityResult struct {
	// err is the public error the fetch returned.
	err error
	// dest is the destination path the row wrote to.
	dest string
	// mismatched records whether the digest-verifying blob reader ran and
	// rejected the layer blob.
	mismatched *atomic.Bool
	// blobs is the blob port mock whose call log proves whether the layer
	// blob was pulled.
	blobs *regmocks.MockBlobs
	// mu guards snaps.
	mu *sync.Mutex
	// snaps is every progress snapshot the fetch emitted.
	snaps *[]Progress
}

// standardIntegrityCase is one row of the spec §8 standard-path integrity
// table driven by TestFetchFilesStandardIntegrityBoundaries: an honest fixture
// for one compression with a single declared value moved, plus the check that
// must reject it.
type standardIntegrityCase struct {
	// name names the row.
	name string
	// fixture builds the honest fixture for the row's compression.
	fixture func(*testing.T, string, []byte) fileFixture
	// mutate moves one declared value on the entry, leaving the
	// retrieved manifest and the served blob honest.
	mutate func(*Entry)
	// served returns the blob bytes the registry hands back, which must
	// keep the declared layer length. A nil served means honest bytes.
	served func([]byte) []byte
	// verifyPort wires Pull to the digest-verifying reader the port
	// contract promises instead of a plain byte reader.
	verifyPort bool
	// wantSub names the check that must produce the failure.
	wantSub string
	// wantPull reports whether the layer blob is fetched at all.
	wantPull bool
}

// fixtureFor builds the row's honest fixture and applies its single mutation.
// A none-compressed row only exercises the check it names while the layer
// digest is still the content digest, so that invariant is asserted here.
func (tc standardIntegrityCase) fixtureFor(t *testing.T, content []byte) fileFixture {
	t.Helper()
	fx := tc.fixture(t, "disk", content)
	if tc.mutate != nil {
		tc.mutate(&fx.entry)
	}
	if fx.entry.Compression == compressionNone && fx.layer != fx.entry.ContentDigest {
		t.Fatalf("none fixture layer digest %s is not the content digest %s",
			fx.layer, fx.entry.ContentDigest)
	}
	return fx
}

// servedBlob returns the blob bytes the registry hands back for the row. A
// mutated blob must keep the declared layer length and must differ from the
// honest bytes, or the row would never reach the check under test.
func (tc standardIntegrityCase) servedBlob(t *testing.T, stored []byte) []byte {
	t.Helper()
	if tc.served == nil {
		return stored
	}
	served := tc.served(stored)
	if len(served) != len(stored) || bytes.Equal(served, stored) {
		t.Fatalf("served blob must keep length %d and differ: got %d bytes, equal=%t",
			len(stored), len(served), bytes.Equal(served, stored))
	}
	return served
}

// assertRejected requires that the row failed on the check it names: the public
// error is an integrity failure whose message names that check and is not the
// decode ceiling, the port's digest verification ran whenever the row wired it,
// the layer blob was never pulled for a row that expects no pull, and neither
// the destination file nor a success-terminal progress snapshot survives.
func (tc standardIntegrityCase) assertRejected(t *testing.T, got standardIntegrityResult) {
	t.Helper()
	if !errors.Is(got.err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", got.err)
	}
	if !strings.Contains(got.err.Error(), tc.wantSub) {
		t.Fatalf("error %v does not name the %q check", got.err, tc.wantSub)
	}
	if errors.Is(got.err, decomp.ErrSizeExceeded) {
		t.Fatalf("error %v is the decode ceiling, not the check under test", got.err)
	}
	if tc.verifyPort && !got.mismatched.Load() {
		t.Fatal("the digest-verifying blob reader never checked the layer digest")
	}
	if !tc.wantPull {
		got.blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
	}
	assertAbsent(t, got.dest)
	assertNoSuccessTerminal(t, got.mu, got.snaps)
}

// assertNoSuccessTerminal requires that a failed transfer emitted no
// success-terminal snapshot: no commit phase was ever reported and no file
// was ever counted complete.
func assertNoSuccessTerminal(t *testing.T, mu *sync.Mutex, snaps *[]Progress) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for i, s := range *snaps {
		if s.Phase == PhaseCommit {
			t.Fatalf("snap %d reported commit phase after a failed transfer: %+v", i, s)
		}
		if s.CompletedFiles != 0 {
			t.Fatalf("snap %d counted %d completed files after a failed transfer", i, s.CompletedFiles)
		}
	}
}
