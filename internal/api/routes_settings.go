package api

// Settings, the fixed choices the form offers, and the rule dry run.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/extract"
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
			// Refuse a folder we cannot write to instead of accepting it and
			// downloading somewhere else.
			if err := settings.Validate(s.DownloadDir); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Rows that carry their own validator are refused here with the reason,
			// rather than being dropped by sanitize on the way to disk. A connection or
			// a schedule window that vanishes on save is the same class of bug as a
			// download folder that silently reverts, except the user blames the proxy
			// for it weeks later.
			if err := validateRows(s); err != nil {
				writeValidationError(w, err)
				return
			}
			before := a.Settings.Get().InstanceName
			applied, err := a.ApplySettings(s)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Same reasoning as PATCH's own call below: the relay only learns
			// this instance's display name once, at connect time, so a
			// changed name has to reconnect the relay client to actually
			// reach any sibling.
			if applied.InstanceName != before {
				applyRelay(a)
				// The LAN announce carries the same name and goes just as
				// stale (internal/discovery.SetSelf) - one rename, both
				// announces, rather than fixing one and leaving the other to
				// be noticed later.
				discoveryRefresh()
			}
			writeJSON(w, settingsBody(a, applied))
		})
	// PATCH is PUT's answer to the trap PUT's own summary names: PUT decodes and
	// writes back the WHOLE document, so a browser that loaded the page before a
	// concurrent edit elsewhere posts its stale copy of EVERY OTHER field back
	// over that edit, silently. A patch body names only the fields it means to
	// change; anything it does not name is read fresh from what is actually
	// stored right now, under the same lock that then writes the merge back.
	// settings.Store.SetPartial's own comment has the exact mechanism. Two
	// clients patching two different sections at once, someone on the
	// Reconnect page, someone else flipping the speed limit, therefore both
	// survive, which two concurrent PUTs of the whole document cannot promise.
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
			// Validated against a PREVIEW of the merge, built the same way
			// SetPartial itself will build the real one, just outside its lock,
			// so a patch gets the identical two refusals PUT already gives a whole
			// document that fails them. This preview can go stale by the
			// microseconds before SetPartial's own authoritative merge; see
			// settings.ApplyPatch's own comment for why that is not a correctness
			// problem, only ever a value sanitize would have clamped anyway.
			preview, err := settings.ApplyPatch(a.Settings.Get(), patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := settings.Validate(preview.DownloadDir); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := validateRows(preview); err != nil {
				writeValidationError(w, err)
				return
			}
			applied, err := a.PatchSettings(patch)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// The relay only learns this instance's display name once, in the
			// hello frame a connection opens with (see protocol.go's own
			// comment: clients never send a later announce, only their
			// initial one) - so a name changed here would otherwise sit
			// stale on every sibling's Instances page until something else
			// happened to reconnect the relay client. Reconnecting on every
			// unrelated settings save would be wasteful; this only fires when
			// the patch actually touched the one field the relay announce is
			// built from.
			if _, ok := patch["instanceName"]; ok {
				applyRelay(a)
				discoveryRefresh()
			}
			writeJSON(w, settingsBody(a, applied))
		})
	// One place every dropdown in the settings form comes from. Hard-coding these
	// in the interface is how a package gains a value nobody can select, and how a
	// value the app cannot honour stays selectable after it has been withdrawn.
	reg.Add(http.MethodGet, "/api/options", "every fixed choice the settings form offers, taken from the packages that implement them",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, options())
		})
	// The dry run used to live here too, as POST /api/rules/test. It is gone:
	// POST /api/rules/preview in routes_rules.go answers the same question from
	// the same engine, and two doors to one room is how a client picks the worse
	// one without ever finding out.
	//
	// Worse, specifically. That route called Compile directly, which honours the
	// set's master switch — so a set that was switched off came back with no
	// problems and every link accepted, including a set holding a pattern the
	// engine cannot parse. A dry run that reports "nothing is wrong" about a rule
	// list that does not compile is the exact failure this whole subsystem exists
	// to prevent, and it had no bound on the sample count either.
}

// settingsResponse is the settings plus what the rule engine could not compile.
// The two travel together on purpose: a rule dropped for a broken regular
// expression that the form never mentions is a rule the user goes on believing
// in, and for a filter that means links they think are blocked and are not.
type settingsResponse struct {
	settings.Settings
	Problems app.RuleProblems `json:"problems"`
}

// settingsBody is the only shape the settings ever leave in. Redacted is not
// optional here: the moment a client is shown the router password or a proxy
// password, the merge machinery that puts them back on save is protecting a
// value the client already holds.
func settingsBody(a *app.App, s settings.Settings) settingsResponse {
	return settingsResponse{Settings: s.Redacted(), Problems: a.RuleProblems()}
}

// writeValidationError refuses a save with the reason, and with the typed
// version of the reason when the failing validator has one.
//
// The envelope exists because the sentence is English and the interface is
// translated into forty-two languages. Translating here is not the answer: the
// server would need the reader's language on a settings request, and the same
// message goes to the log, which would then be written in whatever the last
// browser preferred. So the code travels and the interface picks the words.
//
// Only reconnect speaks in codes today. A validator without one still sends its
// sentence, and the client shows that rather than nothing - being untranslated
// is a smaller failure than being silent.
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(out)
}

// validateRows refuses the rows that carry their own validator, naming the one
// that failed. Only sanitize would otherwise see them, and sanitize's job is to
// drop what it cannot use rather than to explain it.
func validateRows(s settings.Settings) error {
	for i, e := range s.Connections {
		if err := proxycfg.Validate(e); err != nil {
			return fmt.Errorf("connection %d: %w", i+1, err)
		}
	}
	// An unconfigured reconnect is the normal state of a fresh install, not a
	// reason to refuse the whole settings page. Only a half-filled one is.
	if s.Reconnect.Method != reconnect.MethodNone && s.Reconnect.Method != "" {
		if err := s.Reconnect.Validate(); err != nil {
			return err
		}
	}
	for i, e := range s.Schedule {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("schedule row %d: %w", i+1, err)
		}
	}
	return nil
}

// options is every fixed choice the settings form offers, taken from the
// packages that implement them so the two cannot drift.
func options() map[string]any {
	return map[string]any{
		"mirrorPolicies": dedupe.Policies(),
		// collide.Ask is deliberately withheld. It means "park the task until a
		// human answers", and there is no status for that and no way to answer, so
		// a task set to it would sit in the queue forever with nothing saying why.
		"collisionPolicies": policiesExcept(collide.Policies(), collide.Ask),
		// confirm.UseGlobal is withheld for the same reason collide.Ask is just
		// above, from the other direction: it means "defer to the instance's own
		// default", and offered as a choice for the instance's own default it
		// would defer to itself. Ask stays in the menu here - unlike collide.Ask,
		// it is a real, answerable state for THIS field when the confirm is
		// interactive (internal/confirm.Trigger.Interactive), and it only
		// degrades to a fixed fallback for the triggers that are not.
		"confirmPolicies": confirmPoliciesForAGlobalDefault(),
		// Archives get their own two lists rather than borrowing the one above.
		// An extraction can honour a different set from a download - it has
		// nobody to ask, and it decides per folder rather than per file - so the
		// package that implements archive collisions is the one that says which
		// words the archive page may offer.
		"archiveCollisions": extract.Collisions(),
		"archiveDisposals":  extract.Disposals(),
		// Served for the same reason as its two siblings above: the menu is built
		// from what this server implements, not from what the client was compiled
		// with. It was missing, and the symptom was not a broken menu but no menu
		// at all - resumeOnStart was honoured at boot with nothing anywhere to set
		// it, which is a setting only somebody editing settings.json can reach.
		"resumeModes": settings.ResumeModes(),
		// The folder "trash" actually means, so the help text can name it
		// instead of implying a recycle bin the container does not have. It
		// travels with the menu rather than being spelled out in 42 locale
		// files, where renaming it would mean 42 edits and 41 of them forgotten.
		"archiveTrashFolder": extract.TrashName,
		// The capability line on the archive page. Asked of the extractor rather
		// than typed into the interface, because a list of formats written down
		// on the far side of an HTTP boundary drifts the first time a reader is
		// added or retired, and the drift is invisible: the page goes on
		// promising a format the build no longer opens.
		"archiveFormats": extract.Formats(),
		"proxyKinds": []proxycfg.Kind{
			proxycfg.KindNone, proxycfg.KindDirect,
			proxycfg.KindHTTP, proxycfg.KindHTTPS,
			proxycfg.KindSOCKS4, proxycfg.KindSOCKS4A, proxycfg.KindSOCKS5,
		},
		// The resolver options page's two menus, from the package that reads
		// them - same reasoning as every other list here: a quality preset or
		// a subtitle mode this build cannot honour must never be selectable.
		"ytdlpQualities":     ytdlp.Qualities(),
		"ytdlpSubtitleModes": ytdlp.SubtitleModes(),
		// The rule vocabulary is NOT here. It used to be: three hand-written lists
		// of fields, operators and actions, next to the engine that defines all
		// three. GET /api/rules/grammar builds them from the engine instead, and
		// the copy had already drifted — it offered no filter action at all, and
		// the interface's own type for this response had stopped declaring one of
		// the three lists. A menu built from the stale copy offers an operator the
		// engine refuses, which saves cleanly and then never fires.
		"scheduleActions": []schedule.Action{schedule.ActionPause, schedule.ActionResume, schedule.ActionLimit},
		// The cleanup menu is generated from the classes the app implements, for
		// the same reason as everything else here: a menu entry the server does not
		// recognise is a button that answers 400.
		"cleanupClasses": app.CleanupClasses(),
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

// confirmPoliciesForAGlobalDefault is confirm.Policies() with UseGlobal
// dropped - a separate small filter rather than a second call to
// policiesExcept, which is typed for collide.Policy specifically and would
// need generifying (a signature change to a function this file already
// exports) to serve a second package's enum too.
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

// problemsOrEmpty keeps a JSON null out of the response: a client checking the
// length of the list should not have to check for null first.
func problemsOrEmpty(in []rules.Problem) []rules.Problem {
	if in == nil {
		return []rules.Problem{}
	}
	return in
}
