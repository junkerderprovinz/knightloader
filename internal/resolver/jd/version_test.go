package jd

// Client.Version surfaces JD's own revision number - the "JD container's own
// version" row asks for. The namespace and method ("jd"/"version", returning
// JDUtilities.getRevisionNumber()) are JDownloader's own, verified against
// org.jdownloader.api.jd.JDAPI / JDAPIImpl in JDownloader's open source, not
// guessed at.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientVersionReadsTheRevision drives Version against the exact envelope
// shape every other call in this file already assumes ({"data": ...}), on the
// path the "jd" namespace's version() method answers on.
func TestClientVersionReadsTheRevision(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":24471}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	v, err := c.Version()
	if err != nil {
		t.Fatal(err)
	}
	if v != 24471 {
		t.Errorf("Version() = %d, want 24471", v)
	}
	if gotPath != "/jd/version" {
		t.Errorf("called %q, want the jd namespace's version method at /jd/version", gotPath)
	}
}

// TestClientVersionSurfacesATransportError pins that an unreachable JD
// reports an error rather than a silent 0 that would read as "revision zero"
// instead of "could not ask".
func TestClientVersionSurfacesATransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.Version(); err == nil {
		t.Fatal("Version() with a failing JD returned no error")
	}
}
