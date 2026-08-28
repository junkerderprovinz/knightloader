package api

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func browserToolsServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerBrowserTools(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.URL[len("http://"):]
}

// TestDownloadExtensionIsAValidZip is the .zip route end to end: what a GET
// produces has to be a real zip a browser's "load unpacked" (or a store's own
// packer) can read, carrying the extension's manifest and the scripts that
// manifest names.
//
// It used to also assert that config.default.json inside the zip named the
// exact host that asked for it — the substitution that was once the only reason
// this download needed a server at all. The phrase rework removed both the file
// and the substitution: the extension joins a group with the connection phrase
// and never learns an address, so this archive is now byte-identical to a
// checkout and to what goes into a store.
func TestDownloadExtensionIsAValidZip(t *testing.T) {
	testDownloadExtension(t, "/api/browser-extension.zip", "application/zip", "knightloader-extension.zip")
}

// TestDownloadExtensionXpiForFirefox is the same archive under the .xpi
// route Firefox's own install flow looks for - a different name and
// content-type on the identical bytes, not a second build (jdp: "Bei JD
// offizieller Homepage kann man für Firefox z.B. eine xpi Datei
// runterladen" - the one-generic-zip-for-every-browser first pass here
// didn't cover that).
func TestDownloadExtensionXpiForFirefox(t *testing.T) {
	testDownloadExtension(t, "/api/browser-extension.xpi", "application/x-xpinstall", "knightloader-extension.xpi")
}

func testDownloadExtension(t *testing.T, path, wantContentType, wantFilename string) {
	t.Helper()
	srv, _ := browserToolsServer(t)

	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != wantContentType {
		t.Errorf("Content-Type = %q, want %s", ct, wantContentType)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, wantFilename) {
		t.Errorf("Content-Disposition = %q, missing the file name", cd)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(strings.NewReader(string(body)), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	// The manifest a browser parses first, the worker it names, and the four
	// files that worker imports to reach a group at all — a zip missing one of
	// those installs cleanly and then does nothing, which is the failure worth
	// catching here rather than in a browser.
	for _, name := range []string{"manifest.json", "background.js", "group.js", "relay.js", "phrase.js", "wordlist.js"} {
		if _, ok := files[name]; !ok {
			t.Errorf("zip is missing %s", name)
		}
	}
	if t.Failed() {
		return
	}

	manifestBytes := readZipFile(t, files["manifest.json"])
	var manifest struct {
		ManifestVersion int    `json:"manifest_version"`
		Name            string `json:"name"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	if manifest.ManifestVersion != 3 {
		t.Errorf("manifest_version = %d, want 3 (MV2 is being retired)", manifest.ManifestVersion)
	}
	if manifest.Name == "" {
		t.Error("manifest.json has no name")
	}

}

// TestDownloadExtensionRequiresASession folds into the package-wide
// TestOnlyTheseRoutesAreOpen (routes_test.go) already refusing to let this
// route slip onto the open list unnoticed; this is the direct check that the
// route itself, wired through the real guard, honours that — a zip with no
// secret in it is still routed the same way as everything else once a
// password is set, rather than carrying its own case. It goes through the
// real Handler(a), not a bare mux from reg.attach: the session check is
// middleware api.go wraps around the mux, not something reg.attach itself
// applies, and a test that skipped Handler would pass against an
// unauthenticated route regardless of what reg.Add vs reg.AddOpen registered
// it with.
func TestDownloadExtensionRequiresASession(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "at-least-8-chars"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/api/browser-extension.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func readZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("opening %s: %v", f.Name, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading %s: %v", f.Name, err)
	}
	return b
}
