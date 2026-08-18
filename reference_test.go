package imgoci

import (
	"errors"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/ociref"
)

const (
	testSHA256     = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSHA512     = "sha512:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testHost       = "example.com"
	testHostPort   = "localhost:5000"
	testRepository = "os/example"
)

func TestReferenceParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		ref        Reference
		wantHost   string
		wantRepo   string
		wantTag    string
		wantDigest digest.Digest
		wantErr    bool
	}{
		{
			name:     "tag",
			ref:      "example.com/os/example:v1",
			wantHost: testHost,
			wantRepo: testRepository,
			wantTag:  "v1",
		},
		{
			name:       "digest",
			ref:        Reference("example.com/os/example@" + testSHA256),
			wantHost:   testHost,
			wantRepo:   testRepository,
			wantDigest: digest.Digest(testSHA256),
		},
		{
			name:       "tag_and_digest",
			ref:        Reference("example.com/os/example:v1@" + testSHA256),
			wantHost:   testHost,
			wantRepo:   testRepository,
			wantTag:    "v1",
			wantDigest: digest.Digest(testSHA256),
		},
		{
			name:     "host_port",
			ref:      "localhost:5000/os/example:v1",
			wantHost: testHostPort,
			wantRepo: testRepository,
			wantTag:  "v1",
		},
		{
			name:     "no_tag",
			ref:      "example.com/os/example",
			wantHost: testHost,
			wantRepo: testRepository,
		},
		{
			name:    "empty",
			ref:     "",
			wantErr: true,
		},
		{
			name:    "no_registry",
			ref:     "ubuntu:latest",
			wantErr: true,
		},
		{
			name:    "uppercase_repository",
			ref:     "example.com/OS/example:v1",
			wantErr: true,
		},
		{
			name:    "short_digest",
			ref:     "example.com/os/example@sha256:dead",
			wantErr: true,
		},
		{
			name:    "sha512",
			ref:     Reference("example.com/os/example@" + testSHA512),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.ref.parse()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parse(%q) succeeded: %+v", tt.ref, got)
				}
				if errors.Is(err, ErrInvalidSpec) || errors.Is(err, ErrInvalidIndex) {
					t.Fatalf("malformed reference must not wrap a public sentinel: %v", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("parse(%q): %v", tt.ref, err)
			}
			if got.Host != tt.wantHost || got.Repository != tt.wantRepo ||
				got.Tag != tt.wantTag || got.Digest != tt.wantDigest {
				t.Fatalf("parse(%q) = %+v", tt.ref, got)
			}
		})
	}
}

func TestParsedRefManifestRefPrefersDigest(t *testing.T) {
	t.Parallel()
	parsed := ociref.Parsed{Tag: "v1", Digest: digest.Digest(testSHA256)}
	if got := parsed.ManifestRef(); got != testSHA256 {
		t.Fatalf("manifestRef = %q, want digest", got)
	}
	parsed.Digest = ""
	if got := parsed.ManifestRef(); got != "v1" {
		t.Fatalf("manifestRef = %q, want tag", got)
	}
}
