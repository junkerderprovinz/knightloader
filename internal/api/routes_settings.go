package api

// Settings, the fixed choices the form offers, and the rule dry run.

import (
	"fmt"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
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
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			applied, err := a.ApplySettings(s)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
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
	// A dry run of a rule list against sample links. Without it the only way to
	// find out what a rule does is to paste real links and watch, which for a
	// filter means finding out by losing something.
	reg.Add(http.MethodPost, "/api/rules/test", "run a rule set against sample links without staging anything",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Set   rules.Set `json:"set"`
				Which string    `json:"which"` // "packagizer" or "filter"
				Links []struct {
					Filename string `json:"filename"`
					URL      string `json:"url"`
					Source   string `json:"source"`
					Filesize int64  `json:"filesize"`
					Package  string `json:"package"`
				} `json:"links"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			m, problems := rules.Compile(body.Set)
			type result struct {
				URL     string        `json:"url"`
				Effect  rules.Effect  `json:"effect"`
				Verdict rules.Verdict `json:"verdict"`
			}
			results := make([]result, 0, len(body.Links))
			now := time.Now()
			for _, l := range body.Links {
				c := rules.Candidate{
					Filename: l.Filename,
					URL:      l.URL,
					Source:   l.Source,
					Filesize: l.Filesize,
					Package:  l.Package,
					Added:    now,
				}
				// Both halves are reported whatever "which" says. They are one engine,
				// the answer to the other half costs nothing, and a set that rejects a
				// link while also naming a package for it is worth seeing whole.
				results = append(results, result{URL: l.URL, Effect: m.Apply(c), Verdict: m.Check(c)})
			}
			writeJSON(w, map[string]any{"problems": problemsOrEmpty(problems), "results": results})
		})
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
		"proxyKinds": []proxycfg.Kind{
			proxycfg.KindNone, proxycfg.KindDirect,
			proxycfg.KindHTTP, proxycfg.KindHTTPS,
			proxycfg.KindSOCKS4, proxycfg.KindSOCKS4A, proxycfg.KindSOCKS5,
		},
		"ruleFields": []rules.Field{
			rules.FieldFilename, rules.FieldURL, rules.FieldHoster, rules.FieldSource,
			rules.FieldFiletype, rules.FieldFilesize, rules.FieldPackage,
		},
		"ruleOps": []rules.Op{
			rules.OpContains, rules.OpEquals, rules.OpContainsNot,
			rules.OpEqualsNot, rules.OpMatches, rules.OpBetween,
		},
		// The Packagizer actions the app actually honours. "filename" is absent:
		// no backend accepts a destination file name, so a rename would only
		// desynchronise the list from the disk. Offering it would be a setting that
		// silently does nothing.
		"ruleActions":     []string{"packageName", "downloadDir", "comment", "priority", "autoExtract", "chunks"},
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

// problemsOrEmpty keeps a JSON null out of the response: a client checking the
// length of the list should not have to check for null first.
func problemsOrEmpty(in []rules.Problem) []rules.Problem {
	if in == nil {
		return []rules.Problem{}
	}
	return in
}
