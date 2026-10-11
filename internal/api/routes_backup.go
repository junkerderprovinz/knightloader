package api

// Backup and restore. The archive bundles the SQLite store and settings.json,
// which is also where the rule sets and the timetable live, and is sealed with
// the app key when the install has one. See internal/backup for why a restore
// validates everything up front but applies nothing until the next process
// start.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
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

	reg.Add(http.MethodPost, "/api/system/restore",
		"validate an uploaded backup and stage it; a restart applies it",
		func(w http.ResponseWriter, r *http.Request) {
			uploadRestore(w, r, a)
		})
}

func downloadBackup(w http.ResponseWriter, a *app.App) {
	tmp, err := os.CreateTemp("", "kl-backup-*.db")
	if err != nil {
		http.Error(w, "could not prepare the backup", http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	// BackupTo refuses to write onto a path that already exists (see its own
	// doc comment), so the empty file CreateTemp just made has to go first.
	if err := os.Remove(tmpPath); err != nil {
		http.Error(w, "could not prepare the backup", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpPath)

	// VACUUM INTO through the connection every other write goes through, never
	// a raw copy of the live file, which hands back a torn database if a task
	// settles mid-copy.
	if err := a.Store.BackupTo(tmpPath); err != nil {
		http.Error(w, "could not snapshot the database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Unredacted, since a backup that cannot put a router or proxy password
	// back is not a backup. Unlike GET /api/settings this is never served to a
	// page on load, only to a session that asked for the download.
	settingsJSON, err := json.MarshalIndent(a.Settings.Get(), "", "  ")
	if err != nil {
		http.Error(w, "could not encode settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	manifest := backup.Manifest{
		Version:    buildinfo.Version,
		Deployment: buildinfo.Deployment,
		CreatedAt:  time.Now().UTC(),
	}

	filename := fmt.Sprintf("knightloader-backup-%s.zip", time.Now().UTC().Format("20060102-150405"))
	if a.AppKey != nil {
		downloadSealedBackup(w, a, filename, manifest, settingsJSON, tmpPath)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := backup.Build(w, manifest, settingsJSON, tmpPath); err != nil {
		// Too late for http.Error: the headers and part of the zip are
		// already on the wire, so the client has a truncated archive under
		// a 200 and only the log says otherwise.
		log.Printf("backup: the archive did not finish writing to the response: %v", err)
	}
}

// downloadSealedBackup builds the archive in memory and sends it sealed with
// the app key. Sealing needs the whole archive, so unlike the plain download
// a failure can still be answered as one.
func downloadSealedBackup(w http.ResponseWriter, a *app.App, filename string, manifest backup.Manifest, settingsJSON []byte, dbPath string) {
	var buf bytes.Buffer
	if err := backup.Build(&buf, manifest, settingsJSON, dbPath); err != nil {
		http.Error(w, "could not build the backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	sealed, err := backup.Seal(a.AppKey, buf.Bytes())
	if err != nil {
		http.Error(w, "could not encrypt the backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+backup.SealedExt+`"`)
	_, _ = w.Write(sealed)
}

func uploadRestore(w http.ResponseWriter, r *http.Request, a *app.App) {
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxUploadBytes+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "send the backup as a multipart form field named \"file\"", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, backup.MaxUploadBytes+1))
	if err != nil {
		http.Error(w, "could not read the uploaded backup", http.StatusBadRequest)
		return
	}
	if len(data) > backup.MaxUploadBytes {
		http.Error(w, fmt.Sprintf("a backup over %d bytes is refused", backup.MaxUploadBytes), http.StatusRequestEntityTooLarge)
		return
	}

	data, err = backup.Unseal(a.AppKey, data)
	switch {
	case errors.Is(err, backup.ErrNeedsKey):
		writeRefusal(w, http.StatusBadRequest, "restore.appKeyMissing", err.Error(), nil)
		return
	case errors.Is(err, backup.ErrWrongKey):
		writeRefusal(w, http.StatusBadRequest, "restore.appKeyWrong", err.Error(), nil)
		return
	}

	manifest, err := backup.Stage(a.DataDir, data, buildinfo.Version)
	if errors.Is(err, store.ErrNewerSchema) {
		writeRefusal(w, http.StatusBadRequest, "restore.newerSchema", err.Error(), nil)
		return
	}
	if err != nil {
		// Verbatim: every error Stage returns names which check failed.
		// "invalid file" would send somebody re-uploading the same
		// broken archive.
		http.Error(w, err.Error(), http.StatusBadRequest)
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
