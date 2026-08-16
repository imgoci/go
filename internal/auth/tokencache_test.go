package auth

import (
	"testing"
	"time"
)

func TestExpiryOf(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresIn int
		issuedAt  string
		now       time.Time
		want      time.Time
	}{
		{
			name:      "default lifetime is sixty seconds with a thirty second skew",
			expiresIn: 0,
			now:       now,
			want:      now.Add(30 * time.Second),
		},
		{
			name:      "a negative expires_in takes the spec default",
			expiresIn: -1,
			now:       now,
			want:      now.Add(30 * time.Second),
		},
		{
			name:      "a long lifetime gives back thirty seconds",
			expiresIn: 120,
			now:       now,
			want:      now.Add(90 * time.Second),
		},
		{
			name:      "a short lifetime gives back half",
			expiresIn: 10,
			now:       now,
			want:      now.Add(5 * time.Second),
		},
		{
			name:      "issued_at measures the lifetime from the token endpoint's clock",
			expiresIn: 60,
			issuedAt:  now.Add(-20 * time.Second).Format(time.RFC3339),
			now:       now,
			want:      now.Add(10 * time.Second),
		},
		{
			name:      "issued_at with fractional seconds is still RFC 3339",
			expiresIn: 60,
			issuedAt:  now.Add(-20 * time.Second).Format(time.RFC3339Nano),
			now:       now,
			want:      now.Add(10 * time.Second),
		},
		{
			name:      "an unreadable issued_at is treated as now",
			expiresIn: 60,
			issuedAt:  "not-a-time",
			now:       now,
			want:      now.Add(30 * time.Second),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := expiryOf(tc.expiresIn, tc.issuedAt, tc.now)
			if !got.Equal(tc.want) {
				t.Fatalf("expiryOf = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTokenCacheGetPutExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	cache := newTokenCache()
	const (
		realm   = "https://auth.example.com/token"
		service = "reg.example.com"
		scope   = "repository:team/artifact:pull"
		token   = "live-token"
	)

	until := expiryOf(60, "", now)
	cache.put(realm, service, scope, token, until)

	if got := cache.get(realm, service, scope, now); got != token {
		t.Fatalf("get before expiry = %q, want %q", got, token)
	}
	if got := cache.get(realm, service, scope, until.Add(-time.Nanosecond)); got != token {
		t.Fatalf("get just before expiry = %q, want %q", got, token)
	}
	if got := cache.get(realm, service, scope, until); got != "" {
		t.Fatalf("get at expiry = %q, want empty", got)
	}
	if got := cache.get(realm, service, scope, now); got != "" {
		t.Fatalf("expired entry was not dropped = %q", got)
	}
}

func TestTokenCacheKeysDoNotCollide(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	until := now.Add(time.Minute)
	cache := newTokenCache()
	cache.put("realm-a", "svc", "scope", "token-a", until)
	cache.put("realm-b", "svc", "scope", "token-b", until)
	cache.put("realm-a", "other", "scope", "token-c", until)
	cache.put("realm-a", "svc", "other", "token-d", until)

	if got := cache.get("realm-a", "svc", "scope", now); got != "token-a" {
		t.Fatalf("get = %q, want token-a", got)
	}
	if got := cache.get("realm-b", "svc", "scope", now); got != "token-b" {
		t.Fatalf("get = %q, want token-b", got)
	}
	if got := cache.get("realm-a", "other", "scope", now); got != "token-c" {
		t.Fatalf("get = %q, want token-c", got)
	}
	if got := cache.get("realm-a", "svc", "other", now); got != "token-d" {
		t.Fatalf("get = %q, want token-d", got)
	}
}

func TestTokenCacheInvalidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	cache := newTokenCache()
	cache.put("realm", "svc", "scope", "token", now.Add(time.Minute))
	cache.invalidate("realm", "svc", "scope")
	if got := cache.get("realm", "svc", "scope", now); got != "" {
		t.Fatalf("get after invalidate = %q, want empty", got)
	}
}

func TestMarginFor(t *testing.T) {
	t.Parallel()

	if got := marginFor(10 * time.Second); got != 5*time.Second {
		t.Fatalf("short lifetime margin = %v, want 5s", got)
	}
	if got := marginFor(60 * time.Second); got != tokenMargin {
		t.Fatalf("default lifetime margin = %v, want %v", got, tokenMargin)
	}
	if got := marginFor(120 * time.Second); got != tokenMargin {
		t.Fatalf("long lifetime margin = %v, want %v", got, tokenMargin)
	}
}
