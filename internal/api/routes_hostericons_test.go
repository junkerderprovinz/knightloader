package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
