package api

// Settings only: one file that carries settings.json and nothing else, and an
// import that merges the parts somebody ticked into what is already here.
//
// The full archive beside it (routes_backup.go) is a different promise and the
// two must not be confused, which is why the download is named "settings"
// rather than "backup": that one moves an INSTALL - the database, the task
// history, this box's own identity - and it applies at the next start-up,
// wholesale, replacing whatever was here. This one moves a CONFIGURATION, takes
// only the keys the person selected, and applies live, because
// app.PatchSettings runs every runtime effect a saved settings page already
// runs (afterSettingsChange). No restart, and nothing the person did not tick
// changes at all.
//
// Two things this file deliberately does NOT do, both of them one small step
// away from this code, and both of them a default that would change behaviour
// for somebody who upgraded into it: the export never runs on a timer, and the
// import never applies on boot. Two buttons appear and they do nothing until
// pressed.

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

// secretsIncludeParam is the ONE spelling that opts a stored password into the
// download. Anything else - the parameter absent, "omit", a typo, a client that
// has not been taught about it - omits.
//
// That direction is the owner's decision (jdp, 2026-09-08) and it is written as
// a whitelist rather than as `!= "omit"` on purpose, so that the failure mode of
// every future caller is the safe one. The reasoning: the leak cannot be taken
// back. An export lands in a sync folder, a mail attachment or a chat, and the
// router password went with it. The cost of the other direction is a proxy that
// dials with no password after an import, which is visible, loud, named in the
// import's own answer (see importResult.Incomplete) and fixable in one field.
const secretsIncludeParam = "include"

func registerSettingsTransfer(reg *Registry, a *app.App) {
	// reg.Add, never reg.AddOpen. The download is the whole configuration and,
	// when asked for, every stored password in clear text; it sits behind the
	// same session as the backup route it stands beside. routes_test.go pins the
	// open-route list with a written justification per entry, and neither of
	// these has one to give.
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
	// Indented, because this file is one somebody opens in an editor to check
	// what is in it before mailing it to themselves - which is exactly the habit
	// the secrets toggle is trying to encourage.
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		http.Error(w, "could not encode the settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// The word "settings", never "backup". Both files land in the same downloads
	// folder and the archive's own name is knightloader-backup-<stamp>.zip; two
	// names that differ only by extension is how somebody uploads the wrong one
	// into the wrong dialog and is told, correctly and unhelpfully, that it is
	// not a valid archive.
	filename := fmt.Sprintf("knightloader-settings-%s.json", now.UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	// Written whole rather than streamed through an encoder: the body is already
	// in memory and a failure here has to be catchable before the headers commit
	// the response, which is the trap downloadBackup logs its way out of.
	_, _ = w.Write(body)
}

// importRequest is the document plus the selection. The two travel together
// because the selection is made against THAT document, in a preview the browser
// built from it - re-uploading the file and remembering the ticks separately
// would be two round trips that can disagree about what was on screen.
type importRequest struct {
	Document settings.PortableDoc `json:"document"`
	Keys     []string             `json:"keys"`
}

// importResult is what happened, key by key, because "ok" is not an answer here.
type importResult struct {
	// Applied are the keys that reached the store.
	Applied []string `json:"applied"`
	// Skipped are keys the caller asked for that this build refuses to take
	// over: the identity keys (settings.NeverPortable) and anything the document
	// does not actually carry.
	Skipped []string `json:"skipped"`
	// Unknown are keys the document holds that this build's Settings struct does
	// not have. Reported rather than dropped, because dropping them is exactly
	// what would otherwise happen in silence: settings.ApplyPatch merges as raw
	// JSON and re-unmarshals into Settings, and encoding/json discards an
	// unrecognised key without an error. Worse, migrate() runs only inside Load
	// against the raw bytes of settings.json and never against a patch body, so
	// a document old enough to carry a RENAMED key ("deleteArchive", which
	// became archiveDisposal) imports as nothing at all and the person's choice
	// is quietly lost.
	Unknown []string `json:"unknown"`
	// Incomplete names the applied keys whose password did not travel, as codes
	// (settings.SecretlessReconnect and friends). See settings.Secretless for the
	// two failures this exists to prevent, both of which save cleanly and fail
	// silently hours later.
	Incomplete []string `json:"incomplete"`
	// RuleProblems is how many imported rules this build cannot compile.
	// sanitizeRules changes nothing on purpose (settings_rules.go: a filter rule
	// that vanishes on save is a filter the user goes on believing in), so a rule
	// set that came over and does not compile saves cleanly and then never fires.
	// An import that answered only "ok" while a link filter silently passed
	// everything would be the exact failure that subsystem exists to prevent,
	// delivered through a new door.
	RuleProblems int `json:"ruleProblems"`
	// Settings is the whole document as it now stands, in the same shape GET
	// /api/settings sends, so the browser can reseed its draft rather than
	// guessing what the merge produced.
	Settings settingsResponse `json:"settings"`
}

func importSettings(w http.ResponseWriter, r *http.Request, a *app.App) {
	var req importRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := req.Document.Check(buildinfo.Version); err != nil {
		// Verbatim, and typed where it can be. Every refusal Check produces
		// already names which check failed and why, which is the one thing a
		// rejected import has to say to be worth anything - "invalid file" is how
		// somebody re-uploads the same broken document, the argument
		// uploadRestore already makes for the archive.
		writeTransferError(w, err)
		return
	}

	// The identity keys go before anything else looks at either side, so a
	// hand-edited document cannot smuggle one in and a client that names one
	// cannot have it honoured. See settings.NeverPortable for what happens to a
	// relay group when two boxes carry one instanceId.
	never := map[string]bool{}
	for _, k := range settings.NeverPortable() {
		never[k] = true
		delete(req.Document.Settings, k)
	}

	known := knownSettingsKeys()
	// Never nil: a client checking the length of one of these lists should not
	// have to check for null first, the same rule problemsOrEmpty already
	// follows.
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
			// Asked for and not in the file. A stale preview, or a client
			// sending the whole key list rather than the ticked half.
			skipped = append(skipped, k)
		case !known[k]:
			unknown = append(unknown, k)
		default:
			patch[k] = raw
			applied = append(applied, k)
		}
	}

	// Keys the document holds that this build has no field for are reported even
	// when nobody ticked them, because the preview that would have shown them is
	// built by a client this route cannot see - and an older document's lost key
	// is exactly the thing somebody needs to be told about while they still have
	// the old box running.
	for k := range req.Document.Settings {
		if seen[k] || never[k] || known[k] {
			continue
		}
		unknown = append(unknown, k)
		seen[k] = true
	}

	// Sorted so the three lists read the same way twice running. Map iteration
	// is the reason: `unknown` is partly built by ranging over the document, and
	// an answer whose list order changes between two identical requests is a
	// thing somebody eventually files a bug report about.
	sort.Strings(applied)
	sort.Strings(skipped)
	sort.Strings(unknown)

	// Validate, then write, in one block and against one read of the store.
	//
	// An empty patch skips both rather than saving the current document back
	// over itself: a request naming only unknown keys legitimately applies
	// nothing, and it still has to be able to report what it found.
	current := a.Settings.Get()
	if len(patch) > 0 {
		// Validated against a PREVIEW of the merge, built the same way SetPartial
		// will build the real one, just outside its lock - the identical three
		// checks PATCH /api/settings runs, reached through the identical
		// functions rather than restated here. A settings-only import that
		// validated less than the settings page does would be a second, laxer
		// door into the same store.
		preview, err := settings.ApplyPatch(current, patch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := settings.Validate("the download folder", preview.DownloadDir); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := settings.Validate("the working folder", preview.WorkDir); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateRows(preview); err != nil {
			writeValidationError(w, err)
			return
		}

		// THE PATCH, and never the document. This is the sharpest trap in the
		// feature and it is worth the paragraph.
		//
		// PUT /api/settings decodes into a fresh `var s settings.Settings`, so a
		// key the body omits becomes Go's ZERO value - not the default. Compare
		// settings.Load, which deliberately unmarshals over Defaults() "so new
		// fields keep their default value"; PUT does the opposite. Sending a
		// settings-only import down that path would switch extract, autoStart,
		// crawlSameHost, verifyChecksums and preParserEnabled OFF, set
		// maxConcurrent and maxPerHost to 0 and blank shape and navLabels - on a
		// box whose owner asked to take over a speed limit and nothing else.
		//
		// Handing SetPartial the whole `req.Document.Settings` instead of the
		// selected keys is the same failure by the shorter route, and the more
		// tempting one: it looks like "just apply the file". It smears every key
		// the file happens to carry over this box, ticked or not.
		//
		// So: a.PatchSettings, with a patch built from the selection alone, which
		// reads every key it does not name from what is stored right now under
		// the lock that then writes the merge back.
		// TestImportLeavesUnnamedSettingsExactlyAsStored fails, naming the field,
		// if anybody changes either half of that.
		next, err := a.PatchSettings(patch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		current = next
	}

	// Counted AFTER the apply, so this is the state of the rules that just
	// arrived rather than the ones they replaced.
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

// knownSettingsKeys is every TOP-LEVEL key this build's Settings struct has, as
// JSON spells them.
//
// Derived from settingsKinds() rather than from marshalling a Settings value,
// and the difference matters: `omitempty` drops exactly the keys that are empty,
// which on a fresh install is most of the interesting ones (connections, feeds,
// categories, the rule sets), so a marshalled document would call half of a
// perfectly good import "unknown". settingsKinds reflects over the struct
// instead, which is also the same table GET /api/settings/defaults serves to the
// browser - so the preview and this check cannot disagree about what exists.
//
// Cut at the first dot because that table is keyed by dotted path and stops at
// the leaves: a struct field like Reconnect appears only as "reconnect.method",
// "reconnect.username" and so on, never as "reconnect" itself, and a patch is
// addressed by top-level key.
func knownSettingsKeys() map[string]bool {
	out := map[string]bool{}
	for path := range settingsKinds() {
		head, _, _ := strings.Cut(path, ".")
		out[head] = true
	}
	return out
}

// incompleteFor is the document's own secretless list, narrowed to what was
// actually taken over. A missing router password on a key nobody imported is
// not this box's problem and saying so would train people to ignore the list.
//
// The intersection is on the head of each code up to the first dot, which
// settings' own constants guarantee is the top-level key the secret lives under
// ("reconnect.password" -> "reconnect", "archivePasswords" -> itself). One rule,
// no second table to fall out of step with the first.
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

// writeTransferError refuses an import with the reason, and with the typed
// version of the reason when there is one - the same envelope
// writeValidationError builds, for the same reason: the sentence is English and
// the interface is translated into forty-two languages, so the code travels and
// the interface picks the words. A refusal without a code still sends its
// sentence, because untranslated beats silent.
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
