package registry

import (
	"bytes"
	"context"
	"net/http"

	"github.com/imgoci/go/internal/retry"
)

// Put publishes raw at ref with mediaType as the request Content-Type.
//
// ref is a tag or "sha256:…" digest within the bound repository. The body
// is the caller's bytes, never re-encoded. The request uses the same
// identity-wrapped manifest client as [Client.Get]; Accept-Encoding on the
// request is about the response body, not the bytes being stored.
//
// Success is 201 Created. 200 and 202 are also accepted: the e2e seeder
// observes zot and registry:2 answering 201 or 200, and 202 is OCI
// distribution-spec tolerance. 401 and 403 wrap
// [transfer.ErrUnauthorized]. 404 wraps [transfer.ErrNotFound]. 429 and
// 5xx are tagged transient and retried under [retry.Do] with the client's
// policy. Each attempt uses a fresh [bytes.NewReader] so the body is
// replayable.
func (c *Client) Put(ctx context.Context, ref, mediaType string, raw []byte) error {
	return retry.Do(ctx, c.retry, func(ctx context.Context) error {
		return c.putOnce(ctx, ref, mediaType, raw)
	})
}

// putOnce performs one manifest PUT without retrying.
func (c *Client) putOnce(ctx context.Context, ref, mediaType string, raw []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.manifestURL(ref), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set(headerContentType, mediaType)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		if ok, classified := classifyAdapterError(err); ok {
			return classified
		}

		return err
	}
	defer resp.Body.Close()

	return classifyPutStatus(resp)
}

// classifyPutStatus maps a registry PUT status onto a port error.
//
// 201, 200, and 202 are success. Other statuses reuse [classifyManifestStatus]
// so 401/403, 404, and 429/5xx match GET.
func classifyPutStatus(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusAccepted:
		return nil
	default:
		return classifyManifestStatus(resp)
	}
}
