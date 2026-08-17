package adapters

import (
	"context"
	"fmt"

	"github.com/imgoci/go/internal/multipart"
	"github.com/imgoci/go/internal/registry"
)

// Open is the default Factory: it opens a registry adapter for one host and repository.
//
// ctx bounds Docker credential helper lookups while the multipart adapter
// resolves stored credentials.
func Open(ctx context.Context, host, repository string, cfg Config) (Ports, error) {
	client, err := registry.New(registry.Config{
		Host:                        host,
		Repository:                  repository,
		HTTPClient:                  cfg.HTTPClient,
		PlainHTTP:                   cfg.PlainHTTP,
		Credentials:                 cfg.Credentials,
		UnverifiedExternalTransport: cfg.UnverifiedExternalTransport,
	})
	if err != nil {
		return Ports{}, fmt.Errorf("open registry %s/%s: %w", host, repository, err)
	}

	mpCfg, err := multipartConfig(ctx, host, cfg)
	if err != nil {
		return Ports{}, fmt.Errorf("open multipart %s/%s: %w", host, repository, err)
	}
	mp, err := multipart.New(mpCfg)
	if err != nil {
		return Ports{}, fmt.Errorf("open multipart %s/%s: %w", host, repository, err)
	}

	return Ports{
		Manifests: client.Manifests(),
		Blobs:     client.Blobs(),
		Multipart: mp,
	}, nil
}

// multipartConfig maps [Config] onto [multipart.Config] the same way
// [Open] maps them onto [registry.Config]: HTTP client, plain HTTP,
// and credentials. The unverified-external-transport option is never forwarded.
//
// ctx is the operation's caller context, so a cancelled or expired transfer
// interrupts a credential helper instead of waiting out the auth lookup cap.
func multipartConfig(ctx context.Context, host string, cfg Config) (multipart.Config, error) {
	mpCfg := multipart.Config{
		HTTPClient: cfg.HTTPClient,
		PlainHTTP:  cfg.PlainHTTP,
	}
	if cfg.Credentials == nil {
		return mpCfg, nil
	}
	cred, err := cfg.Credentials.Credential(ctx, host)
	if err != nil {
		return multipart.Config{}, err
	}
	mpCfg.Username = cred.Username
	mpCfg.Secret = cred.Password
	return mpCfg, nil
}
