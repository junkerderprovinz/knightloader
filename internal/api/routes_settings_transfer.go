package api

// Settings only: a file that carries settings.json and nothing else, and an
// import that merges the ticked keys into what is here. The full archive
// (routes_backup.go) moves a whole install and applies at the next start; this
// moves configuration and applies live through app.PatchSettings, with no
// restart. Neither half runs on a timer or at boot.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// secretsIncludeParam is the only value that puts stored passwords into the
// download; anything else, including a typo, leaves them out. A leaked export
// cannot be taken back, while a missing password after import is named in
// importResult.Incomplete and fixed in one field.
const secretsIncludeParam = "include"

func registerSettingsTransfer(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/settings/export",
		"download settings.json alone as a portable document; stored passwords only when asked for by ?secrets=include",
		func(w http.ResponseWriter, r *http.Request) {
			exportSettings(w, r, a)
		})

	reg.Add(http.MethodPost, "/api/settings/import",
		"take over the named keys from a settings document; everything not named is left exactly as stored",
		func(w http.ResponseWriter, r *http.Request) {
			importSettings(w, r, a)
		})
}

func exportSettings(w http.ResponseWriter, r *http.Request, a *app.App) {
	includeSecrets := r.URL.Query().Get("secrets") == secretsIncludeParam
	now := time.Now()
	doc, err := settings.Portable(a.Settings.Get(), includeSecrets, buildinfo.Version, buildinfo.Deployment, now)
	if err != nil {
		http.Error(w, "could not encode the settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Indented, so the file can be checked in an editor before it is sent
	// anywhere.
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		http.Error(w, "could not encode the settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// "settings", not "backup", so it is not mistaken for the archive
	// (knightloader-backup-<stamp>.zip) in the same downloads folder.
	filename := fmt.Sprintf("knightloader-settings-%s.json", now.UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write(body)
}

// importRequest is the document plus the keys ticked in the preview built
// from it.
type importRequest struct {
	Document settings.PortableDoc `json:"document"`
	Keys     []string             `json:"keys"`
}

// importResult is what happened, key by key.
type importResult struct {
	// Applied are the keys that reached the store.
	Applied []string `json:"applied"`
	// Skipped are requested keys this build refuses to take over: the identity
	// keys (settings.NeverPortable) and keys the document does not carry.
	Skipped []string `json:"skipped"`
	// Unknown are keys the document holds that this build's Settings does not
	// have. encoding/json would drop them silently, and PortableDoc.Migrated
	// rewrites only the keys an import names, so a key renamed since the old
	// document was written would otherwise vanish.
	Unknown []string `json:"unknown"`
	// Incomplete names applied keys whose password did not travel, as codes
	// (see settings.Secretless).
	Incomplete []string `json:"incomplete"`
	// RuleProblems is how many imported rules this build cannot compile;
	// sanitizeRules keeps them, so they would save cleanly and never fire.
	RuleProblems int `json:"ruleProblems"`
	// Settings is the whole document as it now stands, shaped like GET
	// /api/settings, so the browser can reseed its draft.
	Settings settingsResponse `json:"settings"`
}

func importSettings(w http.ResponseWriter, r *http.Request, a *app.App) {
	var req importRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := req.Document.Check(buildinfo.Version); err != nil {
		writeTransferError(w, err)
		return
	}

	// The identity keys are removed first, so neither the document nor the
	// selection can bring one in (see settings.NeverPortable).
	never := map[string]bool{}
	for _, k := range settings.NeverPortable() {
		never[k] = true
		delete(req.Document.Settings, k)
	}

	known := knownSettingsKeys()
	// Never nil, so none of the lists encodes as null.
	applied := []string{}
	skipped := []string{}
	unknown := []string{}
	patch := map[string]json.RawMessage{}
	seen := map[string]bool{}
	for _, k := range req.Keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		raw, inDoc := req.Document.Settings[k]
		switch {
		case never[k]:
			skipped = append(skipped, k)
		case !inDoc:
			skipped = append(skipped, k)
		case !known[k]:
			unknown = append(unknown, k)
		default:
			patch[k] = raw
			applied = append(applied, k)
		}
	}

	// Unknown keys are reported even when nobody ticked them, while the old box
	// may still be running.
	for k := range req.Document.Settings {
		if seen[k] || never[k] || known[k] {
			continue
		}
		unknown = append(unknown, k)
		seen[k] = true
	}

	// Sorted, since unknown is partly built from map iteration.
	sort.Strings(applied)
	sort.Strings(skipped)
	sort.Strings(unknown)

	// An empty patch writes nothing but still reports what it found.
	current := a.Settings.Get()
	if len(patch) > 0 {
		// A value an older build wrote is read the way a restart reads that
		// build's own settings file.
		patch = req.Document.Migrated(patch)
		// The same checks as PATCH /api/settings, against a preview of the
		// merge, so the import is no laxer door into the store.
		preview, err := settings.ApplyPatch(current, patch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := settings.CheckFolders(preview, patched(patch)); err != nil {
			writeValidationError(w, err)
			return
		}
		if err := validateRows(preview, patched(patch)); err != nil {
			writeValidationError(w, err)
			return
		}

		// A patch of the selected keys only. The PUT path would reset every
		// omitted key to its zero value rather than its default, and applying
		// the whole document would overwrite keys nobody ticked.
		// TestImportLeavesUnnamedSettingsExactlyAsStored guards both.
		next, err := a.PatchSettings(patch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		current = next
	}

	// Counted after the apply, so these are the rules that just arrived.
	problems := a.RuleProblems()

	writeJSON(w, importResult{
		Applied:      applied,
		Skipped:      skipped,
		Unknown:      unknown,
		Incomplete:   incompleteFor(req.Document, applied),
		RuleProblems: len(problems.Packagizer) + len(problems.LinkFilter),
		Settings:     settingsBody(a, current),
	})
}

// knownSettingsKeys is every top-level JSON key of this build's Settings. It
// comes from settingsKinds, the table GET /api/settings/defaults serves, rather
// than from marshalling, where omitempty would hide the empty keys. That table
// is keyed by dotted leaf path, so each path is cut at the first dot.
func knownSettingsKeys() map[string]bool {
	out := map[string]bool{}
	for path := range settingsKinds() {
		head, _, _ := strings.Cut(path, ".")
		out[head] = true
	}
	return out
}

// incompleteFor is the document's secretless list narrowed to the keys that
// were taken over. The part of each code before the first dot is the
// top-level key the secret lives under ("reconnect.password" is "reconnect").
func incompleteFor(d settings.PortableDoc, applied []string) []string {
	out := []string{}
	if len(applied) == 0 {
		return out
	}
	in := map[string]bool{}
	for _, k := range applied {
		in[k] = true
	}
	for _, code := range d.Secretless() {
		head, _, _ := strings.Cut(code, ".")
		if in[head] {
			out = append(out, code)
		}
	}
	return out
}

// writeTransferError refuses an import with the reason and, where there is
// one, a code the interface translates, like writeValidationError.
func writeTransferError(w http.ResponseWriter, err error) {
	out := map[string]any{"error": err.Error()}
	var ve *settings.PortableVersionError
	if errors.As(err, &ve) {
		out["code"] = "transfer.tooNew"
		out["params"] = map[string]any{"version": ve.DocVersion, "running": ve.RunningVersion}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(out)
}
