package api

// Backup and restore.
//
// The backup bundles the SQLite store and settings.json — which is also
// where the rule sets and the timetable live, see settings.Settings's own
// doc comment — into one archive; see internal/backup's own doc comment for
// why there is no separate rules.json or schedule.json to also carry, and
// for why a restore validates to exhaustion but does not apply anything
// until the next process start-up.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/backup"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

func registerBackup(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/system/backup",
		"download the store and settings as one archive",
		func(w http.ResponseWriter, r *http.Request) {
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

	// A consistent snapshot taken through the same connection every other
	// write to this database goes through (VACUUM INTO — see BackupTo's own
	// doc comment), never a raw copy of the live file, which could hand
	// back a torn database if a task settled mid-copy.
	if err := a.Store.BackupTo(tmpPath); err != nil {
		http.Error(w, "could not snapshot the database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Marshalled from live state, deliberately unredacted — a backup that
	// cannot put a router or proxy password back on restore is not a
	// backup. That is safe here in a way it would not be on GET
	// /api/settings: this route requires the same session every other route
	// does and is never served to a page on load, only to a deliberate
	// "download my backup" click.
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
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := backup.Build(w, manifest, settingsJSON, tmpPath); err != nil {
		// Too late for http.Error: the headers, and quite possibly some
		// bytes of the zip, are already on the wire. Logged loudly instead,
		// because the client just received a download that is not a
		// complete, restorable backup and nothing about its HTTP status
		// said so.
		log.Printf("backup: the archive did not finish writing to the response: %v", err)
	}
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

	manifest, err := backup.Stage(a.DataDir, data, buildinfo.Version)
	if err != nil {
		// Verbatim: every error Stage returns already names exactly which
		// check failed and why, which is the one thing a rejected restore
		// has to say to be worth anything — "invalid file" would send
		// somebody re-uploading the same broken archive.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// The validated bundle is staged either way; only whether THIS process
	// can also trigger the restart that applies it depends on RequestExit.
	// A container whose supervisor restarts it on its own schedule, or a
	// build with nothing wired here yet, still has a restore waiting for
	// its next boot — see backup.ApplyPending, called before this process's
	// own store or settings are ever opened.
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
