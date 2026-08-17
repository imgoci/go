//go:build e2e

package e2e

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// gzipFilter reports whether a proxied response should be gzip-coded.
type gzipFilter func(*http.Request) bool

// gzipManifestRequest gzips distribution manifest GET/HEAD responses.
func gzipManifestRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}
	path := req.URL.EscapedPath()
	i := strings.LastIndex(path, "/manifests/")
	return i >= 0 && path[i+len("/manifests/"):] != ""
}

// gzipBlobRequest gzips stored-blob GET/HEAD responses, not upload sessions.
func gzipBlobRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}
	path := req.URL.EscapedPath()
	i := strings.LastIndex(path, "/blobs/")
	if i < 0 {
		return false
	}
	rest := path[i+len("/blobs/"):]
	return rest != "" && !strings.HasPrefix(rest, "uploads")
}

// startGzipProxy fronts backend with an [httputil.ReverseProxy] that gzips
// responses matching filter, then returns the proxy host:port.
func startGzipProxy(t *testing.T, backend string, filter gzipFilter) string {
	t.Helper()
	target, err := url.Parse("http://" + backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode != http.StatusOK {
			return nil
		}
		if filter == nil || !filter(resp.Request) {
			return nil
		}
		return gzipProxyResponse(resp)
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	return hostPortOf(t, server.URL)
}

// startGzipTokenRealm serves gzip-coded token JSON at URL/token.
func startGzipTokenRealm(t *testing.T, token string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, writeErr := zw.Write(body); writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
		if closeErr := zw.Close(); closeErr != nil {
			http.Error(w, closeErr.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(server.Close)
	return server.URL + "/token"
}

// startBearerProxy fronts backend and demands bearer token, issuing a
// challenge that names realmURL, then returns the proxy host:port.
func startBearerProxy(t *testing.T, backend, token, realmURL string) string {
	t.Helper()
	target, err := url.Parse("http://" + backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	want := "Bearer " + token
	challenge := `Bearer realm="` + realmURL + `",service="e2e",scope="repository:` + e2eRepo + `:pull"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return hostPortOf(t, server.URL)
}

// gzipProxyResponse replaces resp's body with a gzip member and sets
// Content-Encoding: gzip.
func gzipProxyResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return err
	}
	if err = resp.Body.Close(); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err = zw.Write(body); err != nil {
		return err
	}
	if err = zw.Close(); err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	resp.ContentLength = int64(buf.Len())
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Encoding", "gzip")
	resp.Header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	return nil
}

// hostPortOf returns the host:port of an http or https URL.
func hostPortOf(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host == "" {
		t.Fatalf("URL %q has no host", raw)
	}
	return parsed.Host
}

// startPassthroughProxy fronts backend with an identity reverse proxy.
func startPassthroughProxy(t *testing.T, backend string) string {
	t.Helper()
	return startGzipProxy(t, backend, func(*http.Request) bool { return false })
}

// startTruncatingBlobProxy fronts backend and bit-flips stored-blob GET bodies.
// Content-addressed registries refuse a digest-mismatched overwrite, so this is
// the black-box equivalent of corrupting a part after publish. Fetches are
// returned as 200/206 so a registry 307 cannot skip the corruption. Redirect
// Location hosts are rewritten to backend because zot may name an unreachable
// container address. Bodies are bit-flipped rather than shortened: a short read
// is retried via Range; a same-length digest mismatch is terminal. The empty
// config blob is left intact.
func startTruncatingBlobProxy(t *testing.T, backend string) string {
	t.Helper()
	target, err := url.Parse("http://" + backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	blobClient := &http.Client{
		Timeout: e2eHTTPTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			req.URL.Scheme = "http"
			req.URL.Host = backend
			return nil
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gzipBlobRequest(r) {
			proxy.ServeHTTP(w, r)
			return
		}
		u := *target
		u.Path = r.URL.Path
		u.RawPath = r.URL.RawPath
		u.RawQuery = r.URL.RawQuery
		req, reqErr := http.NewRequestWithContext(r.Context(), r.Method, u.String(), nil)
		if reqErr != nil {
			http.Error(w, reqErr.Error(), http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		resp, doErr := blobClient.Do(req)
		if doErr != nil {
			http.Error(w, doErr.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadGateway)
			return
		}
		switch resp.StatusCode {
		case http.StatusOK, http.StatusPartialContent:
			if len(body) >= 4 {
				body = bytes.Clone(body)
				body[0] ^= 0xff
			}
		}
		for key, values := range resp.Header {
			switch strings.ToLower(key) {
			case "content-length", "transfer-encoding", "docker-content-digest":
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(resp.StatusCode)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return hostPortOf(t, server.URL)
}

// startBlobRedirectFront fronts backend and 307s blob GET/HEAD to storage.
// Manifest and upload traffic still reverse-proxies to backend. Publish to
// backend, then Fetch through this front, so only reads are redirected.
func startBlobRedirectFront(t *testing.T, backend, storage string) string {
	t.Helper()
	target, err := url.Parse("http://" + backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gzipBlobRequest(r) {
			loc := "http://" + storage + r.URL.RequestURI()
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return hostPortOf(t, server.URL)
}

// startStorageProxy reverse-proxies backend and optionally gzip-codes blob
// GET/HEAD responses. It is the second in-process host a 307 Location names.
func startStorageProxy(t *testing.T, backend string, gzipBlobs bool) string {
	t.Helper()
	if gzipBlobs {
		return startGzipProxy(t, backend, gzipBlobRequest)
	}
	return startPassthroughProxy(t, backend)
}
