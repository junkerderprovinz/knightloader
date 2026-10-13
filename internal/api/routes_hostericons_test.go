package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// TestAnSVGIconCannotRunScriptWhenOpenedDirectly: an <img> never runs an SVG's
// script, but the icon URL opened in a tab would, on the instance's origin.
func TestAnSVGIconCannotRunScriptWhenOpenedDirectly(t *testing.T) {
	t.Parallel()
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`)
	rec := httptest.NewRecorder()
	serveHosterIcon(rec, httptest.NewRequest(http.MethodGet, "/api/hosters/icon?host=example.org", nil), svg, "image/svg+xml")

	h := rec.Header()
	csp := h.Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'none'", "sandbox"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("Content-Security-Policy %q lacks %s", csp, directive)
		}
	}
	if strings.Contains(csp, "script-src") {
		t.Errorf("Content-Security-Policy %q allows a script source", csp)
	}
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
	if rec.Body.String() != string(svg) {
		t.Errorf("body = %q, want the icon unchanged", rec.Body.String())
	}
}

// TestAHostWithoutAnIconAnswersWithoutAnErrorStatus: a browser logs every 4xx
// image as a failed load, and most hosts in a list have no icon.
func TestAHostWithoutAnIconAnswersWithoutAnErrorStatus(t *testing.T) {
	srv, _ := testServer(t)
	resp, err := http.Get(srv.URL + "/api/hosters/icon?host=nas.local")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

// TestAnIconStillBeingFetchedAnswersWithoutAnErrorStatus: the page asks again
// for a pending icon, and a browser would log every 5xx on the way as a failed
// load.
func TestAnIconStillBeingFetchedAnswersWithoutAnErrorStatus(t *testing.T) {
	t.Parallel()
	pending := func(string) ([]byte, string, error) { return nil, "", app.ErrIconPending }
	rec := httptest.NewRecorder()
	hosterIconHandler(pending)(rec, httptest.NewRequest(http.MethodGet, "/api/hosters/icon?host=example.org", nil))

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After header")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want none", rec.Body.String())
	}
}

// TestABatchOfIconsSortsEveryHostIntoOneAnswer: the picker asks for a screen
// of hosts at once and reads each host's fate from one answer.
func TestABatchOfIconsSortsEveryHostIntoOneAnswer(t *testing.T) {
	t.Parallel()
	lookup := func(host string) ([]byte, string, error) {
		switch host {
		case "small.example.org":
			return []byte("png"), "image/png", nil
		case "big.example.org":
			return make([]byte, iconInlineMax+1), "image/png", nil
		case "slow.example.org":
			return nil, "", app.ErrIconPending
		}
		return nil, "", errors.New("no icon")
	}
	rec := httptest.NewRecorder()
	target := "/api/hosters/icons?host=small.example.org&host=big.example.org&host=slow.example.org&host=dead.example.org&host=small.example.org"
	hosterIconsHandler(lookup)(rec, httptest.NewRequest(http.MethodGet, target, nil))

	var got iconBatch
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := "data:image/png;base64,cG5n"; got.Icons["small.example.org"] != want || len(got.Icons) != 1 {
		t.Errorf("icons = %v, want small.example.org as %s", got.Icons, want)
	}
	for name, part := range map[string][]string{"large": got.Large, "pending": got.Pending, "none": got.None} {
		if len(part) != 1 {
			t.Errorf("%s = %v, want one host", name, part)
		}
	}
	if got.RetryAfter <= 0 {
		t.Errorf("retryAfter = %d, want a pause", got.RetryAfter)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// TestABatchOfIconsStopsAtItsLimit: any host name starts a fetch, so one
// request may not start more than a screenful.
func TestABatchOfIconsStopsAtItsLimit(t *testing.T) {
	t.Parallel()
	asked := 0
	lookup := func(string) ([]byte, string, error) {
		asked++
		return nil, "", app.ErrIconPending
	}
	q := url.Values{}
	for i := range 3 * iconBatchMax {
		q.Add("host", fmt.Sprintf("h%d.example.org", i))
	}
	hosterIconsHandler(lookup)(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/hosters/icons?"+q.Encode(), nil))
	if asked != iconBatchMax {
		t.Errorf("looked up %d hosts, want %d", asked, iconBatchMax)
	}
}
