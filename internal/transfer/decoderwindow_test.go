package transfer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/ulikunitz/xz"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/index"
	mpmocks "github.com/imgoci/go/internal/multipart/mocks"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

const (
	// wideDictCap is the LZMA2 dictionary the test stored file declares:
	// above the 8 MiB a lowered ceiling allows, below the package default.
	// It is what `xz -9` declares.
	wideDictCap = 64 << 20
	// loweredWindow is a ceiling that refuses wideDictCap.
	loweredWindow = 8 << 20
)

// wideDictXZ returns an xz stream over payload whose Block Header declares a
// [wideDictCap] LZMA2 dictionary, so the ceiling a request carries decides
// whether it can be decoded at all.
func wideDictXZ(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xz.WriterConfig{DictCap: wideDictCap}.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestFetchFilesDecoderMaxWindowReachesTheLayerDecode proves the request field
// reaches [copyLayer]: the same stored file is refused under a lowered ceiling
// and retrieved under the default, whether the default is named or left zero.
func TestFetchFilesDecoderMaxWindowReachesTheLayerDecode(t *testing.T) {
	t.Parallel()

	content := []byte("hello imgoci wide dictionary")
	fx := newFixture(t, "disk", content, wideDictXZ(t, content), "xz")

	tests := []struct {
		name      string
		maxWindow uint64
		wantErr   bool
	}{
		{name: "unset resolves to the package default", maxWindow: 0},
		{name: "the package default admits it", maxWindow: decomp.DefaultDecoderMaxWindow},
		{name: "a lowered ceiling refuses it", maxWindow: loweredWindow, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := regmocks.NewMockManifests(t)
			m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
				Return(fx.manifest, index.MediaTypeManifest, nil).Once()
			blobs := regmocks.NewMockBlobs(t)
			blobs.EXPECT().Pull(mock.Anything, fx.layer).
				Return(io.NopCloser(bytes.NewReader(fx.stored)), nil).Once()

			dest := filepath.Join(t.TempDir(), "disk.img")
			err := FetchFiles(t.Context(), FetchFilesRequest{
				Manifests:        m,
				Blobs:            blobs,
				Entries:          []Entry{fx.entry},
				ByRole:           map[string]string{"disk": dest},
				DecoderMaxWindow: tt.maxWindow,
			})

			if tt.wantErr {
				if !errors.Is(err, decomp.ErrDecode) {
					t.Fatalf("error %v is not decomp.ErrDecode", err)
				}
				assertAbsent(t, dest)
				return
			}
			if err != nil {
				t.Fatalf("FetchFiles: %v", err)
			}
			got, readErr := os.ReadFile(dest)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("committed %q, want %q", got, content)
			}
		})
	}
}

// TestFetchFilesDecoderMaxWindowReachesTheStoredDecode is the same proof for
// the BigOCI path, which decodes the cached stored file in [decodeStored]
// rather than the layer body.
func TestFetchFilesDecoderMaxWindowReachesTheStoredDecode(t *testing.T) {
	t.Parallel()

	content := []byte("hello imgoci bigoci wide dictionary")
	fx := newBigOCIFixture(t, "disk", content, wideDictXZ(t, content), "xz")

	tests := []struct {
		name      string
		maxWindow uint64
		wantErr   bool
	}{
		{name: "unset resolves to the package default", maxWindow: 0},
		{name: "a lowered ceiling refuses it", maxWindow: loweredWindow, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := regmocks.NewMockManifests(t)
			m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
				Return(fx.manifest, index.MediaTypeManifest, nil).Once()
			blobs := regmocks.NewMockBlobs(t)
			mp := mpmocks.NewMockMultipart(t)
			mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
				RunAndReturn(writePullTo(fx.stored)).Once()

			dest := filepath.Join(t.TempDir(), "disk.img")
			err := FetchFiles(t.Context(), FetchFilesRequest{
				Manifests:        m,
				Blobs:            blobs,
				Multipart:        mp,
				Repository:       testRepo,
				Entries:          []Entry{fx.entry},
				ByRole:           map[string]string{"disk": dest},
				DecoderMaxWindow: tt.maxWindow,
			})

			if tt.wantErr {
				if !errors.Is(err, decomp.ErrDecode) {
					t.Fatalf("error %v is not decomp.ErrDecode", err)
				}
				assertAbsent(t, dest)
				return
			}
			if err != nil {
				t.Fatalf("FetchFiles: %v", err)
			}
			got, readErr := os.ReadFile(dest)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("committed %q, want %q", got, content)
			}
		})
	}
}

// TestPublishDecoderMaxWindowReachesPass1 proves the request field reaches
// pass-1 strict decode: a producer running a lowered ceiling cannot publish a
// stored file a fetch under that same ceiling would refuse, and nothing is
// written to the registry when it tries.
func TestPublishDecoderMaxWindowReachesPass1(t *testing.T) {
	t.Parallel()

	content := []byte("hello imgoci publish wide dictionary")
	source := writeTemp(t, "wide.xz", wideDictXZ(t, content))

	tests := []struct {
		name      string
		maxWindow uint64
		wantErr   bool
	}{
		{name: "unset resolves to the package default", maxWindow: 0},
		{name: "the package default admits it", maxWindow: decomp.DefaultDecoderMaxWindow},
		{name: "a lowered ceiling refuses it", maxWindow: loweredWindow, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := &callLog{}
			ports := recordingPorts(t, log, nil)

			_, err := Publish(t.Context(), ports, PublishRequest{
				Tag:              publishTag,
				Name:             "example",
				Version:          "1",
				Entries:          []PublishEntry{publishEntry(source, "disk", "xz", "disk.img")},
				DecoderMaxWindow: tt.maxWindow,
			})

			if tt.wantErr {
				if !errors.Is(err, decomp.ErrDecode) {
					t.Fatalf("error %v is not decomp.ErrDecode", err)
				}
				if ops := log.snapshot(); len(ops) != 0 {
					t.Fatalf("pass-1 rejection still touched the registry: %v", ops)
				}
				return
			}
			if err != nil {
				t.Fatalf("Publish: %v", err)
			}
			if len(log.putRefs()) == 0 {
				t.Fatal("publish wrote no manifest")
			}
		})
	}
}
