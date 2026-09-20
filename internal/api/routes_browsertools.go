package api

// Packaging the MV3 extension's source (package extension) for download. The
// bookmarklet needs no route: it is generated client-side from
// window.location.origin (web/src/lib/browserTools.ts) and opens /quickadd,
// which the SPA already serves.
//
// Two routes rather than one because Chromium browsers load an unpacked .zip
// through Developer Mode while Firefox's install flow looks for a .xpi, which
// is the same archive under a different name and content-type.

import (
	"archive/zip"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"github.com/junkerderprovinz/knightloader/extension"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// registerBrowserTools takes an app it never reads: packaging a static zip
// touches no state, but the signature keeps the registerX(reg, a) shape
// routes.go's registerAll uses.
func registerBrowserTools(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/browser-extension.zip",
		"the Manifest V3 browser extension, packaged with this instance's own address pre-filled",
		func(w http.ResponseWriter, r *http.Request) {
			downloadExtension(w, r, "knightloader-extension.zip", "application/zip")
		})
	// Firefox's install flow (about:addons drag-and-drop, or
	// about:debugging's "Load Temporary Add-on") looks for a .xpi, which is a
	// zip under a different extension and content-type, so this serves the
	// same archive rather than a second build.
	reg.Add(http.MethodGet, "/api/browser-extension.xpi",
		"the identical browser extension, packaged as a .xpi for Firefox's own install flow",
		func(w http.ResponseWriter, r *http.Request) {
			downloadExtension(w, r, "knightloader-extension.xpi", "application/x-xpinstall")
		})
	reg.Add(http.MethodGet, "/api/browser-extension/version",
		"the extension's own manifest version, so the settings card can show it without hardcoding a second copy",
		func(w http.ResponseWriter, r *http.Request) {
			extensionVersion(w, r)
		})
}

// extensionVersion reads manifest.json out of the same embedded tree
// downloadExtension packages, so the number cannot drift from what ships in
// the archive the way a constant copied into the frontend would.
func extensionVersion(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(extension.Dist, "src/manifest.json")
	if err != nil {
		http.Error(w, "extension source is not embedded in this build", http.StatusInternalServerError)
		return
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		http.Error(w, "could not read the extension's manifest", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Version string `json:"version"`
	}{Version: manifest.Version})
}

// downloadExtension zips extension.Dist's src/ tree under whichever filename
// and content-type the caller's route asked for.
//
// Nothing is substituted on the way through: the extension joins the group
// with the connection phrase and asks the relay who is in it
// (extension/src/group.js), so it needs no instance address baked in. The
// download is byte-identical to a checkout and to what goes into a browser
// store.
func downloadExtension(w http.ResponseWriter, r *http.Request, filename, contentType string) {
	sub, err := fs.Sub(extension.Dist, "src")
	if err != nil {
		http.Error(w, "extension source is not embedded in this build", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	zw := zip.NewWriter(w)
	walkErr := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		fw, err := zw.Create(path)
		if err != nil {
			return err
		}
		_, err = fw.Write(content)
		return err
	})
	if walkErr == nil {
		walkErr = zw.Close()
	}
	if walkErr != nil {
		// Too late for http.Error, as in downloadBackup: headers and some zip
		// bytes are already on the wire.
		log.Printf("browsertools: %s did not finish writing to the response: %v", filename, walkErr)
	}
}
