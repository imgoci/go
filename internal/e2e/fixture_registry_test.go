//go:build e2e

// Registry fixtures: container lifecycle, credentials, and the raw HTTP
// access the suite needs to seed and inspect artifacts a conforming producer
// would never write.

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"

	"github.com/imgoci/go/internal/index"
)

const (
	// e2eRegistryPort is the distribution-spec listen port both images expose.
	e2eRegistryPort = "5000/tcp"
	// e2eStartupTimeout is long enough for a first-time image pull.
	e2eStartupTimeout = 3 * time.Minute
	// e2eHTTPTimeout covers seeding a few-MiB blob over localhost.
	e2eHTTPTimeout = 2 * time.Minute
	// e2eRepo is the repository path every anonymous registry seed uses.
	e2eRepo = "e2e/release"
	// e2eTag is the release tag the production-representative fixture is
	// published at.
	e2eTag = "v1"
	// e2eUser and e2ePass are the htpasswd credentials the auth tests present.
	e2eUser = "e2e"
	e2ePass = "secret"
	// e2eWrongPass is a secret that is not in the htpasswd file.
	e2eWrongPass = "wrong-secret"
	// e2eDistribution is the CNCF Distribution image adversarial cases pin.
	e2eDistribution = "registry:2"
	// e2eBearerToken is the token the in-process realm issues.
	e2eBearerToken = "e2e-token"
)

// e2eRegistry is one testcontainers image the e2e suite runs against.
type e2eRegistry struct {
	// name is the subtest label.
	name string
	// image is the container image reference.
	image string
}

// e2eRegistries returns zot and CNCF Distribution.
func e2eRegistries() []e2eRegistry {
	return []e2eRegistry{
		{name: "zot", image: "ghcr.io/project-zot/zot:v2.1.20"},
		{name: "distribution", image: e2eDistribution},
	}
}

// startRegistry launches image and returns the host:port the test process
// should dial. The container is cleaned up with t.
func startRegistry(t *testing.T, image string) string {
	t.Helper()
	return startRegistryWith(t, image, nil, nil, http.StatusOK)
}

// startHtpasswdRegistry launches registry:2 with htpasswd basic auth.
func startHtpasswdRegistry(t *testing.T) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(e2ePass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	htpasswd := e2eUser + ":" + string(hash) + "\n"
	files := []testcontainers.ContainerFile{{
		Reader:            strings.NewReader(htpasswd),
		ContainerFilePath: "/auth/htpasswd",
		FileMode:          0o644,
	}}
	env := map[string]string{
		"REGISTRY_AUTH":                "htpasswd",
		"REGISTRY_AUTH_HTPASSWD_REALM": "RegistryRealm",
		"REGISTRY_AUTH_HTPASSWD_PATH":  "/auth/htpasswd",
	}
	return startRegistryWith(t, e2eDistribution, env, files, http.StatusUnauthorized)
}

// startRegistryWith launches image with optional env and files, waiting until
// GET /v2/ returns wantStatus.
func startRegistryWith(
	t *testing.T,
	image string,
	env map[string]string,
	files []testcontainers.ContainerFile,
	wantStatus int,
) string {
	t.Helper()
	ctx := t.Context()

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts(e2eRegistryPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/v2/").WithPort(e2eRegistryPort).
				WithStartupTimeout(e2eStartupTimeout).
				WithStatusCodeMatcher(func(status int) bool { return status == wantStatus }),
		),
	}
	if len(env) > 0 {
		opts = append(opts, testcontainers.WithEnv(env))
	}
	if len(files) > 0 {
		opts = append(opts, testcontainers.WithFiles(files...))
	}

	container, err := testcontainers.Run(ctx, image, opts...)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("starting %s: %v", image, err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, e2eRegistryPort)
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort(host, port.Port())
}

// e2eCreds are optional basic credentials for seed HTTP against a protected
// registry.
type e2eCreds struct {
	// user is the htpasswd user name. Empty means anonymous.
	user string
	// pass is the htpasswd secret.
	pass string
}

// seedHTTP is the client seed helpers share. It does not follow redirects:
// upload Location headers must be resolved against the original POST URL.
func seedHTTP() *http.Client {
	return &http.Client{
		Timeout: e2eHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// seedBlob uploads data as a monolithic POST+PUT blob. It is a raw primitive
// for adversarial fixtures a conforming [Client.Publish] cannot emit.
func seedBlob(t *testing.T, registry, repo string, dgst digest.Digest, data []byte, cred e2eCreds) {
	t.Helper()
	ctx := t.Context()
	client := seedHTTP()

	postURL := fmt.Sprintf("http://%s/v2/%s/blobs/uploads/", registry, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start upload: status %d", resp.StatusCode)
	}

	base, err := url.Parse(postURL)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("upload Location %q: %v", resp.Header.Get("Location"), err)
	}
	putURL := base.ResolveReference(loc)
	query := putURL.Query()
	query.Set("digest", dgst.String())
	putURL.RawQuery = query.Encode()

	req, err = http.NewRequestWithContext(ctx, http.MethodPut, putURL.String(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit upload: status %d", resp.StatusCode)
	}
}

// seedManifest PUTs a manifest or index at ref with the given Content-Type.
// It is a raw primitive for adversarial fixtures a conforming [Client.Publish]
// cannot emit.
func seedManifest(t *testing.T, registry, repo, ref, mediaType string, data []byte, cred e2eCreds) {
	t.Helper()
	ctx := t.Context()
	putURL := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.Header.Set("Content-Type", mediaType)
	req.ContentLength = int64(len(data))
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
	default:
		t.Fatalf("put manifest %s: status %d", ref, resp.StatusCode)
	}
}

// getIndexRaw GETs the index document at ref (tag or sha256:…) and returns
// the original response bytes. Accept is the release-index media type.
func getIndexRaw(t *testing.T, registry, repo, ref string, cred e2eCreds) []byte {
	t.Helper()
	getURL := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repo, ref)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, getURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.Header.Set("Accept", index.MediaTypeIndex)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get index %s: status %d body %s", ref, resp.StatusCode, body)
	}
	return body
}

// applyCreds attaches basic credentials when they are non-empty.
func applyCreds(req *http.Request, cred e2eCreds) {
	if cred.user != "" {
		req.SetBasicAuth(cred.user, cred.pass)
	}
}

// getManifestRaw GETs a manifest or index at ref with Accept and returns the
// original response bytes. [getIndexRaw] is the release-index convenience.
func getManifestRaw(t *testing.T, registry, repo, ref, accept string, cred e2eCreds) []byte {
	t.Helper()
	getURL := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repo, ref)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, getURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.Header.Set("Accept", accept)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get manifest %s: status %d body %s", ref, resp.StatusCode, body)
	}
	return body
}

// listTagsRaw GETs /v2/<repo>/tags/list and returns the decoded tag names in
// registry order. This is the raw distribution-spec endpoint, not a library
// helper: PushByDigest must not appear here.
func listTagsRaw(t *testing.T, registry, repo string, cred e2eCreds) []string {
	t.Helper()
	listURL := fmt.Sprintf("http://%s/v2/%s/tags/list", registry, repo)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, listURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tags/list %s: status %d body %s", repo, resp.StatusCode, body)
	}
	var parsed struct {
		Tags []string `json:"tags"`
	}
	if err = json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("tags/list JSON: %v body %s", err, body)
	}
	if parsed.Tags == nil {
		return []string{}
	}
	return parsed.Tags
}

// headBlob HEADs /v2/<repo>/blobs/<digest> and requires HTTP 200. Used to
// prove a referenced blob exists without downloading it.
func headBlob(t *testing.T, registry, repo string, dgst digest.Digest, cred e2eCreds) {
	t.Helper()
	headURL := fmt.Sprintf("http://%s/v2/%s/blobs/%s", registry, repo, dgst.String())
	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, headURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD blob %s: status %d", dgst, resp.StatusCode)
	}
}
