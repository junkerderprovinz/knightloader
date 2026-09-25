package api

// Settings, the fixed choices the form offers, and the rule dry run.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/mediahook"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

func registerSettings(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/settings", "the whole configuration, with every stored secret redacted",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, settingsBody(a, a.Settings.Get()))
		})
	reg.Add(http.MethodPut, "/api/settings", "replace the configuration; redacted secrets are merged back from the stored copy",
		func(w http.ResponseWriter, r *http.Request) {
			var s settings.Settings
			if !decodeJSON(w, r, &s) {
				return
			}
			// Refuse a folder we cannot use instead of accepting it and
			// downloading somewhere else.
			if err := settings.CheckFolders(s, nil); err != nil {
				writeValidationError(w, err)
				return
			}
			// Refused with the reason rather than silently dropped by sanitize.
			if err := validateRows(s, nil); err != nil {
				writeValidationError(w, err)
				return
			}
			before := a.Settings.Get().InstanceName
			beforeRelay := a.Settings.Get().RelayURL
			applied, err := a.ApplySettings(s)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The relay and the LAN announce only send the name when they
			// connect, so a new name or relay address needs a reconnect.
			if applied.InstanceName != before || applied.RelayURL != beforeRelay {
				applyRelay(a)
				reg.refreshDiscovery()
			}
			writeJSON(w, settingsBody(a, applied))
		})
	// PATCH merges only the named fields onto what is stored, under the store's
	// lock (settings.Store.SetPartial), so two clients editing different
	// sections at once both survive. A PUT of a stale page would overwrite the
	// other edit.
	reg.Add(http.MethodPatch, "/api/settings",
		"update only the named top-level fields; every field a caller did not name is left exactly as stored",
		func(w http.ResponseWriter, r *http.Request) {
			var patch map[string]json.RawMessage
			if !decodeJSON(w, r, &patch) {
				return
			}
			if len(patch) == 0 {
				http.Error(w, "the patch names no fields to change", http.StatusBadRequest)
				return
			}
			patch, err := canonicalKeys(patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Validated against a preview of the merge built outside the lock.
			// It can go stale before SetPartial's own merge; settings.ApplyPatch
			// says why that is harmless.
			preview, err := settings.ApplyPatch(a.Settings.Get(), patch)
			if err != nil {
				// A value of the wrong type is refused at its top-level key;
				// the decoder's path leaves out list positions.
				var te *json.UnmarshalTypeError
				if errors.As(err, &te) && te.Field != "" {
					key, _, _ := strings.Cut(te.Field, ".")
					writeValidationError(w, &settings.FieldError{Field: key, Err: err})
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Only the folders the patch names, so a stored folder that went
			// missing does not refuse an edit somewhere else.
			if err := settings.CheckFolders(preview, patched(patch)); err != nil {
				writeValidationError(w, err)
				return
			}
			if err := validateRows(preview, patched(patch)); err != nil {
				writeValidationError(w, err)
				return
			}
			applied, err := a.PatchSettings(patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The relay client is rebuilt only when the patch touches what it
			// is built from: the name it announces in its hello frame, and the
			// relay it dials.
			if _, ok := patch["instanceName"]; ok {
				applyRelay(a)
				reg.refreshDiscovery()
			} else if _, ok := patch["relayUrl"]; ok {
				applyRelay(a)
			}
			writeJSON(w, settingsBody(a, applied))
		})
	// Served from the packages that implement the choices, so the form cannot
	// offer a value the app does not honour.
	reg.Add(http.MethodGet, "/api/options", "every fixed choice the settings form offers, taken from the packages that implement them",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, options())
		})
}

// settingsResponse is the settings plus what the rule engine could not compile,
// so the form can show a rule that was dropped for a broken pattern.
type settingsResponse struct {
	settings.Settings
	Problems app.RuleProblems `json:"problems"`
}

// settingsBody is the only shape the settings leave in, always redacted.
func settingsBody(a *app.App, s settings.Settings) settingsResponse {
	return settingsResponse{Settings: s.Redacted(), Problems: a.RuleProblems()}
}

// writeValidationError refuses a save with the English reason and, when the
// validator has one, a code and parameters the interface translates. A
// validator without a code still sends its sentence.
func writeValidationError(w http.ResponseWriter, err error) {
	out := map[string]any{"error": err.Error()}
	var p *reconnect.ConfigProblem
	if errors.As(err, &p) {
		out["code"] = "reconnect." + p.Code
		params := map[string]any{}
		if p.N != 0 {
			params["n"] = p.N
		}
		if p.Method != "" {
			params["method"] = p.Method
		}
		if p.Var != "" {
			params["var"] = p.Var
		}
		if len(params) > 0 {
			out["params"] = params
		}
	}
	// Settings.tsx prefixes the code with "settings.", which makes this the
	// key the event target page already uses beside the row.
	var np *notify.Problem
	if errors.As(err, &np) {
		out["code"] = "eventTargets.problem." + np.Code
		params := map[string]any{}
		if np.N != 0 {
			params["n"] = np.N
		}
		if np.Header != "" {
			params["header"] = np.Header
		}
		if np.Value != "" {
			params["value"] = np.Value
		}
		if len(params) > 0 {
			out["params"] = params
		}
	}
	// A refusal that names its field is shown beside it, and the page saves the
	// rest of an edit without it. A top-level folder's is translated; a
	// category's folder keeps the sentence, which says which category it is.
	var pp *settings.PathProblem
	if errors.As(err, &pp) && pp.Field != "" {
		out["code"] = "pathProblem." + pp.Code
		out["params"] = map[string]any{"dir": pp.Dir}
		out["field"] = pp.Field
	}
	var fe *settings.FieldError
	if errors.As(err, &fe) {
		out["field"] = fe.Field
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(out)
}

// canonicalKeys spells every key of a patch the way the settings document
// does. encoding/json matches a field name whatever its case, so "downloaddir"
// would change the download folder while every check here, which looks a key
// up by its exact name, let it through. Two spellings of one key are refused,
// since only one of them could win. A key that names no field is left as it
// is, for the decode to drop.
func canonicalKeys(patch map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	known := knownSettingsKeys()
	out := make(map[string]json.RawMessage, len(patch))
	for key, value := range patch {
		name := key
		if !known[key] {
			for k := range known {
				if strings.EqualFold(k, key) {
					name = k
					break
				}
			}
		}
		if _, twice := out[name]; twice {
			return nil, fmt.Errorf("the patch names %s twice", name)
		}
		out[name] = value
	}
	return out, nil
}

// patched reports whether a patch names a top-level field.
func patched(patch map[string]json.RawMessage) func(key string) bool {
	return func(key string) bool {
		_, ok := patch[key]
		return ok
	}
}

// validateRows refuses the rows that carry their own validator, naming the one
// that failed and, as a settings.FieldError, where it is. Otherwise only
// sanitize would see them, and it drops what it cannot use without saying so.
//
// named is CheckFolders' filter: nil checks every row, and a patch checks only
// the lists it sends, so a stored row that went bad, such as a reconnect
// switched back on after its check URL was cleared, does not refuse a change
// of theme. A check across two lists runs when either is sent.
func validateRows(s settings.Settings, named func(key string) bool) error {
	sent := func(keys ...string) bool {
		if named == nil {
			return true
		}
		for _, k := range keys {
			if named(k) {
				return true
			}
		}
		return false
	}
	if sent("connections") {
		for i, e := range s.Connections {
			if err := proxycfg.Validate(e); err != nil {
				return &settings.FieldError{Field: fmt.Sprintf("connections.%d", i),
					Err: fmt.Errorf("connection %d: %w", i+1, err)}
			}
		}
	}
	// An unconfigured reconnect is normal on a fresh install; only a
	// half-filled one is refused.
	if sent("reconnect") && s.Reconnect.Method != reconnect.MethodNone && s.Reconnect.Method != "" {
		if err := s.Reconnect.Validate(); err != nil {
			field := "reconnect"
			var p *reconnect.ConfigProblem
			if errors.As(err, &p) {
				field += "." + p.Field()
			}
			return &settings.FieldError{Field: field, Err: err}
		}
	}
	if sent("schedule") {
		for i, e := range s.Schedule {
			if err := e.Validate(); err != nil {
				return &settings.FieldError{Field: fmt.Sprintf("schedule.%d", i),
					Err: fmt.Errorf("schedule row %d: %w", i+1, err)}
			}
		}
	}
	// A feed whose title filter does not compile must not be polled at all,
	// or it would stage the whole feed.
	if sent("feeds") {
		for i, e := range s.Feeds {
			if err := e.Validate(); err != nil {
				return &settings.FieldError{Field: fmt.Sprintf("feeds.%d", i),
					Err: fmt.Errorf("feed row %d: %w", i+1, err)}
			}
		}
	}
	// notify.Validate returns *Problem rather than error, so it is checked
	// before being wrapped; a typed nil in an error variable is not nil.
	if sent("eventTargets") {
		for i, e := range s.EventTargets {
			if p := notify.Validate(e); p != nil {
				return &settings.FieldError{Field: fmt.Sprintf("eventTargets.%d", i),
					Err: fmt.Errorf("event target %d: %w", i+1, p)}
			}
		}
	}
	if sent("categories", "packagizer") {
		if err := s.ValidateCategories(); err != nil {
			return err
		}
	}
	if sent("torrent") {
		if err := checkTorrentSettings(s.Torrent); err != nil {
			return err
		}
	}
	if sent("categories") {
		if err := checkCategoryFileRules(s.Categories); err != nil {
			return err
		}
	}
	if sent("mediaHooks", "categories") {
		if err := s.ValidateMediaHooks(); err != nil {
			return err
		}
	}
	// The only check here that touches the disk, so a stored category whose
	// share is offline does not refuse a change of theme either.
	if sent("categories") {
		for i, c := range s.Categories {
			if err := settings.Validate("the folder", c.Dir); err != nil {
				return &settings.FieldError{Field: fmt.Sprintf("categories.%d", i),
					Err: fmt.Errorf("category %d (%s): %w", i+1, categoryLabel(c, i), err)}
			}
		}
	}
	return nil
}

// categoryLabel names a category in a message: its name, else its id, else its
// position, which alone is little help on a page where rows are reordered.
func categoryLabel(c settings.Category, index int) string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	if id := strings.TrimSpace(c.ID); id != "" {
		return id
	}
	return fmt.Sprintf("row %d", index+1)
}

// options is every fixed choice the settings form offers, taken from the
// packages that implement them so the two cannot drift.
func options() map[string]any {
	return map[string]any{
		"mirrorPolicies": dedupe.Policies(),
		// collide.Ask would park a task with no status and no way to answer.
		"collisionPolicies": policiesExcept(collide.Policies(), collide.Ask),
		// confirm.UseGlobal would make the global default defer to itself.
		// Ask stays, since interactive triggers can answer it.
		"confirmPolicies": confirmPoliciesForAGlobalDefault(),
		// Extraction has its own lists: it has nobody to ask and decides per
		// folder rather than per file.
		"archiveCollisions": extract.Collisions(),
		"archiveDisposals":  extract.Disposals(),
		"resumeModes":       settings.ResumeModes(),
		// Strictest first, the order the control offers them in.
		"reclaimTrustModes":  settings.ReclaimTrustModes(),
		"mediaHookMethods":   mediahook.Methods(),
		"maxCategories":      settings.MaxCategories,
		"archiveTrashFolder": extract.TrashName,
		"archiveFormats":     extract.Formats(),
		"proxyKinds": []proxycfg.Kind{
			proxycfg.KindNone, proxycfg.KindDirect,
			proxycfg.KindHTTP, proxycfg.KindHTTPS,
			proxycfg.KindSOCKS4, proxycfg.KindSOCKS4A, proxycfg.KindSOCKS5,
		},
		"ytdlpQualities":     ytdlp.Qualities(),
		"ytdlpVideoFormats":  ytdlp.PresetVideoFormats(),
		"ytdlpAudioFormats":  ytdlp.AudioFormats(),
		"ytdlpAudioBitrates": ytdlp.AudioBitrates(),
		// The rule vocabulary comes from GET /api/rules/grammar.
		"scheduleActions": []schedule.Action{schedule.ActionPause, schedule.ActionResume, schedule.ActionLimit},
		"cleanupClasses":  app.CleanupClasses(),
	}
}

// policiesExcept drops a policy the app cannot honour from the menu, so it can
// never be chosen in the first place.
func policiesExcept(in []collide.Policy, drop collide.Policy) []collide.Policy {
	out := make([]collide.Policy, 0, len(in))
	for _, p := range in {
		if p != drop {
			out = append(out, p)
		}
	}
	return out
}

// confirmPoliciesForAGlobalDefault is confirm.Policies() without UseGlobal.
func confirmPoliciesForAGlobalDefault() []confirm.Policy {
	all := confirm.Policies()
	out := make([]confirm.Policy, 0, len(all))
	for _, p := range all {
		if p != confirm.UseGlobal {
			out = append(out, p)
		}
	}
	return out
}

// problemsOrEmpty keeps a JSON null out of the response.
func problemsOrEmpty(in []rules.Problem) []rules.Problem {
	if in == nil {
		return []rules.Problem{}
	}
	return in
}
