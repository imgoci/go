package multipart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/imgoci/bigoci"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/imgoci/go/internal/transfer"
)

func TestClientImplementsMultipart(t *testing.T) {
	t.Parallel()
	var (
		_ transfer.Multipart = (*Client)(nil)
		_ bigociAPI          = (*bigoci.Client)(nil)
	)
}

func TestNewEmptyConfig(t *testing.T) {
	t.Parallel()
	client, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if client.inner == nil {
		t.Fatal("inner client is nil")
	}
}

func TestClientOptionsMapping(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{}
	tests := []struct {
		name string
		cfg  Config
		want int
	}{
		{name: "defaults", cfg: Config{}, want: 0},
		{name: "plain http", cfg: Config{PlainHTTP: true}, want: 1},
		{name: "creds", cfg: Config{Username: "u", Secret: "s"}, want: 1},
		{name: "username only", cfg: Config{Username: "u"}, want: 1},
		{name: "secret only", cfg: Config{Secret: "token"}, want: 1},
		{name: "http client", cfg: Config{HTTPClient: httpClient}, want: 1},
		{name: "nil http client omitted", cfg: Config{HTTPClient: nil, PlainHTTP: true}, want: 1},
		{
			name: "all injected options",
			cfg:  Config{PlainHTTP: true, Username: "u", Secret: "s", HTTPClient: httpClient},
			want: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := clientOptions(tc.cfg)
			if len(opts) != tc.want {
				t.Fatalf("len(opts)=%d, want %d", len(opts), tc.want)
			}
			if _, err := bigoci.New(opts...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClientOptionsNeverUnverified(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	const ident = "WithUnverifiedExternalTransport"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(ident)) {
			t.Errorf("%s references %s", name, ident)
		}
	}
	opts := clientOptions(Config{
		PlainHTTP:  true,
		Username:   "u",
		Secret:     "s",
		HTTPClient: &http.Client{},
	})
	if len(opts) != 3 {
		t.Fatalf("len(opts)=%d, want 3", len(opts))
	}
}

func TestClassifySentinels(t *testing.T) {
	t.Parallel()
	other := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil", err: nil, want: nil},
		{name: "not found", err: bigoci.ErrNotFound, want: transfer.ErrNotFound},
		{name: "unauthorized", err: bigoci.ErrUnauthorized, want: transfer.ErrUnauthorized},
		{name: "digest mismatch", err: bigoci.ErrDigestMismatch, want: transfer.ErrDigestMismatch},
		{name: "wrapped not found", err: fmt.Errorf("pull: %w", bigoci.ErrNotFound), want: transfer.ErrNotFound},
		{name: "part too large", err: bigoci.ErrPartTooLarge, want: bigoci.ErrPartTooLarge},
		{name: "not artifact", err: bigoci.ErrNotBigociArtifact, want: bigoci.ErrNotBigociArtifact},
		{name: "other", err: other, want: other},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classify(tc.err)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPushUsesRepoOnlyReference(t *testing.T) {
	t.Parallel()
	fake := &fakeAPI{desc: ocispec.Descriptor{Digest: digest.Digest("sha256:" + strings.Repeat("ab", 32))}}
	client := &Client{inner: fake}
	const repo = "registry.example/os/img"
	got, err := client.Push(t.Context(), repo, "/tmp/file.bin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(fake.repo) != repo {
		t.Fatalf("PushByDigest repo %q, want %q", fake.repo, repo)
	}
	if strings.Contains(string(fake.repo), ":") {
		t.Fatalf("repo-only reference unexpectedly contains a colon: %q", fake.repo)
	}
	if got.Digest != fake.desc.Digest {
		t.Fatalf("descriptor digest %s, want %s", got.Digest, fake.desc.Digest)
	}
	if fake.pushOpts != 0 {
		t.Fatalf("partSize 0 passed %d push options, want 0", fake.pushOpts)
	}
}

func TestPushPartSizeOption(t *testing.T) {
	t.Parallel()
	fake := &fakeAPI{}
	client := &Client{inner: fake}
	const partSize int64 = 1 << 20
	if _, err := client.Push(t.Context(), "registry.example/os/img", "/tmp/file.bin", partSize); err != nil {
		t.Fatal(err)
	}
	if fake.pushOpts != 1 {
		t.Fatalf("partSize %d passed %d push options, want 1", partSize, fake.pushOpts)
	}
}

func TestPushClassifiesSentinel(t *testing.T) {
	t.Parallel()
	client := &Client{inner: &fakeAPI{pushErr: bigoci.ErrUnauthorized}}
	_, err := client.Push(t.Context(), "registry.example/os/img", "/tmp/file.bin", 0)
	if !errors.Is(err, transfer.ErrUnauthorized) {
		t.Fatalf("got %v, want %v", err, transfer.ErrUnauthorized)
	}
}

func TestPullToDigestReference(t *testing.T) {
	t.Parallel()
	fake := &fakeAPI{}
	client := &Client{inner: fake}
	dgst := digest.FromBytes([]byte("artifact"))
	if err := client.PullTo(t.Context(), "registry.example/os/img", dgst, "/tmp/out.bin"); err != nil {
		t.Fatal(err)
	}
	want := "registry.example/os/img@" + dgst.String()
	if string(fake.ref) != want {
		t.Fatalf("Pull ref %q, want %q", fake.ref, want)
	}
}

func TestPullToClassifiesSentinel(t *testing.T) {
	t.Parallel()
	client := &Client{inner: &fakeAPI{pullErr: bigoci.ErrNotFound}}
	err := client.PullTo(t.Context(), "registry.example/os/img", digest.FromBytes([]byte("x")), "/tmp/out.bin")
	if !errors.Is(err, transfer.ErrNotFound) {
		t.Fatalf("got %v, want %v", err, transfer.ErrNotFound)
	}
}

// fakeAPI records the public bigoci surface the adapter calls.
type fakeAPI struct {
	// repo is the repository-only reference PushByDigest received.
	repo bigoci.Reference
	// ref is the pull reference Pull received.
	ref bigoci.Reference
	// pushOpts is how many push options were passed.
	pushOpts int
	// pullOpts is how many pull options were passed.
	pullOpts int
	// pushErr is returned by PushByDigest.
	pushErr error
	// pullErr is returned by Pull.
	pullErr error
	// desc is returned by PushByDigest.
	desc ocispec.Descriptor
}

func (f *fakeAPI) PushByDigest(
	_ context.Context,
	repo bigoci.Reference,
	_ bigoci.FileSource,
	opts ...bigoci.PushOption,
) (ocispec.Descriptor, error) {
	f.repo = repo
	f.pushOpts = len(opts)
	return f.desc, f.pushErr
}

func (f *fakeAPI) Pull(
	_ context.Context,
	ref bigoci.Reference,
	_ bigoci.FileDest,
	opts ...bigoci.PullOption,
) error {
	f.ref = ref
	f.pullOpts = len(opts)
	return f.pullErr
}
