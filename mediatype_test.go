package imgoci

import "testing"

func TestEqualMediaType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "identical", a: "application/vnd.imgoci.file.v1", b: "application/vnd.imgoci.file.v1", want: true},
		{name: "ascii_case", a: "Application/VND.imgoci.file.v1", b: "application/vnd.imgoci.file.v1", want: true},
		{
			name: "different_subtype",
			a:    "application/vnd.imgoci.file.v1",
			b:    "application/vnd.bigoci.file.v1",
			want: false,
		},
		{name: "empty", a: "", b: "", want: true},
		{name: "empty_vs_value", a: "", b: "application/vnd.imgoci.file.v1", want: false},
		{name: "long_s_not_s", a: "manife\u017Ft", b: "manifest", want: false},
		{name: "s_not_long_s", a: "manifest", b: "manife\u017Ft", want: false},
		{name: "kelvin_not_k", a: "\u212A", b: "k", want: false},
		{name: "k_not_kelvin", a: "k", b: "\u212A", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := EqualMediaType(tc.a, tc.b); got != tc.want {
				t.Fatalf("EqualMediaType(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
