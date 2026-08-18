package imgoci

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/adapters"
	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/transfer"
)

var (
	_ FetchOption   = progressOption(nil)
	_ PublishOption = progressOption(nil)
	_ FetchOption   = workersOption(0)
	_ PublishOption = workersOption(0)
)

func TestFromFile(t *testing.T) {
	t.Parallel()
	const path = "/tmp/disk.img"
	got := FromFile(path)
	if got.path != path {
		t.Fatalf("path = %q, want %q", got.path, path)
	}
	if (Source{}).path != "" {
		t.Fatal("zero Source must be empty")
	}
}

func TestPublishReferenceForm(t *testing.T) {
	t.Parallel()
	pin := digest.FromBytes([]byte("x"))
	tests := []struct {
		name string
		ref  Reference
		want string
	}{
		{name: "tag_only", ref: "example.com/os/example:v1"},
		{
			name: "digest_only",
			ref:  Reference("example.com/os/example@" + pin.String()),
			want: "cannot name a published index",
		},
		{
			name: "tag_and_digest",
			ref:  Reference("example.com/os/example:v1@" + pin.String()),
			want: "has no defined write meaning",
		},
		{
			name: "name_only",
			ref:  "example.com/os/example",
			want: "must be tag-only",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPublishReference(t, tt.ref, tt.want)
		})
	}
}

func assertPublishReference(t *testing.T, ref Reference, wantDetail string) {
	t.Helper()
	var constructed int
	c := clientWithTransferPorts(t, &publishManifests{}, &publishBlobs{}, &constructed)
	_, err := c.Publish(t.Context(), ref, validReleaseSpec(t, []byte("payload")))
	if wantDetail == "" {
		if err != nil {
			t.Fatal(err)
		}
		if constructed != 1 {
			t.Fatalf("adapter constructions = %d, want 1", constructed)
		}
		return
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if !strings.Contains(err.Error(), wantDetail) {
		t.Fatalf("err = %v, want detail %q", err, wantDetail)
	}
	if constructed != 0 {
		t.Fatal("adapter must not be constructed for a rejected publish reference")
	}
}

func TestPublishSpecValidation(t *testing.T) {
	t.Parallel()
	path := writePublishFile(t, "ok.bin", []byte("ok"))
	tests := []struct {
		name   string
		mutate func(*ReleaseSpec)
		detail string
	}{
		{name: "empty_name", mutate: func(s *ReleaseSpec) { s.Name = "" }, detail: "name is empty"},
		{name: "empty_version", mutate: func(s *ReleaseSpec) { s.Version = "" }, detail: "version is empty"},
		{
			name:   "utf8_name",
			mutate: func(s *ReleaseSpec) { s.Name = string([]byte{0xff}) },
			detail: "name is not valid UTF-8",
		},
		{
			name:   "utf8_annotation_value",
			mutate: func(s *ReleaseSpec) { s.Annotations = map[string]string{"note": string([]byte{0xff})} },
			detail: "not valid UTF-8",
		},
		{
			name:   "reserved_root_annotation",
			mutate: func(s *ReleaseSpec) { s.Annotations = map[string]string{"io.imgoci.note": "x"} },
			detail: "reserved annotation",
		},
		{
			name: "reserved_entry_annotation",
			mutate: func(s *ReleaseSpec) {
				s.Files[0].Annotations = map[string]string{"io.imgoci.filename": "sneak"}
			},
			detail: "reserved annotation",
		},
		{
			name:   "negative_part_size",
			mutate: func(s *ReleaseSpec) { s.Files[0].Multipart = &MultipartSpec{PartSize: -1} },
			detail: "multipart part size must be >= 0",
		},
		{
			name:   "invalid_filename",
			mutate: func(s *ReleaseSpec) { s.Files[0].Filename = "../x" },
			detail: "filename",
		},
		{
			// Architecture is not one of the four spec §5.4 registries, so
			// this case reaches the spec §5.3 token check rather than the
			// producer registry check.
			name:   "invalid_selector_token",
			mutate: func(s *ReleaseSpec) { s.Files[0].Selector.Architecture = "bad!" },
			detail: "basic tokens",
		},
		{
			name:   "non_registry_selector_value",
			mutate: func(s *ReleaseSpec) { s.Files[0].Selector.Role = "data" },
			detail: `io.imgoci.role "data" is not a public value or x-<owner>-<name>`,
		},
		{
			name: "duplicate_five_tuple",
			mutate: func(s *ReleaseSpec) {
				s.Files = append(s.Files, s.Files[0])
				s.Files[1].Filename = "b"
			},
			detail: "duplicated",
		},
		{
			name: "missing_required_role",
			mutate: func(s *ReleaseSpec) {
				s.Files[0].Selector.Representation = "qcow2"
				s.Files[0].Selector.Role = "kernel"
				s.Files[0].Filename = "kernel.img"
			},
			detail: "must contain the disk role",
		},
		{
			name: "incus_vm_wrong_target",
			mutate: func(s *ReleaseSpec) {
				s.Files[0].Selector.Representation = "incus-vm"
				s.Files[0].Selector.Target = "qemu"
				s.Files[0].Selector.Role = "disk"
				s.Files[0].Filename = "disk.qcow2"
			},
			detail: "must use target incus",
		},
		{
			name: "filename_collision",
			mutate: func(s *ReleaseSpec) {
				other := s.Files[0]
				other.Selector.Role = "disk"
				other.Filename = "a"
				s.Files = append(s.Files, other)
			},
			detail: "different filenames",
		},
		{
			name: "shared_source_conflicting_compression",
			mutate: func(s *ReleaseSpec) {
				other := s.Files[0]
				other.Selector.Role = "disk"
				other.Selector.Compression = "gzip"
				other.Filename = "b"
				s.Files = append(s.Files, other)
			},
			detail: "conflicting compression",
		},
		{
			name:   "empty_source",
			mutate: func(s *ReleaseSpec) { s.Files[0].Source = Source{} },
			detail: "empty source",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := ReleaseSpec{
				Name:    "example",
				Version: "1",
				Files: []FileSpec{{
					Source: FromFile(path),
					Selector: Selector{
						Architecture:   "amd64",
						Target:         "x-test-target",
						Representation: "x-test-format",
						Role:           "x-test-file",
						Compression:    "none",
					},
					Filename: "a",
				}},
			}
			tt.mutate(&spec)
			var constructed int
			c := clientWithPorts(t, &stubManifests{}, &constructed)
			_, err := c.Publish(t.Context(), "example.com/os/example:v1", spec)
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("err = %v, want ErrInvalidSpec", err)
			}
			if errors.Is(err, ErrInvalidIndex) {
				t.Fatalf("producer validation must not surface as ErrInvalidIndex: %v", err)
			}
			if !strings.Contains(err.Error(), tt.detail) {
				t.Fatalf("err = %v, want detail %q", err, tt.detail)
			}
			if constructed != 0 {
				t.Fatal("adapter must not be constructed for an invalid spec")
			}
		})
	}
}

func TestMapPublishError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "digest", err: fmt.Errorf("blob: %w", transfer.ErrDigestMismatch), want: ErrDigestMismatch},
		{name: "decode", err: fmt.Errorf("gzip: %w", decomp.ErrDecode), want: ErrDecode},
		{name: "not_found", err: fmt.Errorf("get: %w", transfer.ErrNotFound), want: ErrNotFound},
		{name: "unauthorized", err: fmt.Errorf("put: %w", transfer.ErrUnauthorized), want: ErrUnauthorized},
		{
			name: "index_rule",
			err:  fmt.Errorf("build index: %w", index.ErrRule),
			want: ErrInvalidSpec,
		},
		{
			name: "index_rule_reword",
			err:  fmt.Errorf("build index: wording changed completely: %w", index.ErrRule),
			want: ErrInvalidSpec,
		},
		{
			name: "shared_blob",
			err:  fmt.Errorf("group: %w", transfer.ErrSharedBlob),
			want: ErrInvalidSpec,
		},
		{
			name: "part_ceiling",
			err: fmt.Errorf(
				"multipart part count %d exceeds %d for stored size %d: %w",
				8192, 4096, int64(8<<30), index.ErrRule,
			),
			want: ErrInvalidSpec,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapPublishError(tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("err = %v, want %v", got, tt.want)
			}
			if errors.Is(got, ErrInvalidIndex) {
				t.Fatalf("must not map to ErrInvalidIndex: %v", got)
			}
		})
	}

	unclassified := []struct {
		name string
		err  error
	}{
		{
			name: "self_oracle",
			err:  errors.New("index self-oracle validate: spec \u00a76 rule 5: duplicated"),
		},
		{
			name: "substring_only",
			err:  errors.New("build index: spec \u00a76 rule 5: transport alternative duplicated"),
		},
	}
	for _, tt := range unclassified {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapPublishError(tt.err)
			if errors.Is(got, ErrInvalidSpec) {
				t.Fatalf("must not map to ErrInvalidSpec: %v", got)
			}
			if errors.Is(got, ErrInvalidIndex) {
				t.Fatalf("must not map to ErrInvalidIndex: %v", got)
			}
		})
	}
}

func TestPublishMapsUnauthorizedAndDecode(t *testing.T) {
	t.Parallel()
	t.Run("unauthorized", func(t *testing.T) {
		t.Parallel()
		blobs := &publishBlobs{existsErr: transfer.ErrUnauthorized}
		c := clientWithTransferPorts(t, &publishManifests{}, blobs, nil)
		_, err := c.Publish(t.Context(), "example.com/os/example:v1", validReleaseSpec(t, []byte("payload")))
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("two_member_gzip", func(t *testing.T) {
		t.Parallel()
		var constructed int
		c := clientWithTransferPorts(t, &publishManifests{}, &publishBlobs{}, &constructed)
		_, err := c.Publish(t.Context(), "example.com/os/example:v1", gzipTwoMemberSpec(t))
		if !errors.Is(err, ErrDecode) {
			t.Fatalf("err = %v, want ErrDecode", err)
		}
		if constructed != 1 {
			t.Fatal("strict gzip failure is after adapter construction")
		}
	})
	t.Run("not_found", func(t *testing.T) {
		t.Parallel()
		c := clientWithTransferPorts(t, &publishManifests{putErr: transfer.ErrNotFound}, &publishBlobs{}, nil)
		_, err := c.Publish(t.Context(), "example.com/os/example:v1", validReleaseSpec(t, []byte("payload")))
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestPublishWorkersRejectedBeforeIO(t *testing.T) {
	t.Parallel()
	var constructed int
	c := clientWithPorts(t, &stubManifests{}, &constructed)
	_, err := c.Publish(
		t.Context(),
		"example.com/os/example:v1",
		validReleaseSpec(t, []byte("payload")),
		WithWorkers(0),
	)
	if err == nil {
		t.Fatal("expected worker count error")
	}
	if constructed != 0 {
		t.Fatal("adapter must not be constructed for a non-positive worker count")
	}
}

func TestPublishHappyPath(t *testing.T) {
	t.Parallel()
	manifests := &publishManifests{}
	blobs := &publishBlobs{}
	c := clientWithTransferPorts(t, manifests, blobs, nil)
	dgst, err := c.Publish(t.Context(), "example.com/os/example:v1", validReleaseSpec(t, []byte("payload")))
	if err != nil {
		t.Fatal(err)
	}
	if dgst == "" {
		t.Fatal("expected index digest")
	}
	puts := manifests.putRefs()
	if len(puts) < 2 || puts[len(puts)-1] != publishTestTag {
		t.Fatalf("puts %v, want manifests then tag", puts)
	}
}

// publishTestTag is the tag every publish test writes the release index to.
const publishTestTag = "v1"

type publishManifests struct {
	mu     sync.Mutex
	refs   []string
	bodies map[string][]byte
	putErr error
}

func (s *publishManifests) Get(context.Context, string, string) ([]byte, string, error) {
	return nil, "", errors.New("get not implemented")
}

func (s *publishManifests) Put(_ context.Context, ref, _ string, raw []byte) error {
	s.mu.Lock()
	s.refs = append(s.refs, ref)
	if s.bodies == nil {
		s.bodies = make(map[string][]byte)
	}
	s.bodies[ref] = bytes.Clone(raw)
	s.mu.Unlock()
	return s.putErr
}

// body returns the manifest body published at the tag these tests publish to.
func (s *publishManifests) body() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[publishTestTag]
}

func (s *publishManifests) putRefs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.refs))
	copy(out, s.refs)
	return out
}

type publishBlobs struct {
	existsErr error
	pushErr   error
	started   chan struct{}
	proceed   chan struct{}
	startOnce sync.Once
}

func (s *publishBlobs) Exists(context.Context, digest.Digest) (bool, error) {
	if s.started != nil {
		s.startOnce.Do(func() { close(s.started) })
		if s.proceed != nil {
			<-s.proceed
		}
	}
	return false, s.existsErr
}

func (s *publishBlobs) Push(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
	_, _ = io.Copy(io.Discard, r)
	return s.pushErr
}

func (s *publishBlobs) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	return nil, errors.New("pull not implemented")
}

func clientWithTransferPorts(
	t *testing.T,
	manifests transfer.Manifests,
	blobs transfer.Blobs,
	constructed *int,
) *Client {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	c.pool = adapters.NewPool(func(context.Context, string, string, adapters.Config) (adapters.Ports, error) {
		if constructed != nil {
			*constructed++
		}
		return adapters.Ports{Manifests: manifests, Blobs: blobs}, nil
	})
	return c
}

func writePublishFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validReleaseSpec(t *testing.T, data []byte) ReleaseSpec {
	t.Helper()
	return ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{{
			Source: FromFile(writePublishFile(t, "file.bin", data)),
			Selector: Selector{
				Architecture:   "amd64",
				Target:         "x-test-target",
				Representation: "x-test-format",
				Role:           "x-test-file",
				Compression:    "none",
			},
			Filename: "a",
		}},
	}
}

func gzipTwoMemberSpec(t *testing.T) ReleaseSpec {
	t.Helper()
	return ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{{
			Source: FromFile(writePublishFile(t, "two.gz", twoMemberGzip(t))),
			Selector: Selector{
				Architecture:   "amd64",
				Target:         "x-test-target",
				Representation: "x-test-format",
				Role:           "x-test-file",
				Compression:    "gzip",
			},
			Filename: "a",
		}},
	}
}

func twoMemberGzip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, payload := range [][]byte{[]byte("one"), []byte("two")} {
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func gzipMember(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(p); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type callCountingPorts struct {
	mu sync.Mutex
	n  int
}

func (c *callCountingPorts) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func (c *callCountingPorts) hit() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *callCountingPorts) Get(context.Context, string, string) ([]byte, string, error) {
	c.hit()
	return nil, "", errors.New("get")
}

func (c *callCountingPorts) Put(context.Context, string, string, []byte) error {
	c.hit()
	return errors.New("put")
}

func (c *callCountingPorts) Exists(context.Context, digest.Digest) (bool, error) {
	c.hit()
	return false, errors.New("exists")
}

func (c *callCountingPorts) Push(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
	c.hit()
	_, _ = io.Copy(io.Discard, r)
	return errors.New("push")
}

func (c *callCountingPorts) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	c.hit()
	return nil, errors.New("pull")
}

func TestPublishRule8MapsToInvalidSpec(t *testing.T) {
	t.Parallel()
	payload := gzipMember(t, []byte("same-bytes"))
	spec := ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{
			{
				Source: FromFile(writePublishFile(t, "a.gz", payload)),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "disk",
					Compression:    "gzip",
				},
				Filename: "a",
			},
			{
				Source: FromFile(writePublishFile(t, "b.gz", payload)),
				Selector: Selector{
					Architecture:   "arm64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "kernel",
					Compression:    "none",
				},
				Filename: "b",
			},
		},
	}
	ports := &callCountingPorts{}
	c := clientWithTransferPorts(t, ports, ports, nil)
	_, err := c.Publish(t.Context(), "example.com/os/example:v1", spec)
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if !errors.Is(err, transfer.ErrSharedBlob) {
		t.Fatalf("err = %v, want ErrSharedBlob", err)
	}
	if n := ports.calls(); n != 0 {
		t.Fatalf("port calls = %d, want 0", n)
	}
}

func TestPublishRule6AfterHashBeforeNetwork(t *testing.T) {
	t.Parallel()
	spec := ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{
			{
				Source: FromFile(writePublishFile(t, "a.bin", []byte("content-a"))),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "x-test-file",
					Compression:    "none",
				},
				Filename: "same",
			},
			{
				Source: FromFile(writePublishFile(t, "b.gz", gzipMember(t, []byte("content-b")))),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "x-test-file",
					Compression:    "gzip",
				},
				Filename: "same",
			},
		},
	}
	ports := &callCountingPorts{}
	c := clientWithTransferPorts(t, ports, ports, nil)
	_, err := c.Publish(t.Context(), "example.com/os/example:v1", spec)
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if !errors.Is(err, index.ErrRule) {
		t.Fatalf("err = %v, want index.ErrRule", err)
	}
	if n := ports.calls(); n != 0 {
		t.Fatalf("port calls = %d, want 0", n)
	}
}

func TestPublishClonesAnnotations(t *testing.T) {
	t.Parallel()
	manifests := &publishManifests{}
	blobs := &publishBlobs{started: make(chan struct{}), proceed: make(chan struct{})}
	c := clientWithTransferPorts(t, manifests, blobs, nil)
	spec := validReleaseSpec(t, []byte("payload"))
	spec.Annotations = map[string]string{"note": "original"}
	spec.Files[0].Annotations = map[string]string{"extra": "keep"}

	errc := make(chan error, 1)
	go func() {
		_, err := c.Publish(t.Context(), "example.com/os/example:v1", spec)
		errc <- err
	}()
	select {
	case <-blobs.started:
	case <-t.Context().Done():
		t.Fatal("publish did not reach the registry")
	}
	spec.Annotations["note"] = "mutated"
	spec.Files[0].Annotations["extra"] = "mutated"
	close(blobs.proceed)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}

	raw := manifests.body()
	if len(raw) == 0 {
		t.Fatal("index PUT body missing")
	}
	v, err := index.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if v.Annotations["note"] != "original" {
		t.Fatalf("root annotation = %q, want original", v.Annotations["note"])
	}
	if v.Manifests[0].Annotations["extra"] != "keep" {
		t.Fatalf("file annotation = %q, want keep", v.Manifests[0].Annotations["extra"])
	}
}

func TestPublishUsageAnnotation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		usage     Usage
		wantKey   bool
		wantValue string
	}{
		{name: "zero usage omits annotation", wantKey: false},
		{
			name:      "non-empty usage is emitted canonically",
			usage:     mustUsage(t, "live", "install"),
			wantKey:   true,
			wantValue: `"io.imgoci.usage":"install,live"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			manifests := &publishManifests{}
			c := clientWithTransferPorts(t, manifests, &publishBlobs{}, nil)
			spec := validReleaseSpec(t, []byte("payload"))
			spec.Files[0].Selector.Usage = tt.usage
			if _, err := c.Publish(t.Context(), "example.com/os/example:v1", spec); err != nil {
				t.Fatal(err)
			}
			raw := manifests.body()
			if len(raw) == 0 {
				t.Fatal("index PUT body missing")
			}
			const usageKey = `"io.imgoci.usage"`
			if tt.wantKey {
				if !bytes.Contains(raw, []byte(tt.wantValue)) {
					t.Fatalf("encoded index missing %s\ngot: %s", tt.wantValue, raw)
				}
				return
			}
			if bytes.Contains(raw, []byte(usageKey)) {
				t.Fatalf("encoded index contains %s\ngot: %s", usageKey, raw)
			}
		})
	}
}

func TestPublishDifferentUsageDifferentContent(t *testing.T) {
	t.Parallel()
	spec := ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{
			{
				Source: FromFile(writePublishFile(t, "a.bin", []byte("content-a"))),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "x-test-file",
					Compression:    "none",
				},
				Filename: "a",
			},
			{
				Source: FromFile(writePublishFile(t, "b.bin", []byte("content-b"))),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Usage:          mustUsage(t, "live"),
					Role:           "x-test-file",
					Compression:    "none",
				},
				Filename: "b",
			},
		},
	}
	manifests := &publishManifests{}
	c := clientWithTransferPorts(t, manifests, &publishBlobs{}, nil)
	if _, err := c.Publish(t.Context(), "example.com/os/example:v1", spec); err != nil {
		t.Fatal(err)
	}
	raw := manifests.body()
	if len(raw) == 0 {
		t.Fatal("index PUT body missing")
	}
	if !bytes.Contains(raw, []byte(`"io.imgoci.usage":"live"`)) {
		t.Fatalf("encoded index missing live usage\ngot: %s", raw)
	}
}

func TestPublishSameSourceDifferentUsage(t *testing.T) {
	t.Parallel()
	source := FromFile(writePublishFile(t, "shared.bin", []byte("shared-bytes")))
	base := Selector{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Role:           "x-test-file",
		Compression:    "none",
	}
	live := base
	live.Usage = mustUsage(t, "live")
	spec := ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{
			{Source: source, Selector: base, Filename: "shared.bin"},
			{Source: source, Selector: live, Filename: "shared.bin"},
		},
	}
	manifests := &publishManifests{}
	c := clientWithTransferPorts(t, manifests, &publishBlobs{}, nil)
	if _, err := c.Publish(t.Context(), "example.com/os/example:v1", spec); err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(manifests.body())
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	entries := idx.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 distinct descriptors", len(entries))
	}
	wantContent := digest.FromBytes([]byte("shared-bytes"))
	got := map[string]FileEntry{}
	for _, entry := range entries {
		got[entry.Selector.Usage.String()] = entry
	}
	empty, okEmpty := got[""]
	liveEntry, okLive := got["live"]
	if !okEmpty || !okLive {
		t.Fatalf("usage sets = %v, want empty and live", keysOf(got))
	}
	if empty.Filename != "shared.bin" || liveEntry.Filename != "shared.bin" {
		t.Fatalf("filenames = %q, %q; want shared.bin for both", empty.Filename, liveEntry.Filename)
	}
	if empty.ContentDigest != wantContent || liveEntry.ContentDigest != wantContent {
		t.Fatalf("content digests = %s, %s; want %s", empty.ContentDigest, liveEntry.ContentDigest, wantContent)
	}
	if empty.Digest == "" || empty.Digest != liveEntry.Digest {
		t.Fatalf("manifest digests = %s, %s; want the same non-empty digest", empty.Digest, liveEntry.Digest)
	}
}

func keysOf(got map[string]FileEntry) []string {
	out := make([]string, 0, len(got))
	for key := range got {
		out = append(out, key)
	}
	return out
}
