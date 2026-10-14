package httpx

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAClassGoesThroughItsProxyWhileOneIsSet(t *testing.T) {
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.String())
		io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "direct")
	}))
	defer direct.Close()

	c := New(Options{Class: ClassDebrid, Proxy: NoProxy})
	get := func() string {
		t.Helper()
		resp, err := c.Get(direct.URL + "/api")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if got := get(); got != "direct" {
		t.Fatalf("without a class proxy the answer was %q", got)
	}
	u, _ := url.Parse(proxy.URL)
	SetClassProxy(ClassDebrid, u)
	if got := get(); got != "proxied" || len(seen) != 1 {
		t.Errorf("with a class proxy the answer was %q, the proxy saw %v", got, seen)
	}
	SetClassProxy(ClassDebrid, nil)
	c.CloseIdleConnections()
	if got := get(); got != "direct" {
		t.Errorf("after the class proxy was taken away the answer was %q", got)
	}
}
