package api

// The move from JDownloader: read a cfg folder into a preview, then take over
// what was ticked. The folder comes as an uploaded zip or as a path on this
// machine, and is only ever read.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/jdimport"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// maxJDUpload caps an uploaded cfg zip. A cfg folder is a few megabytes; the
// download list of a large queue can add tens more.
const maxJDUpload = 256 << 20

func registerJDImport(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/jdimport/read",
		"read a JDownloader cfg folder, uploaded as a zip or named by its path here, into a preview; writes nothing",
		func(w http.ResponseWriter, r *http.Request) {
			readJDImport(w, r, a)
		})

	reg.Add(http.MethodPost, "/api/jdimport/apply",
		"take over the ticked items of a JDownloader preview",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Token string   `json:"token"`
				IDs   []string `json:"ids"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			rep, err := a.ApplyJDImport(body.Token, body.IDs, func(preview settings.Settings, patch map[string]json.RawMessage) error {
				if err := settings.CheckFolders(preview, patched(patch)); err != nil {
					return err
				}
				return validateRows(preview, patched(patch))
			})
			if errors.Is(err, app.ErrJDImportExpired) {
				writeRefusal(w, http.StatusGone, "jdimport.expired", err.Error(), nil)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, rep)
		})
}

func readJDImport(w http.ResponseWriter, r *http.Request, a *app.App) {
	var fsys fs.FS
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxJDUpload+1<<20)
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file in the upload: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "the upload could not be read: "+err.Error(), http.StatusBadRequest)
			return
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			writeRefusal(w, http.StatusBadRequest, "jdimport.notZip", "this is not a zip file: "+err.Error(), nil)
			return
		}
		fsys = zr
	} else {
		var body struct {
			Path string `json:"path"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		p := strings.TrimSpace(body.Path)
		st, err := os.Stat(p)
		if p == "" || err != nil {
			writeRefusal(w, http.StatusBadRequest, "jdimport.pathMissing",
				"there is no file or folder at that path on this machine", map[string]string{"path": p})
			return
		}
		if st.IsDir() {
			fsys = os.DirFS(p)
		} else {
			zr, err := zip.OpenReader(p)
			if err != nil {
				writeRefusal(w, http.StatusBadRequest, "jdimport.notZip", "this is not a zip file: "+err.Error(), nil)
				return
			}
			defer zr.Close()
			fsys = zr
		}
	}

	preview, err := a.ReadJDImport(fsys)
	if errors.Is(err, jdimport.ErrNoConfig) {
		writeRefusal(w, http.StatusBadRequest, "jdimport.noConfig",
			"no JDownloader settings were found there; point at the cfg folder or a zip of it", nil)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, preview)
}
