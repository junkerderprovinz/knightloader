package httpx_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/httpx/cloudflaretest"
)

func TestABrowserCheckIsAnErrorForAClientThatRefusesThem(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			cloudflaretest.Challenge(w, status)
		}))
		c := httpx.New(httpx.Options{RefuseBrowserChecks: true})
		resp, err := c.Post(srv.URL+"/fd/k3m9x2p7q1ab", "application/x-www-form-urlencoded", strings.NewReader("op=download1"))
		if err == nil {
			resp.Body.Close()
		}
		if !errors.Is(err, httpx.ErrBrowserCheck) {
			t.Errorf("status %d: err = %v, want ErrBrowserCheck", status, err)
		}
		srv.Close()
	}
}

// Cloudflare marks its challenge with a header, and the page alone still
// gives it away where a proxy on the way dropped the header.
func TestABrowserCheckIsKnownByItsPageWithoutTheHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, cloudflaretest.Page)
	}))
	defer srv.Close()

	_, err := httpx.New(httpx.Options{RefuseBrowserChecks: true}).Get(srv.URL)
	if !errors.Is(err, httpx.ErrBrowserCheck) {
		t.Errorf("err = %v, want ErrBrowserCheck", err)
	}
}

func TestAnOrdinaryRefusalReachesTheCallerWhole(t *testing.T) {
	const page = "<html><body>You have reached the download limit</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, page)
	}))
	defer srv.Close()

	resp, err := httpx.New(httpx.Options{RefuseBrowserChecks: true}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || string(body) != page {
		t.Errorf("got %d %q, want the 403 and its page", resp.StatusCode, body)
	}
}

func TestAClientThatDoesNotRefuseThemReadsTheChallengePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cloudflaretest.Challenge(w, http.StatusForbidden)
	}))
	defer srv.Close()

	resp, err := httpx.New(httpx.Options{}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want the 403", resp.StatusCode)
	}
}
