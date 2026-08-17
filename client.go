package imgoci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/multipart"
	"github.com/imgoci/go/internal/registry"
	"github.com/imgoci/go/internal/transfer"
)

// Client fetches imgoci releases from OCI registries.
//
// A Client holds immutable transfer-wide settings and lazily constructed
// per-repository adapters, cached under a mutex keyed by host and repository
// path. [New] builds one. Authentication is anonymous unless
// [WithCredentials] or [WithDockerCredentials] is named.
type Client struct {
	// settings are the option-configurable parts applied by [New].
	settings clientSettings
	// mu guards adapters.
	mu sync.Mutex
	// adapters caches Manifests, Blobs, and Multipart ports per host+repository.
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
	// credentials builds the source a transfer resolves credentials through,
	// nil when no option named one. It is a builder rather than a built source
	// because building one can fail — reading the Docker configuration is the
	// case — and [New] is where a caller can still be told about it.
	credentials func() (auth.Credentials, error)
	// resolved is the credential source [New] built from credentials, nil
	// for anonymous.
	resolved auth.Credentials
	// decoderMaxWindow is the working-set ceiling one decompressor may
	// allocate, applied to every decode a transfer performs. [New] defaults
	// it to [decomp.DefaultDecoderMaxWindow] and rejects zero.
	decoderMaxWindow uint64
}

// adapterKey identifies one cached repository adapter.
type adapterKey struct {
	// host is the registry domain, including a port when present.
	host string
	// repository is the path under /v2.
	repository string
}

// adapterPorts is the Manifests, Blobs, and Multipart triple for one repository.
type adapterPorts struct {
	// manifests is the distribution-spec manifest surface.
	manifests transfer.Manifests
	// blobs is the distribution-spec blob surface.
	blobs transfer.Blobs
	// multipart is the BigOCI surface. Not bound to the repository at
	// construction; [transfer.Multipart.Push] takes repo per call.
	multipart transfer.Multipart
}

// adapterFactory constructs Manifests, Blobs, and Multipart for one host and repository.
//
// ctx bounds Docker credential helper lookups during construction.
type adapterFactory func(ctx context.Context, host, repository string, settings clientSettings) (adapterPorts, error)

// Option configures a [Client] as [New] builds it.
//
// The function signature names a type this package keeps to itself, which seals
// the set: the only Options that exist are the ones declared here, so [New]
// cannot be handed a knob it does not honor.
type Option func(*clientSettings)

// New returns a client configured by opts.
//
// It reports an error when an option cannot be applied: [WithDockerCredentials]
// records the intent to use the credentials `docker login` stores, and this is
// where they are read. A configuration file that is not there — or a machine
// that cannot even name where one would be, with no home directory and no
// $DOCKER_CONFIG — is not an error: that is a machine nobody has logged in on,
// and every registry resolves to the anonymous credential. A file that exists
// but cannot be read as a configuration is, because a caller who asked this
// client to use their credentials would otherwise watch it transfer without
// them and fail somewhere less obvious.
//
// A client built with no credential option is not a client with
// authentication turned off. A registry that asks for a token still gets the
// full exchange, made anonymously, which is what registries that hand out
// public-read tokens expect. It only means this client has no user name or
// secret to offer when the exchange asks for one.
//
// A [WithDecoderMaxWindow] of zero is the other error. Zero is the value a
// caller reaches for to mean "no limit", and it is the one thing the option
// cannot express, so it is rejected here rather than silently reinstating the
// default or refusing every compressed file later.
func New(opts ...Option) (*Client, error) {
	settings := clientSettings{decoderMaxWindow: decomp.DefaultDecoderMaxWindow}
	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}
	if settings.decoderMaxWindow == 0 {
		return nil, errors.New("decoder max window must be positive, got 0")
	}

	if settings.credentials != nil {
		creds, err := settings.credentials()
		if err != nil {
			return nil, err
		}
		settings.resolved = creds
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
// Everything a transfer sends rides unencrypted under it, credentials and token
// exchanges included. It is for local registries only.
func WithPlainHTTP() Option {
	return func(s *clientSettings) {
		s.plainHTTP = true
	}
}

// WithDecoderMaxWindow caps the working set one decompressor may allocate at
// maxBytes, instead of the default 128 MiB
// ([decomp.DefaultDecoderMaxWindow]).
//
// One ceiling covers both codecs that have a working set: the zstd
// Window_Size a frame declares (or, for a single-segment frame, its
// Frame_Content_Size) and the LZMA2 dictionary capacity an xz stream
// declares. A stored file that needs more fails with [ErrDecode] before the
// buffer is allocated, on fetch and on the strict decode Publish performs
// over its own sources alike, so a producer cannot write a release this
// client would refuse to read back.
//
// The bound is per active decoder, not per transfer. Concurrent role
// transfers each hold their own working set, so peak decoder memory is
// maxBytes times the number of entries decoding at once — see
// [WithWorkers], which sets that number.
//
// The default is what mainstream producer output needs: it is the zstd CLI's
// own default decode limit (windowLog 27), and it covers the 64 MiB
// dictionary of `xz -9`. Lowering it rejects such files; raising it accepts
// more hostile ones. Zero is not a way to disable the bound and [New]
// rejects it.
func WithDecoderMaxWindow(maxBytes uint64) Option {
	return func(s *clientSettings) {
		s.decoderMaxWindow = maxBytes
	}
}

// WithDockerCredentials authenticates with the credentials `docker login`
// stores: the entries in the Docker configuration file, and whatever the
// credential helpers that file names print for a registry.
//
// It is opt-in, and the opt is the point. Reading a user's configuration is
// one thing; a configuration that names a credential helper makes a lookup
// into running someone else's program, and a library that did that because it
// was linked in would be a surprise with a security dimension. Naming this
// option is the consent for both.
//
// The file is $DOCKER_CONFIG/config.json where that variable is set, and
// .docker/config.json under the user's home otherwise. [New] reads it, so a
// file that cannot be parsed fails there rather than in the middle of a
// transfer, and a file that is not there is not a failure at all: that is a
// machine nobody has logged in on, and every registry resolves to the
// anonymous credential. Helpers are asked afresh at every lookup, but the file
// itself is read once, so a `docker login` run during a transfer does not
// reach it.
//
// This client only ever reads. No transfer writes a credential anywhere.
func WithDockerCredentials() Option {
	return func(s *clientSettings) {
		s.credentials = dockerCredentials
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
// The credential goes to whatever host the reference names, so the caller, who
// chose both the secret and the reference, is the one deciding who sees it.
// [WithDockerCredentials] is the other shape: it answers only for the hosts a
// login was stored under.
//
// Naming both options leaves the last one named in effect.
func WithCredentials(username, secret string) Option {
	return func(s *clientSettings) {
		s.credentials = func() (auth.Credentials, error) {
			return auth.NewStatic(auth.Credential{
				Username: username,
				Password: secret,
			}), nil
		}
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
// The set is the standard file-manifest type plus
// application/vnd.bigoci.file.v1.
func (c *Client) Capabilities() Capabilities {
	return Capabilities{types: []string{standardFileMediaType, bigociFileMediaType}}
}

// portsFor returns cached Manifests, Blobs, and Multipart ports for host and
// repository, constructing them on first use.
//
// ctx bounds Docker credential helper lookups during adapter construction.
func (c *Client) portsFor(ctx context.Context, host, repository string) (adapterPorts, error) {
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
	ports, err := factory(ctx, host, repository, c.settings)
	if err != nil {
		if errors.Is(err, auth.ErrAuth) {
			return adapterPorts{}, fmt.Errorf("%w: %w", ErrUnauthorized, err)
		}
		return adapterPorts{}, err
	}
	c.adapters[key] = ports

	return ports, nil
}

// defaultAdapter opens a registry adapter for one host and repository.
//
// ctx bounds Docker credential helper lookups while the multipart adapter
// resolves stored credentials.
func defaultAdapter(ctx context.Context, host, repository string, settings clientSettings) (adapterPorts, error) {
	client, err := registry.New(registry.Config{
		Host:                        host,
		Repository:                  repository,
		HTTPClient:                  settings.httpClient,
		PlainHTTP:                   settings.plainHTTP,
		Credentials:                 settings.resolved,
		UnverifiedExternalTransport: settings.allowUnverifiedExternal,
	})
	if err != nil {
		return adapterPorts{}, fmt.Errorf("open registry %s/%s: %w", host, repository, err)
	}

	mpCfg, err := multipartConfig(ctx, host, settings)
	if err != nil {
		return adapterPorts{}, fmt.Errorf("open multipart %s/%s: %w", host, repository, err)
	}
	mp, err := multipart.New(mpCfg)
	if err != nil {
		return adapterPorts{}, fmt.Errorf("open multipart %s/%s: %w", host, repository, err)
	}

	return adapterPorts{
		manifests: client.Manifests(),
		blobs:     client.Blobs(),
		multipart: mp,
	}, nil
}

// multipartConfig maps client settings onto [multipart.Config] the same way
// [defaultAdapter] maps them onto [registry.Config]: HTTP client, plain HTTP,
// and credentials. The unverified-external-transport option is never forwarded.
//
// ctx is the operation's caller context, so a cancelled or expired transfer
// interrupts a credential helper instead of waiting out the auth lookup cap.
func multipartConfig(ctx context.Context, host string, settings clientSettings) (multipart.Config, error) {
	cfg := multipart.Config{
		HTTPClient: settings.httpClient,
		PlainHTTP:  settings.plainHTTP,
	}
	if settings.resolved == nil {
		return cfg, nil
	}
	cred, err := settings.resolved.Credential(ctx, host)
	if err != nil {
		return multipart.Config{}, err
	}
	cfg.Username = cred.Username
	cfg.Secret = cred.Password
	return cfg, nil
}

// dockerCredentials builds the credential source [WithDockerCredentials]
// names: the Docker configuration file wherever this machine keeps it.
//
// A machine that cannot say where its configuration would be — no home
// directory and no $DOCKER_CONFIG, the shape of a scratch container — has no
// configuration, which is the same answer as a configuration file that does
// not exist: no source is installed and every registry resolves anonymously.
// The error [New] reports is reserved for a configuration that exists and
// cannot be read, because that is the one case where failing quietly would
// hide a credential the user meant to be used.
func dockerCredentials() (auth.Credentials, error) {
	path, err := auth.DefaultConfigPath()
	if err != nil {
		return nil, nil //nolint:nilnil,nilerr // no locatable configuration is the anonymous case, not a failure
	}

	return auth.NewStore(path)
}
