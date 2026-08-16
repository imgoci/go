package registry

import (
	"errors"
	"net/http"
	"time"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/retry"
	"github.com/imgoci/go/internal/transfer"
)

const (
	// schemeHTTPS is the default registry URL scheme.
	schemeHTTPS = "https"
	// schemeHTTP is the scheme [Config.PlainHTTP] selects for local registries.
	schemeHTTP = "http"
)

// Config is the construction-time settings for one repository adapter.
type Config struct {
	// Host is the registry authority (host or host:port) with no scheme.
	Host string
	// Repository is the repository path under /v2/, such as "os/example".
	Repository string
	// HTTPClient supplies the base [http.RoundTripper] and Timeout. Nil uses
	// [http.DefaultTransport] and no client timeout.
	HTTPClient *http.Client
	// PlainHTTP selects http:// registry URLs instead of https://. Meant for
	// local registries served without TLS.
	PlainHTTP bool
	// Credentials resolves the credential presented to Host. Nil is the
	// anonymous credential: the bearer exchange still runs.
	Credentials auth.Credentials
	// UnverifiedExternalTransport authorizes using an opaque
	// [http.RoundTripper] for registry-selected storage traffic. A concrete
	// *[http.Transport] is cloned, used without credentials, and identity-
	// wrapped without this flag. An opaque base fails at construction unless
	// this is set; with it, the opaque transport is used for storage traffic
	// and still identity-wrapped unconditionally.
	UnverifiedExternalTransport bool
}

// Client is the distribution adapter for one host and repository.
//
// It implements [transfer.Manifests]. Blob operations are on the value
// [Client.Blobs] returns.
type Client struct {
	// host is the registry authority credentials and URLs use.
	host string
	// repository is the path under /v2/.
	repository string
	// scheme is http or https, from [Config.PlainHTTP].
	scheme string
	// http is the authenticated, unconditionally identity-wrapped client
	// manifest GET and PUT use. Redirect hops stay in scope because the
	// wrapper is not path-filtered.
	http *http.Client
	// blobs is the go-oci-blob adapter bound to the same repository.
	blobs transfer.Blobs
	// retry is the policy [retry.Do] uses for Get, Put, Exists, and Pull.
	// The zero value is [retry.Default]. Tests replace Sleep and Rand so
	// waits do not block.
	retry retry.Policy
}

// New builds a [Client] for cfg.
//
// The registry-side stack is: cfg's base transport (or the default), then
// path-scoped identity enforcement, then [auth.Transport] whose RealmClient
// is a plain [http.Client] on the unwrapped base. Realm isolation is
// therefore structural: token GETs never pass through identityTransport.
// The manifest client wraps that authenticated stack with an unconditional
// identity decorator so a 302 onto an off-path URL is still identity-coded.
func New(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, errors.New("registry host is required")
	}
	if cfg.Repository == "" {
		return nil, errors.New("repository is required")
	}

	stacks, err := buildTransports(cfg)
	if err != nil {
		return nil, err
	}

	client := &Client{
		host:       cfg.Host,
		repository: cfg.Repository,
		scheme:     registryScheme(cfg.PlainHTTP),
		http: &http.Client{
			Transport: newStorageIdentityTransport(stacks.registry),
			Timeout:   stacks.timeout,
		},
	}
	client.blobs = newBlobAdapter(cfg, stacks, client)

	return client, nil
}

// Manifests returns the adapter as a [transfer.Manifests] port.
func (c *Client) Manifests() transfer.Manifests {
	return c
}

// Blobs returns the go-oci-blob adapter bound to this repository.
func (c *Client) Blobs() transfer.Blobs {
	return c.blobs
}

// transportStacks is the pair of RoundTrippers [New] hands to the manifest
// client and to go-oci-blob.
type transportStacks struct {
	// registry is auth wrapping path-scoped identity wrapping base. The
	// manifest client wraps this again with unconditional identity; go-oci-blob
	// uses it as its registry transport so upload sessions stay unscoped.
	registry http.RoundTripper
	// storage is unconditional identity wrapping the credential-stripped
	// cloned (or explicitly authorized opaque) base.
	storage http.RoundTripper
	// timeout is copied from [Config.HTTPClient].
	timeout time.Duration
}

// buildTransports assembles the registry and storage stacks from cfg.
func buildTransports(cfg Config) (transportStacks, error) {
	base, timeout := baseTransport(cfg)
	storage, err := storageTransport(base, cfg.UnverifiedExternalTransport)
	if err != nil {
		return transportStacks{}, err
	}

	identity := newIdentityTransport(base)
	registry := &auth.Transport{
		Base:        identity,
		Host:        cfg.Host,
		Credentials: cfg.Credentials,
		RealmClient: &http.Client{
			Transport:     base,
			Timeout:       timeout,
			CheckRedirect: refuseRedirect,
		},
	}

	return transportStacks{
		registry: registry,
		storage:  newStorageIdentityTransport(storage),
		timeout:  timeout,
	}, nil
}

// baseTransport returns cfg's RoundTripper and Timeout, filling defaults.
func baseTransport(cfg Config) (http.RoundTripper, time.Duration) {
	if cfg.HTTPClient == nil {
		return http.DefaultTransport, 0
	}
	base := cfg.HTTPClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	return base, cfg.HTTPClient.Timeout
}

// storageTransport is the credential-stripped base used for redirected blob
// traffic. A concrete *[http.Transport] is cloned so storage has its own
// pool and none of the registry credential state. An opaque RoundTripper
// cannot be cloned or inspected: construction fails unless unverified is
// set, in which case the opaque base is used as-is. Identity wrapping
// happens at the caller.
func storageTransport(base http.RoundTripper, unverified bool) (http.RoundTripper, error) {
	if transport, ok := base.(*http.Transport); ok {
		return transport.Clone(), nil
	}
	if unverified {
		return base, nil
	}

	return nil, errors.New("opaque HTTP transport requires WithUnverifiedExternalTransport")
}

// registryScheme is the URL scheme registry requests use.
func registryScheme(plainHTTP bool) string {
	if plainHTTP {
		return schemeHTTP
	}

	return schemeHTTPS
}

// refuseRedirect is the realm client's CheckRedirect: a redirected token
// endpoint is a shape no measured registry has, and failing loudly beats
// deciding where a credential goes next on a token server's say-so.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}
