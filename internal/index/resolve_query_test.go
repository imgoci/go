package index

import (
	"strings"
	"testing"
)

func TestValidateResolveQueryCompressions(t *testing.T) {
	t.Parallel()
	base := ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
	}
	tests := []struct {
		name         string
		compressions []string
		wantErr      bool
	}{
		{name: "none", compressions: []string{"none"}},
		{name: "gzip", compressions: []string{"gzip"}},
		{name: "xz", compressions: []string{"xz"}},
		{name: "zstd", compressions: []string{"zstd"}},
		{name: "all_legal", compressions: []string{"none", "gzip", "xz", "zstd"}},
		{name: "x-brotli", compressions: []string{"x-brotli"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := base
			q.Compressions = tc.compressions
			_, err := ValidateResolveQuery(q)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateResolveQuery(%q) succeeded", tc.compressions)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateResolveQuery(%q): %v", tc.compressions, err)
			}
		})
	}
}

func TestValidateResolveQueryUsage(t *testing.T) {
	t.Parallel()
	base := ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "iso",
		Compressions:   []string{"none"},
	}
	tests := []struct {
		name    string
		usage   []string
		want    string
		wantErr string
	}{
		{name: "nil is the empty set", usage: nil, want: ""},
		{name: "empty slice is the empty set", usage: []string{}, want: ""},
		{
			name:  "install-offline without install is accepted",
			usage: []string{"install-offline"},
			want:  "install-offline",
		},
		{
			name:    "duplicates are rejected",
			usage:   []string{"install", "install"},
			wantErr: "resolve query usage",
		},
		{
			name:    "a non-basic token is rejected",
			usage:   []string{"INSTALL"},
			wantErr: "resolve query usage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q := base
			q.Usage = tt.usage
			got, err := ValidateResolveQuery(q)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("ValidateResolveQuery succeeded")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateResolveQuery: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ValidateResolveQuery = %q, want %q", got, tt.want)
			}
		})
	}
}
