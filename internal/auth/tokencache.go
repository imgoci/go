package auth

import (
	"sync"
	"time"
)

const (
	// tokenMargin is the most of a token's stated lifetime this package gives
	// back. A token is stopped being presented this long before the registry
	// says it ends, so a request that leaves in time cannot arrive after.
	tokenMargin = 30 * time.Second
	// shortTokenShare is the fraction of a short token's lifetime given back
	// instead of the full margin. Half is what keeps a lifetime under a
	// minute usable at all: giving back thirty seconds of a ten-second token
	// would leave nothing to present.
	shortTokenShare = 2
	// defaultTokenLifetime is how long a token is treated as good for when
	// the token endpoint did not say. It is the distribution spec's own
	// default, and it is the value that governs at registries which send no
	// expires_in at all.
	defaultTokenLifetime = 60 * time.Second
)

// tokenCache holds bearer tokens keyed by realm, service, and scope, with
// expiry measured against an injected clock.
type tokenCache struct {
	// mu guards entries.
	mu sync.Mutex
	// entries are live tokens, keyed by realm + "\x00" + service + "\x00" + scope.
	entries map[string]cachedToken
}

// cachedToken is one cached bearer token and when it stops being presented.
type cachedToken struct {
	// token is the bearer token value, without the "Bearer " prefix.
	token string
	// until is the instant this package stops presenting the token, already
	// reduced by the safety skew.
	until time.Time
}

// newTokenCache returns an empty cache.
func newTokenCache() *tokenCache {
	return &tokenCache{entries: make(map[string]cachedToken)}
}

// get returns a still-usable token for realm, service, and scope, or empty.
func (c *tokenCache) get(realm, service, scope string, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(realm, service, scope)
	held, ok := c.entries[key]
	if !ok {
		return ""
	}
	if !now.Before(held.until) {
		delete(c.entries, key)
		return ""
	}

	return held.token
}

// put records token for realm, service, and scope until the safety-skewed expiry.
func (c *tokenCache) put(realm, service, scope, token string, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[cacheKey(realm, service, scope)] = cachedToken{token: token, until: until}
}

// invalidate drops any token cached for realm, service, and scope.
func (c *tokenCache) invalidate(realm, service, scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, cacheKey(realm, service, scope))
}

// cacheKey joins realm, service, and scope so the three never collide inside the map.
func cacheKey(realm, service, scope string) string {
	return realm + "\x00" + service + "\x00" + scope
}

// expiryOf turns expires_in and issued_at into the instant this package stops
// presenting the token.
//
// issued_at is the token endpoint's clock. When it is present and parseable
// as RFC 3339, the lifetime is measured from that instant rather than from
// now, so a token that has already aged on the wire is given up earlier. An
// absent or unreadable issued_at is treated as now. Absent, zero, and
// negative expires_in take the spec's 60-second default.
//
// The safety skew is half of a short lifetime and thirty seconds of a long
// one, matching bigoci: a request is authorized when its headers are read,
// not when its body finishes.
func expiryOf(expiresIn int, issuedAt string, now time.Time) time.Time {
	lifetime := lifetimeOf(expiresIn)
	start := now
	if issuedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, issuedAt); err == nil {
			start = parsed
		}
	}

	return start.Add(lifetime - marginFor(lifetime))
}

// lifetimeOf turns the token endpoint's expires_in into the lifetime the
// expiry rule measures against.
func lifetimeOf(expiresIn int) time.Duration {
	if expiresIn <= 0 {
		return defaultTokenLifetime
	}

	return time.Duration(expiresIn) * time.Second
}

// marginFor returns how long before a token's stated end this package stops
// presenting it.
func marginFor(lifetime time.Duration) time.Duration {
	if half := lifetime / shortTokenShare; half < tokenMargin {
		return half
	}

	return tokenMargin
}
