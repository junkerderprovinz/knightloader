package api

// The bookmarklet needs no route at all — it is generated client-side from
// window.location.origin (web/src/lib/browserTools.ts) and opens
// /quickadd, a page the SPA already serves. This file is the one piece of
// build-plan.md's 11D that a browser cannot build for itself: packaging the
// MV3 extension's source (package extension) as a zip, with this specific
// instance's own address already filled in so installing it takes no
// configuration step for the common case of one browser, one instance.

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
			downloadExtension(w, r)
		})
}

// downloadExtension zips extension.Dist's src/ tree, substituting
// config.default.json's content along the way.
func downloadExtension(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(extension.Dist, "src")
	if err != nil {
		http.Error(w, "extension source is not embedded in this build", http.StatusInternalServerError)
		return
	}

	config, err := json.Marshal(struct {
		InstanceURL string `json:"instanceUrl"`
	}{InstanceURL: requestOrigin(r)})
	if err != nil {
		http.Error(w, "could not build the extension's default config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="knightloader-extension.zip"`)

	zw := zip.NewWriter(w)
	walkErr := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		if path == "config.default.json" {
			content = config
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
		log.Printf("browsertools: the extension zip did not finish writing to the response: %v", walkErr)
	}
}
