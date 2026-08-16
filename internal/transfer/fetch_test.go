package transfer

import (
	"errors"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/mock"

	"github.com/imgoci/go/internal/index"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

func TestFetchIndexHappyAndPin(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schemaVersion":2}`)
	got := digest.FromBytes(raw)
	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, "repo:tag", index.MediaTypeIndex).
		Return(raw, index.MediaTypeIndex, nil).Once()

	res, err := FetchIndex(t.Context(), m, "repo:tag", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Digest != got || string(res.Bytes) != string(raw) {
		t.Fatalf("got %+v", res)
	}

	m2 := regmocks.NewMockManifests(t)
	m2.EXPECT().Get(mock.Anything, "repo@sha256:abc", index.MediaTypeIndex).
		Return(raw, index.MediaTypeIndex, nil).Once()
	if _, err := FetchIndex(t.Context(), m2, "repo@sha256:abc", got); err != nil {
		t.Fatal(err)
	}
}

func TestFetchIndexTables(t *testing.T) { //nolint:gocognit // table-driven cases share Get setup
	t.Parallel()
	raw := []byte("index-bytes")
	tests := []struct {
		name        string
		contentType string
		body        []byte
		pinned      digest.Digest
		getErr      error
		wantIs      error
		wantSub     string
	}{
		{
			name:        "content-type mismatch",
			contentType: "application/json",
			body:        raw,
			wantIs:      ErrInvalidDocument,
			wantSub:     "content type",
		},
		{
			name:        "digest pin mismatch",
			contentType: index.MediaTypeIndex,
			body:        raw,
			pinned:      digest.FromBytes([]byte("other")),
			wantIs:      ErrDigestMismatch,
		},
		{
			name:   "not found",
			getErr: ErrNotFound,
			wantIs: ErrNotFound,
		},
		{
			name:   "unauthorized",
			getErr: ErrUnauthorized,
			wantIs: ErrUnauthorized,
		},
		{
			name:        "case-insensitive content type",
			contentType: "APPLICATION/VND.OCI.IMAGE.INDEX.V1+JSON",
			body:        raw,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := regmocks.NewMockManifests(t)
			if tc.getErr != nil {
				m.EXPECT().Get(mock.Anything, "ref", index.MediaTypeIndex).
					Return(nil, "", tc.getErr).Once()
			} else {
				m.EXPECT().Get(mock.Anything, "ref", index.MediaTypeIndex).
					Return(tc.body, tc.contentType, nil).Once()
			}
			res, err := FetchIndex(t.Context(), m, "ref", tc.pinned)
			if tc.wantIs == nil && tc.wantSub == "" {
				if err != nil {
					t.Fatal(err)
				}
				if res == nil {
					t.Fatal("expected result")
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("error %v is not %v", err, tc.wantIs)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %v does not contain %q", err, tc.wantSub)
			}
		})
	}
}
