package jd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// decodeCallParams unpacks the query string call() builds (one escaped JSON
// value per '&'-joined part) into out. It uses Errorf because the fake JD
// servers call it from handler goroutines, where FailNow is not allowed.
func decodeCallParams(t *testing.T, rawQuery string, out any) {
	t.Helper()
	if rawQuery == "" {
		return
	}
	// Each part is escaped on its own, so an '&' inside a value arrives as %26.
	parts := strings.Split(rawQuery, "&")
	decoded := make([]string, 0, len(parts))
	for _, p := range parts {
		unescaped, err := url.QueryUnescape(p)
		if err != nil {
			t.Errorf("query part %q did not decode: %v", p, err)
			return
		}
		decoded = append(decoded, unescaped)
	}
	body := "[" + strings.Join(decoded, ",") + "]"
	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Errorf("request body %q did not parse: %v", body, err)
	}
}

// JD writes the decoded bytes straight to a file, so they must round-trip
// unchanged.
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
		t.Error("overwritePackagizerRules = false; without it a packagizer rule renames the package out from under the marker, which is one of the two anchors the crawl is found again by")
	}
}

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
