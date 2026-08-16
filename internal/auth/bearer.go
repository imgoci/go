package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The query parameters a token request carries beside whatever the realm
// already had.
const (
	// tokenServiceParam names the registry the token is for, in the issuer's
	// own vocabulary rather than this package's.
	tokenServiceParam = "service"
	// tokenScopeParam names one access grant. It repeats, once per grant.
	tokenScopeParam = "scope"
)

// tokenBodyLimit caps the token endpoint's answer. A token document is a few
// hundred bytes; anything past this is not one.
const tokenBodyLimit = 64 << 10

// ErrAuth reports that authentication failed in a way repeating the request
// cannot fix: the registry asked for a scheme or credential this client
// cannot supply, the token realm redirected, or the token endpoint refused
// the exchange. Registry adapters map it onto transfer.ErrUnauthorized.
var ErrAuth = errors.New("authentication failed")

// authError is a terminal authentication failure that wraps [ErrAuth].
type authError string

// Error renders the structural diagnosis without admitting registry-controlled
// bytes.
func (e authError) Error() string {
	return string(e)
}

// Unwrap exposes [ErrAuth] so callers can match the class without naming
// each cause.
func (e authError) Unwrap() error {
	return ErrAuth
}

const (
	errBasicNoCredential authError      = "the registry asked for a user name and password and none is configured"
	errNotTokenDocument  challengeError = "the token endpoint's answer is not a token document"
	errNoToken           challengeError = "the token endpoint answered with a document carrying no token"
	errRealmRedirect     authError      = "the token realm redirected the request, which this package does not follow"
	errCannotReplay      challengeError = "the request body cannot be sent again to answer a challenge"
)

// Transport authenticates registry HTTP requests as a [http.RoundTripper]
// decorator.
//
// A 401 with a WWW-Authenticate challenge becomes a token GET against the
// named realm (service and scope as query parameters), optionally with static
// Basic credentials, and the resulting bearer token is cached until expiry.
// The original request is then retried once with the Bearer token.
//
// [Transport.RealmClient] is deliberately outside identity enforcement. Realm
// requests are issued with RealmClient.Do and never through Base.RoundTrip.
type Transport struct {
	// Base is the registry-facing RoundTripper the adapter may wrap with
	// identity enforcement. Nil is [http.DefaultTransport]. Realm requests
	// never pass through Base.
	Base http.RoundTripper
	// Host is the registry host credentials apply to. A request whose host
	// differs — typically a blob redirect onto object storage — has
	// Authorization stripped and receives no credential.
	Host string
	// Credentials resolves the credential presented to Host. Nil is the
	// anonymous credential: the bearer exchange still runs.
	Credentials Credentials
	// RealmClient is the HTTP client used only for token-realm GETs. It is
	// deliberately outside identity enforcement: a compressing token realm keeps
	// working while identity is enforced on manifest and blob GETs. Nil is a plain
	// [http.Client] that does not follow redirects.
	RealmClient *http.Client

	// now is the clock token expiry is measured against. Nil is [time.Now].
	now func() time.Time
	// init guards cache and plainRealm.
	init sync.Once
	// cache holds bearer tokens keyed by realm, service, and scope.
	cache *tokenCache
	// plainRealm is the default RealmClient, built once.
	plainRealm *http.Client
	// mu guards last.
	mu sync.Mutex
	// last is the most recent challenge this transport answered.
	last challenge
}

// tokenResponse is the token endpoint's answer, reduced to what this package
// reads off it.
type tokenResponse struct {
	// Token is the bearer token under the distribution spec's field name.
	Token string `json:"token"`
	// AccessToken is the same value under the OAuth2 field name. Registries
	// send one, the other, or both, so both are read and the spec's name wins.
	AccessToken string `json:"access_token"`
	// ExpiresIn is how many seconds the token is good for. An absent field
	// reads as zero, which takes the spec's default.
	ExpiresIn int `json:"expires_in"`
	// IssuedAt is the token endpoint's clock as RFC 3339. An absent or
	// unreadable value is treated as now.
	IssuedAt string `json:"issued_at"`
}

// RoundTrip authenticates req against [Transport.Host] and sends it through
// [Transport.Base].
//
// An off-origin request has Authorization stripped and is not challenged. An
// on-origin 401 is answered at most once: the challenge is parsed, a token is
// fetched (or a Basic header is built), and the original request is retried
// with that credential.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	outbound := req.Clone(req.Context())
	if !t.onOrigin(outbound) {
		outbound.Header.Del(headerAuthorization)
		return t.base().RoundTrip(outbound)
	}

	if err := t.authorize(outbound); err != nil {
		return nil, err
	}

	resp, err := t.base().RoundTrip(outbound)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	return t.retryWithChallenge(req, outbound, resp)
}

// retryWithChallenge answers a 401 once and re-issues orig. A credential that
// was already presented returns the 401 as-is. A challenge this package cannot
// read is an error: the body is closed and nothing is retried.
func (t *Transport) retryWithChallenge(orig, sent *http.Request, resp *http.Response) (*http.Response, error) {
	asked, err := parseChallenge(challengeHeader(resp))
	if err != nil {
		closeBody(resp)
		return nil, err
	}

	presented := sent.Header.Get(headerAuthorization)
	header, err := t.answer(orig.Context(), asked, presented)
	if err != nil {
		closeBody(resp)
		return nil, err
	}
	if header == "" || header == presented {
		return resp, nil
	}

	closeBody(resp)
	t.remember(asked)

	retry := orig.Clone(orig.Context())
	if err := rewindBody(retry, orig); err != nil {
		return nil, err
	}
	retry.Header.Set(headerAuthorization, header)

	return t.base().RoundTrip(retry)
}

// authorize attaches a cached bearer token or remembered Basic credential to
// an on-origin request. The first request against a registry that has not yet
// challenged goes out carrying nothing.
func (t *Transport) authorize(req *http.Request) error {
	asked := t.remembered()
	switch asked.scheme {
	case "":
		return nil
	case schemeBearer:
		token := t.tokens().get(asked.realm, asked.service, scopeKey(asked.scopes), t.clock())
		if token != "" {
			req.Header.Set(headerAuthorization, bearerHeader(token))
		}
		return nil
	case schemeBasic:
		cred, err := t.lookup(req.Context())
		if err != nil {
			return err
		}
		if !cred.Empty() {
			req.SetBasicAuth(cred.Username, cred.Password)
		}
		return nil
	default:
		return nil
	}
}

// answer produces the Authorization header that responds to asked. presented
// is what the refused request already carried, empty on the first challenge.
func (t *Transport) answer(ctx context.Context, asked challenge, presented string) (string, error) {
	cred, err := t.lookup(ctx)
	if err != nil {
		return "", err
	}

	switch asked.scheme {
	case schemeBasic:
		return basicHeader(cred)
	case schemeBearer:
		return t.bearerAnswer(ctx, asked, cred, presented)
	default:
		return "", errUnknownScheme
	}
}

// bearerAnswer returns a Bearer header for asked, from the cache or from a
// realm GET. A presented token that was just refused is dropped before the
// exchange so the retry cannot replay it.
func (t *Transport) bearerAnswer(
	ctx context.Context,
	asked challenge,
	cred Credential,
	presented string,
) (string, error) {
	scope := scopeKey(asked.scopes)
	if presented != "" {
		t.tokens().invalidate(asked.realm, asked.service, scope)
	} else if token := t.tokens().get(asked.realm, asked.service, scope, t.clock()); token != "" {
		return bearerHeader(token), nil
	}

	token, until, err := t.exchange(ctx, asked, cred)
	if err != nil {
		return "", err
	}

	t.tokens().put(asked.realm, asked.service, scope, token, until)

	return bearerHeader(token), nil
}

// exchange asks the realm a bearer challenge named for a token covering the
// challenge's scopes.
//
// It is a GET carrying the credential in a Basic header, which is what the
// distribution spec's token flow defines. The exchange rides [Transport.RealmClient],
// never [Transport.Base], so identity enforcement on registry GETs cannot
// touch it. A redirect from the default realm client is terminal.
func (t *Transport) exchange(ctx context.Context, asked challenge, cred Credential) (string, time.Time, error) {
	endpoint, err := parseRealm(asked.realm)
	if err != nil {
		return "", time.Time{}, err
	}
	endpoint.RawQuery = tokenQuery(endpoint, asked.service, asked.scopes)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", time.Time{}, errBadRealm
	}
	if !cred.Empty() {
		req.SetBasicAuth(cred.Username, cred.Password)
	}

	resp, err := t.realmHTTP().Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer closeBody(resp)

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, tokenStatusError(resp.StatusCode)
	}

	return readToken(resp, t.clock())
}

// lookup resolves the credential for [Transport.Host]. A nil resolver is the
// anonymous credential.
func (t *Transport) lookup(ctx context.Context) (Credential, error) {
	if t.Credentials == nil {
		return Credential{}, nil
	}

	return t.Credentials.Credential(ctx, t.Host)
}

// onOrigin reports whether req is for [Transport.Host]. An empty Host treats
// every request as on-origin.
func (t *Transport) onOrigin(req *http.Request) bool {
	if t.Host == "" {
		return true
	}

	return sameRegistry(t.Host, requestHost(req))
}

// base is [Transport.Base], or [http.DefaultTransport] when Base is nil.
func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}

	return http.DefaultTransport
}

// realmHTTP is [Transport.RealmClient], or a plain client that does not follow
// redirects. The default is built once and never wraps [Transport.Base].
func (t *Transport) realmHTTP() *http.Client {
	if t.RealmClient != nil {
		return t.RealmClient
	}

	t.ensure()

	return t.plainRealm
}

// tokens is the bearer cache, created on first use.
func (t *Transport) tokens() *tokenCache {
	t.ensure()

	return t.cache
}

// clock is the instant token expiry is measured against.
func (t *Transport) clock() time.Time {
	if t.now != nil {
		return t.now()
	}

	return time.Now()
}

// ensure builds the default cache and realm client exactly once.
func (t *Transport) ensure() {
	t.init.Do(func() {
		if t.cache == nil {
			t.cache = newTokenCache()
		}
		if t.plainRealm == nil {
			t.plainRealm = &http.Client{CheckRedirect: refuseRealmRedirect}
		}
	})
}

// remember records the challenge later requests reuse without waiting for
// another 401.
func (t *Transport) remember(asked challenge) {
	t.mu.Lock()
	t.last = asked
	t.mu.Unlock()
}

// remembered returns the last challenge this transport answered.
func (t *Transport) remembered() challenge {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.last
}

// tokenQuery renders a token request's query: whatever the realm already
// carried, the service the challenge named, and one scope parameter per grant.
func tokenQuery(endpoint *url.URL, service string, scopes []string) string {
	query := endpoint.Query()
	if service != "" {
		query.Set(tokenServiceParam, service)
	}
	for _, one := range scopes {
		query.Add(tokenScopeParam, one)
	}

	return query.Encode()
}

// readToken reads a token out of the endpoint's answer.
func readToken(resp *http.Response, now time.Time) (string, time.Time, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, tokenBodyLimit+1))
	if err != nil {
		return "", time.Time{}, err
	}
	if len(body) > tokenBodyLimit {
		return "", time.Time{}, errTokenTooLarge(tokenBodyLimit)
	}

	var answer tokenResponse
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", time.Time{}, errNotTokenDocument
	}

	token := answer.Token
	if token == "" {
		token = answer.AccessToken
	}
	if token == "" {
		return "", time.Time{}, errNoToken
	}

	return token, expiryOf(answer.ExpiresIn, answer.IssuedAt, now), nil
}

// basicHeader answers a Basic challenge with the credential's user name and
// secret.
func basicHeader(cred Credential) (string, error) {
	if cred.Empty() {
		return "", errBasicNoCredential
	}

	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred.Username+":"+cred.Password)), nil
}

// bearerHeader renders a bearer token as an Authorization header value.
func bearerHeader(token string) string {
	return "Bearer " + token
}

// scopeKey joins the challenge's scopes the way they are cached: one string,
// space-separated, in the order the challenge listed them.
func scopeKey(scopes []string) string {
	return strings.Join(scopes, " ")
}

// rewindBody gives dst a fresh Body from src's GetBody, which is what a retry
// needs after Base has already consumed the first attempt.
func rewindBody(dst, src *http.Request) error {
	if src.Body == nil || src.Body == http.NoBody {
		return nil
	}
	if src.GetBody == nil {
		return errCannotReplay
	}

	body, err := src.GetBody()
	if err != nil {
		return err
	}
	dst.Body = body

	return nil
}

// closeBody drains and closes a response so the connection can be reused.
func closeBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, tokenBodyLimit+1))
	_ = resp.Body.Close()
}

// refuseRealmRedirect is the default [http.Client.CheckRedirect]. A redirected
// token endpoint is an error: this package does not follow the redirect, so a
// token server cannot choose where the credential is sent next.
func refuseRealmRedirect(*http.Request, []*http.Request) error {
	return errRealmRedirect
}

// tokenStatusError reports a token endpoint's status without admitting its
// path or response body into the public error.
func tokenStatusError(status int) error {
	return authError("the token endpoint answered with status " + strconv.Itoa(status))
}

// errTokenTooLarge reports a token document past [tokenBodyLimit] without
// quoting its contents.
func errTokenTooLarge(n int) error {
	return challengeError("the token endpoint's answer is larger than the " +
		strconv.Itoa(n) + " byte limit")
}
