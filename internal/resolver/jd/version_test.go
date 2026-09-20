package jd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

// A failure must not read as revision zero.
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
