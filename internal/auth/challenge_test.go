package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ghcrChallenge is the challenge GHCR answers an unauthenticated repository
// request with, copied from the wire. The comma inside its quoted scope is what
// rules out splitting the header on commas.
const ghcrChallenge = `Bearer realm="https://ghcr.io/token",service="ghcr.io",` +
	`scope="repository:team/artifact:pull,push"`

func TestParseChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		header      string
		wantScheme  string
		wantRealm   string
		wantService string
		wantScopes  []string
		wantErr     error
	}{
		{
			name:        "a comma inside a quoted scope is part of the scope",
			header:      ghcrChallenge,
			wantScheme:  schemeBearer,
			wantRealm:   "https://ghcr.io/token",
			wantService: "ghcr.io",
			wantScopes:  []string{"repository:team/artifact:pull,push"},
		},
		{
			name:       "a bearer challenge may carry nothing but a realm",
			header:     `Bearer realm="https://auth.example.com/token"`,
			wantScheme: schemeBearer,
			wantRealm:  "https://auth.example.com/token",
		},
		{
			name:       "a basic challenge is answered when it is all there is",
			header:     `Basic realm="registry"`,
			wantScheme: schemeBasic,
			wantRealm:  "registry",
		},
		{
			name:        "bearer wins wherever in the list it appears",
			header:      `Basic realm="registry", Bearer realm="https://auth.example.com/token",service="reg"`,
			wantScheme:  schemeBearer,
			wantRealm:   "https://auth.example.com/token",
			wantService: "reg",
		},
		{
			name:       "bearer wins when it comes first as well",
			header:     `Bearer realm="https://auth.example.com/token", Basic realm="registry"`,
			wantScheme: schemeBearer,
			wantRealm:  "https://auth.example.com/token",
		},
		{
			name:        "a backslash escape stands for the character after it",
			header:      `Bearer realm="https://auth.example.com/token",service="a \"quoted\" name"`,
			wantScheme:  schemeBearer,
			wantRealm:   "https://auth.example.com/token",
			wantService: `a "quoted" name`,
		},
		{
			name:        "scheme and parameter names are matched without regard to case",
			header:      `bEaReR ReAlM="https://auth.example.com/token",SERVICE="reg",Scope="repository:a:pull"`,
			wantScheme:  schemeBearer,
			wantRealm:   "https://auth.example.com/token",
			wantService: "reg",
			wantScopes:  []string{"repository:a:pull"},
		},
		{
			name:        "parameters are read in whatever order they arrive",
			header:      `Bearer scope="repository:a:pull",service="reg",realm="https://auth.example.com/token"`,
			wantScheme:  schemeBearer,
			wantRealm:   "https://auth.example.com/token",
			wantService: "reg",
			wantScopes:  []string{"repository:a:pull"},
		},
		{
			name:       "a scope parameter carries a space separated list",
			header:     `Bearer realm="https://auth.example.com/token",scope="repository:a:pull repository:b:pull,push"`,
			wantScheme: schemeBearer,
			wantRealm:  "https://auth.example.com/token",
			wantScopes: []string{"repository:a:pull", "repository:b:pull,push"},
		},
		{
			name:        "an unquoted parameter value is a token",
			header:      `Bearer realm="https://auth.example.com/token",service=reg`,
			wantScheme:  schemeBearer,
			wantRealm:   "https://auth.example.com/token",
			wantService: "reg",
		},
		{
			name:    "an unquoted value carrying characters a token cannot hold is refused",
			header:  `Bearer realm=https://auth.example.com/token`,
			wantErr: errUnreadableChallenge,
		},
		{
			name:        "extra whitespace and empty list elements are skipped",
			header:      `  Bearer  realm = "https://auth.example.com/token" , , service = "reg"  `,
			wantScheme:  schemeBearer,
			wantRealm:   "https://auth.example.com/token",
			wantService: "reg",
		},
		{name: "an absent header is not a challenge", header: "", wantErr: errNoChallenge},
		{
			name:    "a bearer challenge naming no realm is refused",
			header:  `Bearer service="reg",scope="repository:a:pull"`,
			wantErr: errBearerNoRealm,
		},
		{
			name:    "a bearer challenge naming no realm is refused even beside a usable basic one",
			header:  `Bearer service="reg", Basic realm="registry"`,
			wantErr: errBearerNoRealm,
		},
		{
			name:    "a scheme this package does not implement is refused",
			header:  `Negotiate realm="https://auth.example.com/token"`,
			wantErr: errUnknownScheme,
		},
		{
			name:    "an unterminated quoted string is refused",
			header:  `Bearer realm="https://auth.example.com/token`,
			wantErr: errUnknownScheme,
		},
		{
			name:    "a parameter before any scheme is refused",
			header:  `realm="https://auth.example.com/token"`,
			wantErr: errUnreadableChallenge,
		},
		{
			name:    "a header made of characters the grammar has no place for is refused",
			header:  `((((`,
			wantErr: errUnreadableChallenge,
		},
		{
			name:    "nine kilobytes of garbage is not read at all",
			header:  strings.Repeat("x", 9<<10),
			wantErr: errChallengeTooLarge(9 << 10),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertParsedChallenge(t, tc.header, tc.wantScheme, tc.wantRealm, tc.wantService, tc.wantScopes, tc.wantErr)
		})
	}
}

func assertParsedChallenge(
	t *testing.T,
	header, wantScheme, wantRealm, wantService string,
	wantScopes []string,
	wantErr error,
) {
	t.Helper()

	asked, err := parseChallenge(header)
	if wantErr != nil {
		if err == nil {
			t.Fatal("parseChallenge succeeded, want error")
		}
		if err.Error() != wantErr.Error() {
			t.Fatalf("error = %q, want %q", err, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("parseChallenge: %v", err)
	}
	if asked.scheme != wantScheme {
		t.Fatalf("scheme = %q, want %q", asked.scheme, wantScheme)
	}
	if asked.realm != wantRealm {
		t.Fatalf("realm = %q, want %q", asked.realm, wantRealm)
	}
	if asked.service != wantService {
		t.Fatalf("service = %q, want %q", asked.service, wantService)
	}
	if !stringSliceEqual(asked.scopes, wantScopes) {
		t.Fatalf("scopes = %q, want %q", asked.scopes, wantScopes)
	}
}

func TestParseChallengeDoesNotRepeatWhatItCouldNotRead(t *testing.T) {
	t.Parallel()

	const secret = "malformed-challenge-secret"
	_, err := parseChallenge("Negotiate " + secret + strings.Repeat("z", challengeLimit-20))
	if err == nil {
		t.Fatal("parseChallenge succeeded, want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error repeats peer-controlled bytes: %v", err)
	}
	if strings.Contains(err.Error(), "Negotiate") {
		t.Fatalf("error repeats the scheme: %v", err)
	}
}

func TestChallengeHeaderJoinsMultipleFieldLines(t *testing.T) {
	t.Parallel()

	resp := &http.Response{Header: make(http.Header)}
	resp.Header.Add(headerChallenge, `Basic realm="registry"`)
	resp.Header.Add(headerChallenge, `Bearer realm="https://auth.example.com/token",service="reg"`)

	asked, err := parseChallenge(challengeHeader(resp))
	if err != nil {
		t.Fatalf("parseChallenge: %v", err)
	}
	if asked.scheme != schemeBearer {
		t.Fatalf("scheme = %q, want bearer (joined lines must still see Bearer)", asked.scheme)
	}
	if asked.service != "reg" {
		t.Fatalf("service = %q, want reg", asked.service)
	}
}

func TestParseRealm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		realm   string
		wantErr bool
	}{
		{name: "an absolute https realm is the token endpoint", realm: "https://auth.example.com/token"},
		{name: "an absolute http realm is allowed", realm: "http://127.0.0.1:5000/token"},
		{name: "a realm on a host other than the registry is allowed", realm: "https://auth.docker.io/token"},
		{name: "a relative realm is refused", realm: "/token", wantErr: true},
		{name: "a missing host is refused", realm: "https:///token", wantErr: true},
		{name: "a missing scheme is refused", realm: "auth.example.com/token", wantErr: true},
		{name: "a non-http scheme is refused", realm: "ftp://auth.example.com/token", wantErr: true},
		{name: "userinfo is refused", realm: "https://user:pass@auth.example.com/token", wantErr: true},
		{name: "a fragment is refused", realm: "https://auth.example.com/token#frag", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseRealm(tc.realm)
			if tc.wantErr {
				if err == nil {
					t.Fatal("parseRealm succeeded, want error")
				}
				if strings.Contains(err.Error(), tc.realm) {
					t.Fatalf("error repeats the realm: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRealm: %v", err)
			}
			if got.String() != tc.realm {
				t.Fatalf("url = %q, want %q", got, tc.realm)
			}
		})
	}
}

func TestChallengeHeaderOnARealResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add(headerChallenge, `Bearer realm="https://auth.example.com/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	asked, err := parseChallenge(challengeHeader(resp))
	if err != nil {
		t.Fatalf("parseChallenge: %v", err)
	}
	if asked.realm != "https://auth.example.com/token" {
		t.Fatalf("realm = %q", asked.realm)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
