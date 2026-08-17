package index

import (
	"slices"
	"strings"
	"testing"
)

func TestCanonicalizeUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      []string
		want    string
		wantErr string
	}{
		{name: "nil is empty set", in: nil, want: ""},
		{name: "empty is empty set", in: []string{}, want: ""},
		{name: "sorts tokens", in: []string{"live", "install"}, want: "install,live"},
		{name: "collapses duplicates", in: []string{"live", "install", "live"}, want: "install,live"},
		{name: "single token", in: []string{"live"}, want: "live"},
		{name: "invalid token", in: []string{"Live"}, wantErr: `usage token "Live" is not a basic token`},
		{
			name:    "129-byte token",
			in:      []string{strings.Repeat("a", maxBasicTokenBytes+1)},
			wantErr: `usage token "` + strings.Repeat("a", maxBasicTokenBytes+1) + `" is not a basic token`,
		},
		{
			name: "result of exactly 4096 bytes",
			in:   strings.Split(usageValueOfLength(maxUsageBytes), ","),
			want: usageValueOfLength(maxUsageBytes),
		},
		{
			name:    "result longer than 4096 bytes",
			in:      strings.Split(usageValueOfLength(maxUsageBytes+1), ","),
			wantErr: "usage value exceeds 4096 ASCII bytes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := CanonicalizeUsage(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("CanonicalizeUsage succeeded")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalizeUsage: %v", err)
			}
			if got != tc.want {
				t.Fatalf("CanonicalizeUsage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalizeUsageDoesNotMutateCaller(t *testing.T) {
	t.Parallel()
	in := []string{"live", "install"}
	orig := slices.Clone(in)
	if _, err := CanonicalizeUsage(in); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(in, orig) {
		t.Fatalf("caller slice mutated: %v, want %v", in, orig)
	}
}

func TestValidateUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "live", in: "live"},
		{name: "install and install-offline", in: "install,install-offline"},
		{name: "private token", in: "x-owner-name"},
		{name: "unknown valid token", in: "custom-usage"},
		{name: "4096-byte value", in: usageValueOfLength(maxUsageBytes)},
		{name: "empty string", in: "", wantErr: "usage value must not be empty"},
		{name: "empty middle token", in: "a,,b", wantErr: "empty token"},
		{name: "leading comma", in: ",a", wantErr: "empty token"},
		{name: "trailing comma", in: "a,", wantErr: "empty token"},
		{name: "descending order", in: "live,install", wantErr: "strictly ascending"},
		{name: "duplicate token", in: "install,install", wantErr: "strictly ascending"},
		{name: "whitespace", in: "a, b", wantErr: `usage token " b" is not a basic token`},
		{name: "uppercase", in: "Live", wantErr: `usage token "Live" is not a basic token`},
		{
			name:    "129-byte token",
			in:      strings.Repeat("a", maxBasicTokenBytes+1),
			wantErr: `usage token "` + strings.Repeat("a", maxBasicTokenBytes+1) + `" is not a basic token`,
		},
		{
			name:    "4097-byte value",
			in:      usageValueOfLength(maxUsageBytes + 1),
			wantErr: "usage value exceeds 4096 ASCII bytes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUsage(tc.in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateUsage(%q): %v", tc.in, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateUsage(%q) succeeded", tc.in)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateUsageRelationship(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "bare install-offline", in: "install-offline", wantErr: true},
		{name: "install and install-offline", in: "install,install-offline"},
		{name: "install alone", in: "install"},
		{name: "empty set", in: ""},
		{name: "substring does not trigger", in: "install-offlinex"},
		{name: "substring does not satisfy", in: "install-offline,install-offlinex", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUsageRelationship(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateUsageRelationship(%q) succeeded", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateUsageRelationship(%q): %v", tc.in, err)
			}
		})
	}
}

// usageValueOfLength returns a canonical usage value of exactly n ASCII bytes.
func usageValueOfLength(n int) string {
	const fullTokens = 31
	lastLen := n - fullTokens*maxBasicTokenBytes - fullTokens
	var b strings.Builder
	b.Grow(n)
	for i := range fullTokens {
		if i > 0 {
			b.WriteByte(',')
		}
		writePaddedUsageToken(&b, i, maxBasicTokenBytes)
	}
	b.WriteByte(',')
	writePaddedUsageToken(&b, fullTokens, lastLen)
	return b.String()
}

// writePaddedUsageToken writes a unique basic token of length bytes for index i.
func writePaddedUsageToken(b *strings.Builder, i, length int) {
	const prefix = 3
	writeDecimalPrefix(b, i, prefix)
	for range length - prefix {
		b.WriteByte('a')
	}
}

// writeDecimalPrefix writes i zero-padded to width decimal digits.
func writeDecimalPrefix(b *strings.Builder, i, width int) {
	var buf [8]byte
	for pos := width - 1; pos >= 0; pos-- {
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	b.Write(buf[:width])
}
