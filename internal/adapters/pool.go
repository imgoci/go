package adapters

import (
	"context"
	"net/http"
	"sync"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/transfer"
)

// Config is the transfer-wide configuration a pool opens adapters with.
type Config struct {
	// HTTPClient sends every registry request, nil when the caller named none.
	HTTPClient *http.Client
	// PlainHTTP talks http:// to the registry instead of https://.
	PlainHTTP bool
	// UnverifiedExternalTransport authorizes registry-selected cross-host
	// requests through a custom dial hook, proxy, or opaque transport whose
	// final destination the adapter cannot verify.
	UnverifiedExternalTransport bool
	// Credentials is the resolved credential source the caller supplies, nil
	// for anonymous.
	Credentials auth.Credentials
}

// Ports is the Manifests, Blobs, and Multipart triple for one repository.
type Ports struct {
	// Manifests is the distribution-spec manifest surface.
	Manifests transfer.Manifests
	// Blobs is the distribution-spec blob surface.
	Blobs transfer.Blobs
	// Multipart is the BigOCI surface. Not bound to the repository at
	// construction; [transfer.Multipart.Push] takes repo per call.
	Multipart transfer.Multipart
}

// Factory constructs ports for one host and repository.
//
// ctx bounds Docker credential helper lookups during construction.
type Factory func(ctx context.Context, host, repository string, cfg Config) (Ports, error)

// adapterKey identifies one cached repository adapter.
type adapterKey struct {
	// host is the registry domain, including a port when present.
	host string
	// repository is the path under /v2.
	repository string
}

// Pool caches constructed adapter ports per host and repository.
//
// The configuration is supplied per call rather than held here, because the
// caller owns it: a client reads its own settings when it asks for ports.
type Pool struct {
	// factory constructs ports for one repository. Nil means
	// [Open].
	factory Factory
	// mu guards adapters.
	mu sync.Mutex
	// adapters caches Manifests, Blobs, and Multipart ports per host+repository.
	adapters map[adapterKey]Ports
}

// NewPool returns a pool that opens adapters through factory. A nil factory means Open.
func NewPool(factory Factory) *Pool {
	return &Pool{
		factory:  factory,
		adapters: make(map[adapterKey]Ports),
	}
}

// PortsFor returns cached ports for host and repository, constructing them
// with cfg on first use.
//
// ctx bounds Docker credential helper lookups during adapter construction.
func (p *Pool) PortsFor(ctx context.Context, host, repository string, cfg Config) (Ports, error) {
	key := adapterKey{host: host, repository: repository}
	p.mu.Lock()
	defer p.mu.Unlock()
	if ports, ok := p.adapters[key]; ok {
		return ports, nil
	}

	factory := p.factory
	if factory == nil {
		factory = Open
	}
	ports, err := factory(ctx, host, repository, cfg)
	if err != nil {
		return Ports{}, err
	}
	p.adapters[key] = ports

	return ports, nil
}
