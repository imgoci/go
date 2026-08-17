package imgoci

import "github.com/imgoci/go/internal/index"

// EqualMediaType reports whether a and b identify the same parameter-free
// media type under spec section 4. Comparison is ASCII case-insensitive and
// allocates nothing. HTTP Content-Type headers, which may carry parameters,
// must be stripped by the registry adapter before they reach this helper.
func EqualMediaType(a, b string) bool {
	return index.EqualMediaType(a, b)
}
