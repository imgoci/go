package imgoci

// EqualMediaType reports whether a and b identify the same parameter-free
// media type under spec section 4. Comparison is ASCII case-insensitive and
// allocates nothing. HTTP Content-Type headers, which may carry parameters,
// must be stripped by the registry adapter before they reach this helper.
func EqualMediaType(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if asciiFold(a[i]) != asciiFold(b[i]) {
			return false
		}
	}
	return true
}

// asciiFold returns c with ASCII 'A'..'Z' folded to 'a'..'z'. Other bytes,
// including UTF-8 for U+017F and U+212A, are unchanged.
func asciiFold(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
