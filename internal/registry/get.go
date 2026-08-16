package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/retry"
	"github.com/imgoci/go/internal/transfer"
)

const (
	// maxManifestSize is the largest manifest body this adapter reads.
	// Registries commonly cap manifests at 4 MiB; a larger body means
	// something other than a manifest is answering.
	maxManifestSize = 4 << 20

	// serverErrorCeiling is the highest status in the 5xx class. A proxy
	// can answer with a number nobody named, so the class is a range.
	serverErrorCeiling = 599

	// headerAccept is the request header whose value Get sends exactly as
	// the caller supplied it.
	headerAccept = "Accept"

	// headerContentType is the response header parseContentType reads.
	headerContentType = "Content-Type"

	// headerRetryAfter is the response header a registry asks for a pause in.
	headerRetryAfter = "Retry-After"

	// headerDockerContentDigest is the registry-computed digest header.
	// Get ignores it (ARCHITECTURE.md §6.8): a digest a caller verifies
	// content against cannot come from the same response the content did.
	headerDockerContentDigest = "Docker-Content-Digest"
)

// Get fetches the manifest or index at ref, sending accept as the request
// Accept header, and returns the original bytes and the parameter-stripped
// Content-Type.
//
// ref is a tag or "sha256:…" digest within the bound repository. The
// Docker-Content-Digest header is ignored (ARCHITECTURE.md §6.8). Bytes are
// returned untouched: Get never re-encodes the body.
//
// 401 and 403 wrap [transfer.ErrUnauthorized]. 404 wraps
// [transfer.ErrNotFound]. 429 and 5xx are tagged transient and retried
// under [retry.Do] with the client's policy (zero value: [retry.Default]).
// A content-coding rejection and an [auth.ErrAuth] failure are terminal.
func (c *Client) Get(ctx context.Context, ref, accept string) ([]byte, string, error) {
	var (
		raw         []byte
		contentType string
	)
	err := retry.Do(ctx, c.retry, func(ctx context.Context) error {
		var err error
		raw, contentType, err = c.getOnce(ctx, ref, accept)

		return err
	})

	return raw, contentType, err
}

// getOnce performs one manifest GET without retrying.
func (c *Client) getOnce(ctx context.Context, ref, accept string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.manifestURL(ref), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set(headerAccept, accept)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", err
		}
		if ok, classified := classifyAdapterError(err); ok {
			return nil, "", classified
		}

		return nil, "", err
	}
	defer resp.Body.Close()

	// The header is read so a future reader of this file sees the ignore
	// is deliberate. The value is discarded: identity is the body bytes.
	_ = resp.Header.Get(headerDockerContentDigest)

	if err = classifyManifestStatus(resp); err != nil {
		return nil, "", err
	}

	body, err := readManifestBody(resp.Body)
	if err != nil {
		return nil, "", err
	}
	contentType, err := parseContentType(resp.Header.Get(headerContentType))
	if err != nil {
		return nil, "", err
	}

	return body, contentType, nil
}

// manifestURL is scheme://host/v2/<repository>/manifests/<ref>.
func (c *Client) manifestURL(ref string) string {
	return (&url.URL{
		Scheme: c.scheme,
		Host:   c.host,
		Path:   pathPrefixV2 + c.repository + pathManifests + ref,
	}).String()
}

// classifyManifestStatus maps a registry status onto a port error.
//
// 200 is success. 401/403 wrap [transfer.ErrUnauthorized]. 404 wraps
// [transfer.ErrNotFound]. 429 and 5xx are tagged [retry.Transient] with
// the parsed Retry-After floor. Every other status is terminal.
func classifyManifestStatus(resp *http.Response) error {
	status := resp.StatusCode
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusNotFound:
		return fmt.Errorf("manifest: %w", transfer.ErrNotFound)
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("manifest: %w", transfer.ErrUnauthorized)
	case status == http.StatusTooManyRequests ||
		(status >= http.StatusInternalServerError && status <= serverErrorCeiling):
		after := retry.ParseRetryAfter(resp.Header.Get(headerRetryAfter), time.Now())

		return retry.Transient(fmt.Errorf("manifest: registry returned status %d", status), after)
	default:
		return fmt.Errorf("manifest: registry returned status %d", status)
	}
}

// classifyAdapterError maps identity and auth failures onto port errors and
// tags dial/timeout/reset as transient. ok is false when the caller should
// keep classifying.
func classifyAdapterError(err error) (bool, error) {
	var coding *contentCodingError
	if errors.As(err, &coding) {
		return true, err
	}
	if errors.Is(err, auth.ErrAuth) {
		return true, fmt.Errorf("%w: %w", err, transfer.ErrUnauthorized)
	}
	if transientNetwork(err) {
		return true, retry.Transient(err, 0)
	}

	return false, nil
}

// transientNetwork reports whether err is a [url.Error] wrapping a net dial,
// timeout, or connection reset. Those are the transport failures repeating
// the request can fix.
func transientNetwork(err error) bool {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return false
	}
	if urlErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	if opErr.Timeout() {
		return true
	}
	if opErr.Op == "dial" {
		return true
	}
	var errno syscall.Errno
	if errors.As(opErr.Err, &errno) {
		return errno == syscall.ECONNRESET || errno == syscall.EPIPE
	}

	return false
}

// readManifestBody copies body up to [maxManifestSize]. A larger body is
// an error rather than an unbounded allocation.
func readManifestBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxManifestSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifestSize {
		return nil, errors.New("manifest exceeds 4 MiB")
	}

	return data, nil
}
