package jd

// Client.AddContainerData is the Deprecated API's inline-content path for a
// container that was never a fetchable file (Click'n'Load's addcrypted v1).
// Verified against JDownloader's own LinkCollectorAPIImplV2#addLinks: a
// dataURLs entry is "data:application/<ext>;base64,<content>", decoded to a
// temp file named by <ext> and fed into the same crawl entrance a URL would
// be. This file pins the wire shape, not JD's behaviour on the other end of
// it, which nothing in this tree can exercise without a live JD.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// decodeCallParams unpacks the query string call() builds — one
// url.QueryEscape(json.Marshal(param)) per '&'-joined part — back into a Go
// value, so a test can assert on the request JD actually received rather than
// re-deriving the encoding by hand. Shared with backend_test.go's fake JD.
//
// Errorf rather than Fatalf: backend_test.go's fake JD calls this from an
// httptest server's own handler goroutine, and FailNow (what Fatalf calls) is
// documented as unsafe off the test's own goroutine — it would end the
// handler mid-response instead of the test, leaving the client to hang or see
// a truncated body rather than the clear failure Errorf reports.
func decodeCallParams(t *testing.T, rawQuery string, out any) {
	t.Helper()
	unescaped, err := url.QueryUnescape(rawQuery)
	if err != nil {
		t.Errorf("query %q did not decode: %v", rawQuery, err)
		return
	}
	if err := json.Unmarshal([]byte("["+unescaped+"]"), out); err != nil {
		t.Errorf("request body %q did not parse: %v", unescaped, err)
	}
}

// TestAddContainerDataSendsInlineBase64 pins the exact request JD receives:
// the namespace/method, the base64 round-trip of the raw bytes (not of some
// re-encoding of them — JD writes this straight to a file and feeds it to its
// own DLC-shaped crawl, so a single stray transform here is a payload JD can
// no longer make sense of), and the marker/overwrite flags AddContainerLinks
// already relies on for identifying its own package afterwards.
func TestAddContainerDataSendsInlineBase64(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":{"id":42}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	raw := []byte("not-really-rsa-but-stands-in-for-it\x00\x01\x02")
	id, err := c.AddContainerData("dlc", raw, "KL-marker")
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	if gotPath != "/linkgrabberv2/addLinks" {
		t.Errorf("path = %q, want /linkgrabberv2/addLinks", gotPath)
	}

	var params []struct {
		DataURLs                 []string `json:"dataURLs"`
		PackageName              string   `json:"packageName"`
		Autostart                bool     `json:"autostart"`
		OverwritePackagizerRules bool     `json:"overwritePackagizerRules"`
	}
	decodeCallParams(t, gotQuery, &params)
	if len(params) != 1 || len(params[0].DataURLs) != 1 {
		t.Fatalf("params = %+v, want exactly one dataURLs entry", params)
	}
	got := params[0].DataURLs[0]
	wantPrefix := "data:application/dlc;base64,"
	if len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("dataURL = %q, want it to start with %q", got, wantPrefix)
	}
	decoded, err := base64.StdEncoding.DecodeString(got[len(wantPrefix):])
	if err != nil {
		t.Fatalf("dataURL base64 did not decode: %v", err)
	}
	if string(decoded) != string(raw) {
		t.Errorf("round-tripped bytes = %q, want the original %q", decoded, raw)
	}
	if params[0].PackageName != "KL-marker" {
		t.Errorf("packageName = %q, want KL-marker", params[0].PackageName)
	}
	if params[0].Autostart {
		t.Error("autostart = true; a container's links must land in the grabber, not start downloading unread")
	}
	if !params[0].OverwritePackagizerRules {
		t.Error("overwritePackagizerRules = false; without it JD names the package after whatever the container decrypts to, and the marker can no longer find it")
	}
}

// TestAddContainerDataSurfacesATransportError matches AddContainerLinks's own
// behaviour: a JD that cannot be reached must fail loudly here, not report an
// empty id that reads as "JD opened it and found nothing".
func TestAddContainerDataSurfacesATransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.AddContainerData("dlc", []byte("x"), "marker"); err == nil {
		t.Fatal("AddContainerData with a failing JD returned no error")
	}
}
