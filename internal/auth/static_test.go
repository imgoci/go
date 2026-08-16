package auth

import (
	"net/http"
	"testing"
)

func TestCredentialEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cred Credential
		want bool
	}{
		{name: "zero is anonymous", want: true},
		{name: "username only is not empty", cred: Credential{Username: "alice"}},
		{name: "password only is not empty", cred: Credential{Password: "secret"}},
		{name: "both is not empty", cred: Credential{Username: "alice", Password: "secret"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cred.Empty(); got != tc.want {
				t.Fatalf("Empty() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStaticAnswersEveryRegistry(t *testing.T) {
	t.Parallel()

	want := Credential{Username: "alice", Password: "secret"}
	src := NewStatic(want)

	for _, registry := range []string{"ghcr.io", "registry.example.com", ""} {
		got, err := src.Credential(t.Context(), registry)
		if err != nil {
			t.Fatalf("Credential(%q): %v", registry, err)
		}
		if got != want {
			t.Fatalf("Credential(%q) = %+v, want %+v", registry, got, want)
		}
	}
}

func TestSameRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		host     string
		want     bool
	}{
		{name: "exact match", registry: "ghcr.io", host: "ghcr.io", want: true},
		{name: "case is ignored", registry: "GHCR.IO", host: "ghcr.io", want: true},
		{name: "a different host is off-origin", registry: "ghcr.io", host: "storage.example.com"},
		{name: "port is part of the host", registry: "127.0.0.1:5000", host: "127.0.0.1:5001"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sameRegistry(tc.registry, tc.host); got != tc.want {
				t.Fatalf("sameRegistry(%q, %q) = %v, want %v", tc.registry, tc.host, got, tc.want)
			}
		})
	}
}

func TestRequestHostPrefersURLHost(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://ghcr.io/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "other.example.com"
	if got := requestHost(req); got != "ghcr.io" {
		t.Fatalf("requestHost = %q, want ghcr.io", got)
	}
}
