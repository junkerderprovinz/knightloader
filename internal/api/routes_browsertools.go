package api

// The bookmarklet needs no route at all — it is generated client-side from
// window.location.origin (web/src/lib/browserTools.ts) and opens
// /quickadd, a page the SPA already serves. This file is the one piece of
// build-plan.md's 11D that a browser cannot build for itself: packaging the
// MV3 extension's source (package extension), with this specific instance's
// own address already filled in so installing it takes no configuration
// step for the common case of one browser, one instance. Two routes, not
// one: Chromium browsers (Chrome/Edge/Brave) load an unpacked .zip via
// Developer Mode, but Firefox's own install flow looks for a .xpi - the
// identical archive under a different name and content-type.

import (
	"archive/zip"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"github.com/junkerderprovinz/knightloader/extension"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// a goes unused below: unlike every other routes_*.go file, packaging a
// static, per-request zip touches no app state. It stays in the signature
// anyway, for the same registerX(reg, a) shape every subsystem in
// routes.go's registerAll uses — a reader should not have to learn a second
// calling convention for the one file that happens not to need it.
func registerBrowserTools(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/browser-extension.zip",
		"the Manifest V3 browser extension, packaged with this instance's own address pre-filled",
		func(w http.ResponseWriter, r *http.Request) {
			downloadExtension(w, r, "knightloader-extension.zip", "application/zip")
		})
	// Firefox's own install flow (about:addons drag-and-drop, or
	// about:debugging's "Load Temporary Add-on") looks for a .xpi, not a
	// bare .zip - jdp: "Bei JD offizieller Homepage kann man für Firefox
	// z.B. eine xpi Datei runterladen", after a first pass here only ever
	// offered one generic zip for every browser. An XPI IS a zip - Mozilla's
	// own format is exactly that, under a different extension and
	// content-type - so this is the identical archive, not a second build.
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

// extensionVersion reads manifest.json straight out of the same embedded
// tree downloadExtension packages, so this number can never drift from what
// actually ships in the zip/xpi - a hand-copied constant in the frontend
// would silently go stale the next time manifest.json's version is bumped.
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
// and content-type the caller's own browser-family route asked for.
//
// It used to substitute config.default.json on the way through, baking this
// instance's own address into the download so installing it took no
// configuration step. That is gone with the phrase rework: the extension no
// longer knows what an instance address is — it joins the group with the
// connection phrase and asks the relay who is in it (extension/src/group.js).
//
// The download is therefore byte-identical to what a `git clone` checkout
// contains, and to what goes into a browser store. One artefact, one thing to
// reason about, and no per-instance flavour that has to be explained to a
// store reviewer.
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
		// Too late for http.Error, the same reasoning as downloadBackup in
		// routes_backup.go: headers and possibly some zip bytes are already on
		// the wire, so the failure is logged rather than turned into a status
		// code nobody downstream will see.
		log.Printf("browsertools: %s did not finish writing to the response: %v", filename, walkErr)
	}
}
