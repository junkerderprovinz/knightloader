package api

import (
	"net/http"
	"testing"
)

func TestNoOtherSiteCanFrameQuickAdd(t *testing.T) {
	servedUnder(t, "/kl")
	srv, _ := testServer(t)
	defer srv.Close()

	for _, path := range []string{"/quickadd?url=magnet%3A%3Fxt%3Durn%3Abtih%3Aabc", "/kl/quickadd", "/kl/quickadd/"} {
		resp, _ := fetch(t, http.MethodGet, srv.URL+path, nil, nil)
		if csp := resp.Header.Get("Content-Security-Policy"); resp.StatusCode != http.StatusOK || csp != "frame-ancestors 'self'" {
			t.Errorf("GET %s = %d with Content-Security-Policy %q, want frame-ancestors 'self'", path, resp.StatusCode, csp)
		}
		if xfo := resp.Header.Get("X-Frame-Options"); xfo != "SAMEORIGIN" {
			t.Errorf("GET %s carries X-Frame-Options %q, want SAMEORIGIN", path, xfo)
		}
	}

	// A dashboard that embeds the interface keeps working.
	resp, _ := fetch(t, http.MethodGet, srv.URL+"/kl/", nil, nil)
	if resp.Header.Get("Content-Security-Policy") != "" || resp.Header.Get("X-Frame-Options") != "" {
		t.Errorf("GET /kl/ refuses to be framed: %v", resp.Header)
	}
}
