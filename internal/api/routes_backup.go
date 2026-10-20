package api

// Backup and restore. The archive bundles the SQLite store and settings.json,
// which is also where the rule sets and the timetable live, and is sealed with
// the app key when the install has one. The automatic backups are the same
// archive, written into the data directory on a schedule. See internal/backup
// for why a restore validates everything up front but applies nothing until
// the next process start.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

func registerBackup(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/system/backup",
		"download the store and settings as one archive, every stored password included; refused while no login password is set",
		func(w http.ResponseWriter, r *http.Request) {
			if !passwordFirst(a, w, "downloading a backup, which carries every password in the settings") {
				return
			}
			downloadBackup(w, a)
		})

	reg.Add(http.MethodGet, "/api/system/backups",
		"the automatic backups in the data directory, when the next one is due and how the last run went",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.AutoBackupState())
		})

	reg.Add(http.MethodGet, "/api/system/backups/{name}",
		"download one automatic backup, every stored password included; refused while no login password is set",
		func(w http.ResponseWriter, r *http.Request) {
			if !passwordFirst(a, w, "downloading a backup, which carries every password in the settings") {
				return
			}
			downloadAutoBackup(w, r, a)
		})

	reg.Add(http.MethodPost, "/api/system/restore/check",
		"validate an uploaded backup without staging it, and say whether it comes from an instance in a pairing group and what becomes of the login",
		func(w http.ResponseWriter, r *http.Request) {
			data, ok := readUpload(w, r, a)
			if !ok {
				return
			}
			report, err := backup.Check(data, buildinfo.Version, a.AppKey, a.Auth.Enabled())
			if !refuseStage(w, err) {
				return
			}
			writeJSON(w, report)
		})

	reg.Add(http.MethodPost, "/api/system/restore",
		"validate an uploaded backup and stage it; a restart applies it",
		func(w http.ResponseWriter, r *http.Request) {
			uploadRestore(w, r, a)
		})
}

func downloadBackup(w http.ResponseWriter, a *app.App) {
	// Unredacted. Unlike GET /api/settings this is never served to a page on
	// load, only to a session that asked for the download.
	parts, err := a.PrepareBackup()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer parts.Discard()

	filename := fmt.Sprintf("knightloader-backup-%s.zip", time.Now().UTC().Format("20060102-150405"))
	if a.AppKey != nil {
		// Sealing needs the whole archive, so unlike the plain download a
		// failure can still be answered as one.
		var buf bytes.Buffer
		if err := a.WriteBackup(&buf, parts); err != nil {
			http.Error(w, "could not build the backup: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+backup.SealedExt+`"`)
		_, _ = w.Write(buf.Bytes())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := a.WriteBackup(w, parts); err != nil {
		// Too late for http.Error: the headers and part of the zip are
		// already on the wire, so the client has a truncated archive under
		// a 200 and only the log says otherwise.
		log.Printf("backup: the archive did not finish writing to the response: %v", err)
	}
}

// downloadAutoBackup sends one automatic backup from the backups folder. The
// name is checked against the names the schedule writes before it becomes a
// path, so nothing else in the data directory can be reached through it.
func downloadAutoBackup(w http.ResponseWriter, r *http.Request, a *app.App) {
	name := r.PathValue("name")
	if _, ok := backup.ParseAutoName(name); !ok {
		http.Error(w, "there is no automatic backup called "+name, http.StatusNotFound)
		return
	}
	f, err := os.Open(filepath.Join(a.AutoBackupDir(), name))
	if err != nil {
		http.Error(w, "there is no automatic backup called "+name, http.StatusNotFound)
		return
	}
	defer f.Close()
	contentType := "application/zip"
	if strings.HasSuffix(name, backup.SealedExt) {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	// Too late for http.Error once this starts, as with the log files.
	_, _ = io.Copy(w, f)
}

// readUpload reads and unseals the backup a restore route was sent, and
// answers the request itself when it cannot.
func readUpload(w http.ResponseWriter, r *http.Request, a *app.App) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxUploadBytes+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "send the backup as a multipart form field named \"file\"", http.StatusBadRequest)
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, backup.MaxUploadBytes+1))
	if err != nil {
		http.Error(w, "could not read the uploaded backup", http.StatusBadRequest)
		return nil, false
	}
	if len(data) > backup.MaxUploadBytes {
		http.Error(w, fmt.Sprintf("a backup over %d bytes is refused", backup.MaxUploadBytes), http.StatusRequestEntityTooLarge)
		return nil, false
	}

	// The app key of the instance that made the backup, for one sealed under
	// another key than this instance's.
	var uploadKey *appkey.Key
	if v := r.FormValue("appKey"); strings.TrimSpace(v) != "" {
		if uploadKey, err = appkey.Parse(v); err != nil {
			writeRefusal(w, http.StatusBadRequest, "restore.appKeyInvalid", err.Error(), nil)
			return nil, false
		}
	}
	data, err = backup.Unseal(data, a.AppKey, uploadKey)
	switch {
	case errors.Is(err, backup.ErrNeedsKey):
		writeRefusal(w, http.StatusBadRequest, "restore.appKeyMissing", err.Error(), nil)
		return nil, false
	case errors.Is(err, backup.ErrWrongKey):
		writeRefusal(w, http.StatusBadRequest, "restore.appKeyWrong", err.Error(), nil)
		return nil, false
	}
	return data, true
}

// refuseStage answers a bundle that failed validation and reports whether it
// passed.
func refuseStage(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrNewerSchema) {
		writeRefusal(w, http.StatusBadRequest, "restore.newerSchema", err.Error(), nil)
		return false
	}
	if err != nil {
		// Verbatim: every error validation returns names which check failed.
		// "invalid file" would send somebody re-uploading the same broken
		// archive.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func uploadRestore(w http.ResponseWriter, r *http.Request, a *app.App) {
	data, ok := readUpload(w, r, a)
	if !ok {
		return
	}
	manifest, err := backup.Stage(a.DataDir, data, buildinfo.Version, a.AppKey)
	if !refuseStage(w, err) {
		return
	}

	// The bundle is staged either way; RequestExit only decides whether this
	// process can also trigger the restart that applies it. A container
	// restarted on its supervisor's schedule still finds the staged restore
	// on its next boot, through backup.ApplyPending.
	restarting := a.RequestExit != nil && a.RequestExit(true)
	status := "validated and staged; restart the server to apply it"
	if restarting {
		status = "validated and staged; the server is restarting to apply it"
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]any{
		"manifest":   manifest,
		"restarting": restarting,
		"status":     status,
	})
}
