package imgoci

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/imgoci/go/internal/adapters"
	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/decomp"
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
	// pool caches Manifests, Blobs, and Multipart ports per host+repository.
	pool *adapters.Pool
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
// A [WithDecoderMaxWindow] of zero is the other error. Zero does not disable
// the decoder bound, so it is rejected here rather than reinstating the
// default or failing every compressed file later.
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
		pool:     adapters.NewPool(nil),
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
// buffer is allocated, both on fetch and on the strict decode Publish
// performs over its own sources, so a producer cannot publish a release this
// client refuses to read back.
//
// The bound is per active decoder, not per transfer. Concurrent role
// transfers each hold their own working set, so peak decoder memory is
// maxBytes times the number of entries decoding at once, which
// [WithWorkers] sets.
//
// The default equals the zstd CLI's own decode limit (windowLog 27) and
// covers the 64 MiB dictionary of `xz -9`. A lower ceiling rejects such
// files; a higher one admits inputs that allocate correspondingly more per
// decoder. Zero is not a way to disable the bound, and [New] rejects it.
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
		s.credentials = auth.NewDockerCredentials
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
func (c *Client) portsFor(ctx context.Context, host, repository string) (adapters.Ports, error) {
	ports, err := c.pool.PortsFor(ctx, host, repository, adapterConfig(c.settings))
	if err != nil {
		if errors.Is(err, auth.ErrAuth) {
			return adapters.Ports{}, fmt.Errorf("%w: %w", ErrUnauthorized, err)
		}
		return adapters.Ports{}, err
	}

	return ports, nil
}

// adapterConfig maps client settings onto [adapters.Config]: HTTP client,
// plain HTTP, unverified-external-transport, and resolved credentials.
func adapterConfig(settings clientSettings) adapters.Config {
	return adapters.Config{
		HTTPClient:                  settings.httpClient,
		PlainHTTP:                   settings.plainHTTP,
		UnverifiedExternalTransport: settings.allowUnverifiedExternal,
		Credentials:                 settings.resolved,
	}
}
