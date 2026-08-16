package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAnonymousBearerEndToEnd(t *testing.T) {
	t.Parallel()

	const token = "anonymous-token"
	var (
		mu           sync.Mutex
		registryAuth []string
		realmAuth    []string
		realmHits    int
	)

	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		realmHits++
		realmAuth = append(realmAuth, r.Header.Get(headerAuthorization))
		mu.Unlock()
		if r.URL.Query().Get("service") != "fixture-registry" {
			t.Errorf("service = %q, want fixture-registry", r.URL.Query().Get("service"))
		}
		if got := r.URL.Query()["scope"]; len(got) != 1 || got[0] != "repository:team/artifact:pull" {
			t.Errorf("scope = %q, want repository:team/artifact:pull", r.URL.Query()["scope"])
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: token, ExpiresIn: 120})
	}))
	t.Cleanup(realm.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		registryAuth = append(registryAuth, r.Header.Get(headerAuthorization))
		mu.Unlock()
		if r.Header.Get(headerAuthorization) == bearerHeader(token) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
			return
		}
		w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+
			`/token",service="fixture-registry",scope="repository:team/artifact:pull"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	base := &recordingTripper{inner: http.DefaultTransport}
	realmRT := &recordingTripper{inner: http.DefaultTransport}
	tr := &Transport{
		Base:        base,
		Host:        mustHost(t, registry.URL),
		RealmClient: &http.Client{Transport: realmRT},
	}

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		registry.URL+"/v2/team/artifact/manifests/v1",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(registryAuth) != 2 {
		t.Fatalf("registry hits = %d, want 2 (401 then retry)", len(registryAuth))
	}
	if registryAuth[0] != "" {
		t.Fatalf("first registry Authorization = %q, want empty", registryAuth[0])
	}
	if registryAuth[1] != bearerHeader(token) {
		t.Fatalf("retried Authorization = %q, want Bearer %s", registryAuth[1], token)
	}
	if realmHits != 1 {
		t.Fatalf("realm hits = %d, want 1", realmHits)
	}
	if len(realmAuth) != 1 || realmAuth[0] != "" {
		t.Fatalf("realm Authorization = %q, want anonymous", realmAuth)
	}

	if !tripperSawOnly(base, registry.URL) {
		t.Fatalf("Base saw off-registry URLs: %q", base.urls())
	}
	if !tripperSawOnly(realmRT, realm.URL) {
		t.Fatalf("RealmClient saw off-realm URLs: %q", realmRT.urls())
	}
}

func TestBearerAppliesStaticCredsToTheRealmRequest(t *testing.T) {
	t.Parallel()

	const (
		user = "alice"
		pass = "secret"
		tok  = "authed-token"
	)
	var realmUser, realmPass string
	var realmOK bool

	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		realmUser, realmPass, realmOK = r.BasicAuth()
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: tok, ExpiresIn: 60})
	}))
	t.Cleanup(realm.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerAuthorization) == bearerHeader(tok) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+`/token",service="reg"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{
		Base:        http.DefaultTransport,
		Host:        mustHost(t, registry.URL),
		Credentials: NewStatic(Credential{Username: user, Password: pass}),
		RealmClient: realm.Client(),
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !realmOK || realmUser != user || realmPass != pass {
		t.Fatalf("realm basic auth = %q:%q ok=%v, want %s:%s", realmUser, realmPass, realmOK, user, pass)
	}
}

func TestStaticBasicAuthFlow(t *testing.T) {
	t.Parallel()

	const (
		user = "alice"
		pass = "secret"
	)
	var seen []string

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get(headerAuthorization))
		gotUser, gotPass, ok := r.BasicAuth()
		if ok && gotUser == user && gotPass == pass {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerChallenge, `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{
		Base:        http.DefaultTransport,
		Host:        mustHost(t, registry.URL),
		Credentials: NewStatic(Credential{Username: user, Password: pass}),
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(seen) != 2 {
		t.Fatalf("hits = %d, want 2", len(seen))
	}
	if seen[0] != "" {
		t.Fatalf("first Authorization = %q, want empty", seen[0])
	}
	want, err := basicHeader(Credential{Username: user, Password: pass})
	if err != nil {
		t.Fatal(err)
	}
	if seen[1] != want {
		t.Fatalf("retried Authorization = %q, want %q", seen[1], want)
	}
}

func TestCredentialStrippingOnHostChange(t *testing.T) {
	t.Parallel()

	var gotAuth string
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(headerAuthorization)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(storage.Close)

	tr := &Transport{
		Base: http.DefaultTransport,
		Host: "registry.example.com",
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, storage.URL+"/blob", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(headerAuthorization, "Bearer must-not-leave")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if gotAuth != "" {
		t.Fatalf("storage Authorization = %q, want empty", gotAuth)
	}
	if req.Header.Get(headerAuthorization) != "Bearer must-not-leave" {
		t.Fatal("RoundTrip mutated the caller's request")
	}
}

func TestOffOriginUnauthorizedIsNotChallenged(t *testing.T) {
	t.Parallel()

	realmHits := 0
	realm := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		realmHits++
	}))
	t.Cleanup(realm.Close)

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+`/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(storage.Close)

	tr := &Transport{
		Base:        http.DefaultTransport,
		Host:        "registry.example.com",
		RealmClient: realm.Client(),
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, storage.URL+"/blob", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if realmHits != 0 {
		t.Fatalf("realm hits = %d, want 0", realmHits)
	}
}

func TestCachedTokenIsReusedWithoutAnotherRealmFetch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	const token = "cached-token"
	realmHits := 0

	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		realmHits++
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: token, ExpiresIn: 120})
	}))
	t.Cleanup(realm.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerAuthorization) == bearerHeader(token) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+`/token",service="reg",scope="repository:a:pull"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{
		Base:        http.DefaultTransport,
		Host:        mustHost(t, registry.URL),
		RealmClient: realm.Client(),
		now:         func() time.Time { return now },
	}

	for range 2 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	if realmHits != 1 {
		t.Fatalf("realm hits = %d, want 1 (second request uses the cache)", realmHits)
	}
}

func TestAccessTokenFieldIsAccepted(t *testing.T) {
	t.Parallel()

	const token = "oauth-token"
	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: token, ExpiresIn: 60})
	}))
	t.Cleanup(realm.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerAuthorization) == bearerHeader(token) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+`/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{
		Base:        http.DefaultTransport,
		Host:        mustHost(t, registry.URL),
		RealmClient: realm.Client(),
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRealmClientIsolationIsStructural(t *testing.T) {
	t.Parallel()

	// A RealmClient that cannot reach anything. If the token GET were issued
	// through Base, the exchange would still succeed against the httptest
	// token endpoint.
	failing := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errRealmRedirect
	})

	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: "should-not-be-fetched", ExpiresIn: 60})
	}))
	t.Cleanup(realm.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+`/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{
		Base:        http.DefaultTransport,
		Host:        mustHost(t, registry.URL),
		RealmClient: &http.Client{Transport: failing},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	if err == nil {
		t.Fatal("RoundTrip succeeded through Base; realm traffic must use RealmClient")
	}
	if !strings.Contains(err.Error(), errRealmRedirect.Error()) {
		t.Fatalf("error = %v, want the injected RealmClient failure", err)
	}
}

func TestUnreadableChallengeIsAnError(t *testing.T) {
	t.Parallel()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerChallenge, "((((")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{Base: http.DefaultTransport, Host: mustHost(t, registry.URL)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	if err == nil {
		t.Fatal("RoundTrip succeeded, want unreadable-challenge error")
	}
	if err.Error() != errUnreadableChallenge.Error() {
		t.Fatalf("error = %q, want %q", err, errUnreadableChallenge)
	}
}

func TestBasicChallengeWithoutCredentialsFails(t *testing.T) {
	t.Parallel()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerChallenge, `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	tr := &Transport{Base: http.DefaultTransport, Host: mustHost(t, registry.URL)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	if err == nil {
		t.Fatal("RoundTrip succeeded, want missing-credential error")
	}
	if err.Error() != errBasicNoCredential.Error() {
		t.Fatalf("error = %q, want %q", err, errBasicNoCredential)
	}
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

func TestTokenRealmUnauthorizedWrapsErrAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
	}{
		{name: "401", status: http.StatusUnauthorized},
		{name: "403", status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(realm.Close)

			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(headerChallenge, `Bearer realm="`+realm.URL+`/token",service="reg"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(registry.Close)

			tr := &Transport{Base: http.DefaultTransport, Host: mustHost(t, registry.URL)}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := tr.RoundTrip(req)
			if resp != nil {
				t.Cleanup(func() { _ = resp.Body.Close() })
			}
			if err == nil {
				t.Fatal("RoundTrip succeeded, want token-status error")
			}
			if !errors.Is(err, ErrAuth) {
				t.Fatalf("err = %v, want ErrAuth", err)
			}
		})
	}
}

// recordingTripper is a [http.RoundTripper] that records the URLs it was asked
// to fetch, so a test can see which client a request left through.
type recordingTripper struct {
	inner http.RoundTripper
	mu    sync.Mutex
	seen  []string
}

// RoundTrip records req's URL and forwards it.
func (r *recordingTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.seen = append(r.seen, req.URL.String())
	r.mu.Unlock()
	return r.inner.RoundTrip(req)
}

// urls returns the recorded URLs in order.
func (r *recordingTripper) urls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.seen))
	copy(out, r.seen)
	return out
}

// roundTripFunc is a [http.RoundTripper] backed by a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls f.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// tripperSawOnly reports whether every recorded URL starts with prefix.
func tripperSawOnly(rt *recordingTripper, prefix string) bool {
	for _, u := range rt.urls() {
		if !strings.HasPrefix(u, prefix) {
			return false
		}
	}
	return true
}

// mustHost returns the host:port of rawURL.
func mustHost(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}
