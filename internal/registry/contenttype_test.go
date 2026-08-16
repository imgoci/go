package registry

import (
	"testing"
)

func TestParseContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  string
		want    string
		wantErr bool
	}{
		{
			name:   "bare type",
			header: "application/vnd.oci.image.index.v1+json",
			want:   "application/vnd.oci.image.index.v1+json",
		},
		{
			name:   "charset parameter is stripped",
			header: "application/json; charset=utf-8",
			want:   "application/json",
		},
		{
			name:   "quoted charset is stripped",
			header: `text/html; charset="utf-8"`,
			want:   "text/html",
		},
		{
			name:   "boundary junk is stripped",
			header: `multipart/mixed; boundary=----junk; charset=utf-8`,
			want:   "multipart/mixed",
		},
		{
			name:   "type is folded to lowercase",
			header: "Application/JSON; charset=UTF-8",
			want:   "application/json",
		},
		{
			name:    "empty is malformed",
			header:  "",
			wantErr: true,
		},
		{
			name:    "missing subtype is malformed",
			header:  "application",
			wantErr: true,
		},
		{
			name:    "parameter without value is malformed",
			header:  "application/json; charset",
			wantErr: true,
		},
		{
			name:    "lone slash is malformed",
			header:  "/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseContentType(tt.header)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
