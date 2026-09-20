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
// produces has to be a zip a browser's "load unpacked", or a store's packer,
// can read, carrying the extension's manifest and the scripts it names.
//
// Nothing inside is substituted. The extension joins a group with the
// connection phrase and never learns an address, so the archive is
// byte-identical to a checkout and to what goes into a store.
func TestDownloadExtensionIsAValidZip(t *testing.T) {
	testDownloadExtension(t, "/api/browser-extension.zip", "application/zip", "knightloader-extension.zip")
}

// TestDownloadExtensionXpiForFirefox is the same archive under the .xpi route
// Firefox's install flow looks for: a different name and content-type on the
// identical bytes, not a second build.
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
	// files that worker imports to reach a group. A zip missing one of those
	// installs cleanly and then does nothing, which is worth catching here
	// rather than in a browser.
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

// TestDownloadExtensionRequiresASession checks the route through the real
// guard, where TestOnlyTheseRoutesAreOpen only checks the table. A zip with no
// secret in it is still routed like everything else once a password is set.
//
// Through Handler(a) rather than a bare mux from reg.attach: the session check
// is middleware api.go wraps around the mux, so a test that skipped Handler
// would pass whichever of reg.Add and reg.AddOpen registered the route.
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
