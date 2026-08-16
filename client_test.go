package imgoci

import (
	"net/http"
	"testing"
)

func TestNewOptionSealing(t *testing.T) {
	t.Parallel()

	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.settings.plainHTTP {
		t.Fatal("default scheme is https")
	}
	if c.settings.httpClient != nil {
		t.Fatal("default HTTP client is unset")
	}
	if c.settings.allowUnverifiedExternal {
		t.Fatal("default transport is verified")
	}
	if c.settings.credentials != nil {
		t.Fatal("default credentials are anonymous")
	}

	c, err = New(WithPlainHTTP())
	if err != nil {
		t.Fatal(err)
	}
	if !c.settings.plainHTTP {
		t.Fatal("WithPlainHTTP must set plain HTTP")
	}

	c, err = New(WithHTTPClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.settings.httpClient != nil {
		t.Fatal("nil HTTP client must be ignored")
	}

	hc := &http.Client{}
	c, err = New(WithHTTPClient(hc), WithUnverifiedExternalTransport(), WithCredentials("user", "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if c.settings.httpClient != hc {
		t.Fatal("WithHTTPClient must keep the given client")
	}
	if !c.settings.allowUnverifiedExternal {
		t.Fatal("WithUnverifiedExternalTransport must set the escape hatch")
	}
	if c.settings.credentials == nil {
		t.Fatal("WithCredentials must install a static source")
	}
}

func TestClientCapabilitiesAreStandard(t *testing.T) {
	t.Parallel()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Capabilities().supports(standardFileMediaType) {
		t.Fatal("standard file type must be supported")
	}
	if c.Capabilities().supports("application/vnd.bigoci.file.v1") {
		t.Fatal("BigOCI must not be assumed")
	}
}

func TestNewIgnoresNilOption(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err != nil {
		t.Fatal(err)
	}
}
