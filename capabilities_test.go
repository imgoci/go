package imgoci

import "testing"

func TestNewCapabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		types   []string
		wantErr bool
	}{
		{name: "standard_only", types: []string{standardFileMediaType}},
		{name: "standard_plus_bigoci", types: []string{standardFileMediaType, "application/vnd.bigoci.file.v1"}},
		{name: "standard_case_fold", types: []string{"Application/VND.imgoci.file.v1"}},
		{
			name:    "duplicate_after_fold",
			types:   []string{standardFileMediaType, "APPLICATION/vnd.imgoci.file.v1"},
			wantErr: true,
		},
		{name: "missing_standard", types: []string{"application/vnd.bigoci.file.v1"}, wantErr: true},
		{name: "parameterized", types: []string{standardFileMediaType + "; charset=utf-8"}, wantErr: true},
		{name: "bad_syntax", types: []string{standardFileMediaType, "not-a-type"}, wantErr: true},
		{name: "empty", types: nil, wantErr: true},
		{name: "leading_slash", types: []string{"/vnd.imgoci.file.v1"}, wantErr: true},
		{name: "two_slashes", types: []string{"application/vnd/imgoci"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewCapabilities(tc.types...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NewCapabilities(%q) succeeded: %#v", tc.types, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewCapabilities(%q): %v", tc.types, err)
			}
			if !got.supports(standardFileMediaType) {
				t.Fatal("expected standard type to be supported")
			}
		})
	}
}

func TestZeroCapabilitiesMeansStandard(t *testing.T) {
	t.Parallel()
	var zero Capabilities
	if !zero.supports(standardFileMediaType) {
		t.Fatal("zero Capabilities must support the standard file-manifest type")
	}
	if zero.supports("application/vnd.bigoci.file.v1") {
		t.Fatal("zero Capabilities must not assume BigOCI")
	}
	std := StandardCapabilities()
	if !std.supports(standardFileMediaType) {
		t.Fatal("StandardCapabilities must support the standard type")
	}
}
