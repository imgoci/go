package imgoci

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/registry"
	"github.com/imgoci/go/internal/transfer"
)

// Client fetches imgoci releases from OCI registries.
//
// A Client holds immutable transfer-wide settings and lazily constructed
// per-repository adapters, cached under a mutex keyed by host and repository
// path. [New] builds one. [WithDockerCredentials] is deferred to a later
// slice; until then authentication is anonymous or [WithCredentials].
type Client struct {
	// settings are the option-configurable parts applied by [New].
	settings clientSettings
	// mu guards adapters.
	mu sync.Mutex
	// adapters caches Manifests and Blobs ports per host+repository.
	adapters map[adapterKey]adapterPorts
	// newAdapter constructs ports for one repository. Nil means
	// [defaultAdapter]. Tests assign this unexported field; it is not a
	// public knob.
	newAdapter adapterFactory
}

// clientSettings are the option-configurable parts of a client, collected so
// [New] can apply every option before it builds the immutable [Client].
type clientSettings struct {
	// httpClient sends every registry request, nil when the caller named none.
	httpClient *http.Client
	// plainHTTP talks http:// to the registry instead of https://.
	plainHTTP bool
	// allowUnverifiedExternal authorizes registry-selected cross-host
	// requests through a custom dial hook, proxy, or opaque transport whose
	// final destination the adapter cannot verify.
	allowUnverifiedExternal bool
	// credentials resolves the credential presented to a registry, nil for
	// anonymous.
	credentials auth.Credentials
}

// adapterKey identifies one cached repository adapter.
type adapterKey struct {
	// host is the registry domain, including a port when present.
	host string
	// repository is the path under /v2.
	repository string
}

// adapterPorts is the Manifests and Blobs pair for one repository.
type adapterPorts struct {
	// manifests is the distribution-spec manifest surface.
	manifests transfer.Manifests
	// blobs is the distribution-spec blob surface.
	blobs transfer.Blobs
}

// adapterFactory constructs Manifests and Blobs for one host and repository.
type adapterFactory func(host, repository string, settings clientSettings) (adapterPorts, error)

// Option configures a [Client] as [New] builds it.
//
// The function signature names a type this package keeps to itself, which
// seals the set: the only Options that exist are the ones declared here, so
// [New] can never be handed a knob the client does not know how to honor.
type Option func(*clientSettings)

// New returns a client configured by opts.
//
// A client built with no credential option is not a client with
// authentication turned off. A registry that asks for a token still gets the
// full exchange, made anonymously, which is what registries that hand out
// public-read tokens expect. It only means this client has no user name or
// secret to offer when the exchange asks for one.
func New(opts ...Option) (*Client, error) {
	var settings clientSettings
	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}

	return &Client{
		settings: settings,
		adapters: make(map[adapterKey]adapterPorts),
	}, nil
}

// WithHTTPClient sends every registry request with client instead of the
// default one.
//
// This is the seam for timeouts, proxies, connection pool tuning, and a
// credential source this package does not have. A nil client is ignored, so
// a caller may pass one through unconditionally.
func WithHTTPClient(client *http.Client) Option {
	return func(s *clientSettings) {
		if client != nil {
			s.httpClient = client
		}
	}
}

// WithPlainHTTP talks http:// to the registry instead of https://.
//
// Everything a transfer sends rides unencrypted under it, credentials and
// token exchanges included, which is one more reason it is for local
// registries only.
func WithPlainHTTP() Option {
	return func(s *clientSettings) {
		s.plainHTTP = true
	}
}

// WithCredentials presents username and secret to whatever registry a
// transfer dials.
//
// It is the direct route, for a caller who already holds the secret: a CI
// job with a registry token in its environment, or a program that reads its
// own configuration. Nothing is looked up, no file is read, and no program
// is run. secret is a password, or — at most registries today — a personal
// access token.
//
// Every registry is the deliberate part. The credential goes to whatever
// host the reference names, so the caller, who chose both the secret and the
// reference, is the one deciding who sees it.
func WithCredentials(username, secret string) Option {
	return func(s *clientSettings) {
		s.credentials = auth.NewStatic(auth.Credential{
			Username: username,
			Password: secret,
		})
	}
}

// WithUnverifiedExternalTransport authorizes an opaque [http.RoundTripper]
// as the storage transport for registry-selected cross-host blob traffic.
//
// Without this option a concrete *[http.Transport] base is cloned, used
// without credentials, and identity-wrapped; an opaque base fails at
// adapter construction. With it, the opaque transport is used for storage
// traffic and is still identity-wrapped unconditionally. The option never
// disables TLS verification.
func WithUnverifiedExternalTransport() Option {
	return func(s *clientSettings) {
		s.allowUnverifiedExternal = true
	}
}

// Capabilities reports what this built client can retrieve conformingly.
//
// The set is [StandardCapabilities] until slice-5 fixtures pin BigOCI as a
// compile-time fact of the dependency. BigOCI is never assumed.
func (c *Client) Capabilities() Capabilities {
	return StandardCapabilities()
}

// portsFor returns cached Manifests and Blobs ports for host and repository,
// constructing them on first use.
func (c *Client) portsFor(host, repository string) (adapterPorts, error) {
	key := adapterKey{host: host, repository: repository}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ports, ok := c.adapters[key]; ok {
		return ports, nil
	}

	factory := c.newAdapter
	if factory == nil {
		factory = defaultAdapter
	}
	ports, err := factory(host, repository, c.settings)
	if err != nil {
		return adapterPorts{}, err
	}
	c.adapters[key] = ports

	return ports, nil
}

// defaultAdapter opens a registry adapter for one host and repository.
func defaultAdapter(host, repository string, settings clientSettings) (adapterPorts, error) {
	client, err := registry.New(registry.Config{
		Host:                        host,
		Repository:                  repository,
		HTTPClient:                  settings.httpClient,
		PlainHTTP:                   settings.plainHTTP,
		Credentials:                 settings.credentials,
		UnverifiedExternalTransport: settings.allowUnverifiedExternal,
	})
	if err != nil {
		return adapterPorts{}, fmt.Errorf("open registry %s/%s: %w", host, repository, err)
	}

	return adapterPorts{
		manifests: client.Manifests(),
		blobs:     client.Blobs(),
	}, nil
}
