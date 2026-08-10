package api

// The module registry: one row per subsystem, with a verdict this build can
// stand behind, and a kill switch for the ones that have a real one.
//
// It is compiled in rather than derived from the settings file, because the
// question it answers is "what is in this binary", and a settings key can only
// ever answer "what did somebody configure". A subsystem nobody has configured
// and a subsystem that was never built look identical from settings.json, and
// the second one needs a reason printed next to it.
//
// Three consumers read this table — the modules page, the settings rail's list
// of sub-pages, and the self-describing index — so it lives here once. Three
// copies of "which subsystems exist" drift within a wave or two, and the drift
// is silent: the page keeps offering a switch for something the build dropped.
//
// The rule the rest of this file is built around: Enabled is DERIVED from live
// state on every request and never read back from a stored flag. A stored flag
// is how a switch and the thing it switches end up disagreeing, and that
// disagreement is invisible — the page says folder watch is off while the
// watcher goes on adding links.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// FeatureVerdict is what this build can honestly say about a subsystem. Three
// answers, because "not there" and "not there in this build" are different
// facts and a user who reads the second one as the first files a bug.
type FeatureVerdict string

const (
	// VerdictShipped: the code is in this binary and reachable from the server.
	VerdictShipped FeatureVerdict = "shipped"
	// VerdictDesktop: built, but only reachable in the desktop bundle. The
	// container has no desktop session to put it in.
	VerdictDesktop FeatureVerdict = "desktop"
	// VerdictNotBuilt: absent, and Reason says why. Never an empty section
	// labelled "not installed", which reads as a broken page rather than a
	// decision — Go has no portable dynamic plugin loading, so the set of
	// modules is fixed when the binary is built and the page says so.
	VerdictNotBuilt FeatureVerdict = "not-built"
)

// FeatureSwitch is how a module is switched, and — the part that matters — a
// declaration of whether it can be switched from here at all.
type FeatureSwitch string

const (
	// SwitchNone: there is no switch. Reason says why, and the interface renders
	// the control disabled carrying that reason. A switch that stores a boolean
	// nothing reads is worse than no switch, because it looks like it worked.
	SwitchNone FeatureSwitch = "none"

	// SwitchSetting: a boolean the subsystem re-reads every time it is about to
	// act, so clearing it stops the next action. Nothing already in flight is
	// killed, and none of these subsystems holds a goroutine open between
	// actions, so there is nothing left running to leak.
	SwitchSetting FeatureSwitch = "setting"

	// SwitchParked: the subsystem is configured by a value rather than by a flag,
	// so "off" means clearing that value — which genuinely tears it down, because
	// the app applies the cleared value the same way it applies any other save.
	// The old value is parked so switching back on restores it instead of handing
	// the user an empty field and a shrug.
	SwitchParked FeatureSwitch = "parked"
)

// Feature is one subsystem as this build has it.
type Feature struct {
	// ID is stable and is what the interface looks a label up by. The label is
	// deliberately not here: a server string cannot be translated by the browser,
	// and this instance does not know which of the 42 locales is looking at it.
	ID string `json:"id"`

	Verdict FeatureVerdict `json:"verdict"`

	// Page is the settings sub-page this module is configured on, empty when it
	// has none. It is what lets a page with nothing shipped behind it explain
	// itself out of this table rather than inventing its own excuse.
	Page string `json:"page"`

	// Enabled is computed from live state on every request. See the file comment.
	Enabled bool `json:"enabled"`

	Switch FeatureSwitch `json:"switch"`

	// Parked is whether a SwitchParked module has a value waiting to come back.
	//
	// It is the difference between "somebody switched this off" and "this was
	// never set up", and without it the two are the same row. That collapse is a
	// deadlock on a fresh install: the page that configures the module disables
	// its field because the module reads off, and the switch refuses to turn on
	// because nothing is configured, so there is no way in from either end.
	Parked bool `json:"parked"`

	// Reason is why the verdict is what it is, or why there is no switch. It is
	// English prose from the server for the same reason a Go error is: it is a
	// fact about this build, and inventing a translation key per build fact means
	// 42 files change every time a subsystem lands.
	Reason string `json:"reason,omitempty"`

	// Detail is one line of live state — the folder being watched, the port, how
	// many rules there are — so the row says something even when the switch does
	// not apply.
	Detail string `json:"detail,omitempty"`
}

// FeaturePage is one settings sub-page as registered. Every page is listed even
// when it is empty: a later wave then fills a page that already exists, with a
// route people may already have bookmarked, instead of inventing one and
// deciding its name and place all over again.
type FeaturePage struct {
	ID string `json:"id"`
	// Modules are the module ids configured on this page, so the page can render
	// the registry's reason for what is missing instead of writing its own.
	Modules []string `json:"modules"`
}

// Deliberately no "is this page built yet" flag. Whether a page has controls is
// a fact only the interface can know — it is whether a component exists — and a
// server-side copy of it is a copy that goes stale the first time a wave ships
// the page and forgets the flag, silently, with the rail then greying out a page
// full of controls. This table owns the SET and the ORDER of the pages; the
// interface owns which of them it can draw.

// FeatureState is the whole registry as one document, because the modules page
// and the rail both need all of it and two requests would let them disagree for
// as long as the second one is in flight.
type FeatureState struct {
	Modules []Feature     `json:"modules"`
	Pages   []FeaturePage `json:"pages"`
}

// parkBucket is where a kill switch keeps the value it cleared.
//
// The interface-state store is reused rather than a settings field being added,
// because the value parked here is not configuration — nothing reads it but the
// switch that wrote it — and because a settings field is a schema change in a
// file another lane owns. It gets a bucket of its own so the browser, which
// writes its layout bucket whole, cannot overwrite it.
const parkBucket = "features"

// errNoSwitch is a module the caller tried to switch that has no switch. It is
// a 400 rather than a silent no-op: the interface already knows from the table
// which rows are switchable, so a request for one that is not is a client bug
// and hiding it makes the client bug permanent.
var errNoSwitch = errors.New("this module has no switch here")

func registerFeatures(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/features", "every subsystem this build contains, with a verdict and its live on/off state",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, featureState(a))
		})

	reg.Add(http.MethodPut, "/api/features/{id}", "switch one subsystem on or off; refused with a reason where there is no real switch",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Enabled bool `json:"enabled"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if err := setFeature(a, r.PathValue("id"), body.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The whole table comes back rather than the one row: switching folder
			// watch off changes what the Downloads page may offer, and a client that
			// patched one row locally would keep showing the stale rest.
			writeJSON(w, featureState(a))
		})

	// What the advanced key table needs that GET /api/settings cannot tell it.
	reg.Add(http.MethodGet, "/api/settings/defaults", "the factory settings and the type of every settings key, for the advanced table's per-row reset",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, settingsSchema{
				// Redacted for the same reason the live settings are: this is served
				// to a browser, and a default that is a secret today is a secret
				// tomorrow.
				Values: settings.Defaults().Redacted(),
				Kinds:  settingsKinds(),
			})
		})
}

// settingsSchema is what the advanced table is built from.
type settingsSchema struct {
	Values settings.Settings `json:"values"`
	// Kinds is the type of every settings key, by the same dotted path the table
	// flattens the document into.
	Kinds map[string]string `json:"kinds"`
}

// settingsKinds reads the type of every settings field off the struct.
//
// The table cannot work this out from the values alone, and the failure is not
// cosmetic: Go writes an empty []string as JSON null, so `archivePasswords`
// arrives indistinguishable from an unset string. A row that guessed "text"
// there would hand the user a text box, and the string they typed into it would
// be refused by the decoder on save — an edit that cannot work, offered as if it
// could.
//
// It also surfaces the keys `omitempty` drops on the way out: an empty
// connection list is a key the user should be able to see and fill, not a key
// that appears only once somebody has already used another page to create it.
func settingsKinds() map[string]string {
	out := map[string]string{}
	collectKinds(reflect.TypeOf(settings.Settings{}), "", out)
	return out
}

func collectKinds(t reflect.Type, prefix string, out map[string]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Bool:
			out[path] = "boolean"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			out[path] = "number"
		case reflect.String:
			out[path] = "text"
		case reflect.Slice, reflect.Array, reflect.Map:
			// Stopped at, not walked into, and the table agrees: a rule list exploded
			// into one row per condition is a hundred rows whose ORDER is load-bearing
			// and which nobody can safely edit one at a time.
			out[path] = "list"
		case reflect.Struct:
			collectKinds(ft, path, out)
		default:
			out[path] = "text"
		}
	}
}

// featureState builds the table from live state.
func featureState(a *app.App) FeatureState {
	return FeatureState{Modules: featureList(a), Pages: featurePages()}
}

func featureList(a *app.App) []Feature {
	s := a.Settings.Get()
	parked := parkedIDs(a)
	return []Feature{
		{
			ID: "extraction", Verdict: VerdictShipped, Page: "archives",
			Switch: SwitchSetting, Enabled: s.Extract,
			Detail: extractionDetail(s),
		},
		{
			ID: "watch", Verdict: VerdictShipped, Page: "downloads",
			Switch: SwitchParked, Enabled: strings.TrimSpace(s.WatchDir) != "",
			Parked: parked["watch"], Detail: watchDetail(s),
		},
		{
			ID: "crawler", Verdict: VerdictShipped, Page: "downloads",
			Switch: SwitchSetting, Enabled: s.Crawl,
		},
		{
			ID: "checksums", Verdict: VerdictShipped, Page: "downloads",
			Switch: SwitchSetting, Enabled: s.VerifyChecksums,
		},
		{
			ID: "scheduler", Verdict: VerdictShipped, Page: "schedule",
			Switch: SwitchParked, Enabled: len(s.Schedule) > 0,
			Parked: parked["scheduler"], Detail: countDetail(len(s.Schedule), "window", "windows"),
		},
		{
			ID: "reconnect", Verdict: VerdictShipped, Page: "reconnect",
			Switch: SwitchParked, Enabled: a.ReconnectState().Configured,
			Parked: parked["reconnect"], Detail: reconnectDetail(s),
		},
		{
			ID: "packagizer", Verdict: VerdictShipped, Page: "rules",
			Switch: SwitchNone, Enabled: len(s.Packagizer.Rules) > 0,
			// Spelled out rather than left blank: the obvious switch here would be
			// an Enabled flag on the rule set, and an Enabled flag defaults to false,
			// which switches both engines off on the first boot after the upgrade.
			// The symptom reads as a matching bug, not as a settings bug.
			Reason: "a rule set is switched off by having no rules; there is no separate flag, " +
				"because a flag that defaults to off would silently disable every existing rule on upgrade",
			Detail: countDetail(len(s.Packagizer.Rules), "rule", "rules"),
		},
		{
			ID: "linkfilter", Verdict: VerdictShipped, Page: "rules",
			Switch: SwitchNone, Enabled: len(s.LinkFilter.Rules) > 0,
			Reason: "same as the Packagizer: an empty list is the off state",
			Detail: countDetail(len(s.LinkFilter.Rules), "rule", "rules"),
		},
		{
			ID: "connections", Verdict: VerdictShipped, Page: "connections",
			Switch: SwitchNone, Enabled: enabledConnections(s) > 0,
			Reason: "each outbound connection carries its own switch, so there is nothing " +
				"for one switch here to mean that the rows do not already say",
			Detail: countDetail(enabledConnections(s), "connection in use", "connections in use"),
		},
		{
			ID: "cnl", Verdict: VerdictShipped, Page: "access",
			Switch: SwitchNone, Enabled: cnlPort() > 0,
			// This is the one named in the brief that genuinely cannot be wired from
			// here, and saying so beats shipping a switch that closes nothing: the
			// listener is created in cmd/knightloader/main.go and its handle never
			// reaches the app, so nothing reachable from an HTTP handler can close
			// the port.
			Reason: "the listener is started by the process, not by the app: KL_CNL picks the port " +
				"(KL_CNL=0 switches it off) and closing it needs a restart",
			Detail: cnlDetail(),
		},
		{
			ID: "federation", Verdict: VerdictShipped, Page: "",
			Switch: SwitchNone, Enabled: len(a.Federation.List()) > 0,
			Reason: "federation is a list of peers and holds nothing open; the list being empty is its off state",
			Detail: countDetail(len(a.Federation.List()), "peer", "peers"),
		},
		{
			ID: "jd", Verdict: VerdictShipped, Page: "accounts",
			Switch: SwitchNone, Enabled: a.ContainerBackendConfigured(),
			Reason: "the headless JDownloader backend is wired at start-up from KL_JD; " +
				"unsetting it here would leave the process talking to a backend the page says is gone",
			Detail: jdDetail(a),
		},
		{
			ID: "captcha", Verdict: VerdictShipped, Page: "captcha",
			Switch: SwitchNone, Enabled: a.ContainerBackendConfigured(),
			Reason: "the only source this build relays is the headless JDownloader sidecar (internal/captcha.JDSource); " +
				"unswitchable for the same reason the jd row above is - a challenge already sitting on JD's side " +
				"does not stop existing because the switch here says otherwise",
			Detail: captchaDetail(a),
		},
		{
			ID: "scripting", Verdict: VerdictNotBuilt, Page: "advanced",
			Switch: SwitchNone,
			Reason: "there is no script host in this build; event scripts and user actions have nowhere to run",
		},
		{
			ID: "tray", Verdict: VerdictDesktop, Page: "",
			Switch: SwitchNone,
			Reason: "a tray icon needs a desktop session, which the container build does not have; " +
				"the browser tab's title and icon carry the same information there",
		},
		{
			ID: "windowpolicy", Verdict: VerdictDesktop, Page: "",
			Switch: SwitchNone,
			Reason: "what closing or minimising the window does is a property of one installation on one machine, " +
				"so it is not served from here at all",
		},
		{
			ID: "myjd", Verdict: VerdictNotBuilt, Page: "access",
			Switch: SwitchNone,
			Reason: "my.jdownloader.org is a vendor relay with no protocol to join; reaching this instance " +
				"from outside the network is a port forward, a reverse proxy or a VPN, and a peer instance covers the LAN case",
		},
		{
			ID: "updater", Verdict: VerdictNotBuilt, Page: "advanced",
			Switch: SwitchNone,
			Reason: "a container cannot replace itself from the inside, and the deployment that can " +
				"already both detects and performs the update; a second indicator would only disagree with it",
		},
	}
}

// featurePages is the sub-page list, in rail order.
//
// Every page is here whether or not anything draws it yet. A wave then fills a
// page that already exists, at an address people may already have bookmarked,
// instead of inventing one and re-deciding its name and its place in the rail —
// and until then the page explains itself out of the module rows filed under it.
func featurePages() []FeaturePage {
	return []FeaturePage{
		{ID: "general"},
		{ID: "modules"},
		{ID: "downloads", Modules: []string{"watch", "crawler", "checksums"}},
		{ID: "archives", Modules: []string{"extraction"}},
		{ID: "rules", Modules: []string{"packagizer", "linkfilter"}},
		{ID: "connections", Modules: []string{"connections"}},
		{ID: "reconnect", Modules: []string{"reconnect"}},
		{ID: "accounts", Modules: []string{"jd"}},
		{ID: "captcha", Modules: []string{"captcha"}},
		{ID: "schedule", Modules: []string{"scheduler"}},
		{ID: "look"},
		{ID: "access", Modules: []string{"cnl", "myjd"}},
		{ID: "advanced", Modules: []string{"scripting", "updater"}},
		// diagnostics, system and help carry no module row of their own,
		// same as look above: the log ring and the diagnostics bundle are
		// always-on infrastructure rather than a subsystem with an on/off
		// switch, backup/restore/quit/restart are the same regardless of
		// which resolvers or modules happen to be enabled, and help is
		// static content. They are filed here only so each gets a real,
		// bookmarkable address in the rail the way every other sub-page
		// does.
		{ID: "diagnostics"},
		{ID: "system"},
		{ID: "help"},
	}
}

// setFeature switches one module. Every branch here changes state the subsystem
// itself reads; there is deliberately no default branch that stores a flag.
func setFeature(a *app.App, id string, on bool) error {
	// The stored settings, not the redacted ones a client was shown: the router
	// and proxy passwords have to round-trip through this save untouched, and the
	// merge in Store.Set only puts back a password the client sent as the
	// placeholder.
	next := a.Settings.Get()

	switch id {
	case "extraction":
		next.Extract = on
	case "crawler":
		next.Crawl = on
	case "checksums":
		next.VerifyChecksums = on

	case "watch":
		if !on {
			if err := parkValue(a, id, next.WatchDir); err != nil {
				return err
			}
			next.WatchDir = ""
			break
		}
		var dir string
		if !unparkValue(a, id, &dir) || strings.TrimSpace(dir) == "" {
			return errors.New("there is no watch folder to switch back on; set one on the Downloads page")
		}
		next.WatchDir = dir

	case "scheduler":
		if !on {
			if err := parkValue(a, id, next.Schedule); err != nil {
				return err
			}
			next.Schedule = nil
			break
		}
		var entries []schedule.Entry
		if !unparkValue(a, id, &entries) || len(entries) == 0 {
			return errors.New("there is no timetable to switch back on; add a window on the Schedule page")
		}
		next.Schedule = entries

	case "reconnect":
		if !on {
			if err := parkValue(a, id, next.Reconnect.Method); err != nil {
				return err
			}
			next.Reconnect.Method = reconnect.MethodNone
			break
		}
		var method string
		if !unparkValue(a, id, &method) || method == "" || method == reconnect.MethodNone {
			return errors.New("there is no reconnect method to switch back on; pick one on the Reconnect page")
		}
		next.Reconnect.Method = method

	default:
		return fmt.Errorf("%s: %w", id, errNoSwitch)
	}

	// One path in and out: ApplySettings is what restarts the watcher, re-arms
	// the timetable and recompiles the rules. Writing the store directly would
	// persist the change and leave every one of those running on the old value.
	_, err := a.ApplySettings(next)
	return err
}

// parkValue remembers the value a kill switch is about to clear.
//
// A zero value is never parked. Switching an already-off module off again would
// otherwise overwrite the folder somebody set last month with an empty string,
// and the switch would then be a one-way door with no error to say so.
func parkValue(a *app.App, id string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if isEmptyJSON(b) {
		return nil
	}
	doc, err := parkDoc(a)
	if err != nil {
		return err
	}
	doc[id] = json.RawMessage(b)
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return a.SetUIState(parkBucket, string(out))
}

// unparkValue reads a parked value back. It reports whether there was one, so
// the caller can refuse with a sentence instead of silently switching a module
// on with nothing behind it.
func unparkValue(a *app.App, id string, into any) bool {
	doc, err := parkDoc(a)
	if err != nil {
		return false
	}
	raw, ok := doc[id]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, into) == nil
}

// parkedIDs is which modules have a value waiting to come back. Read once per
// table build rather than per row: it is one database read either way, and
// three rows asking separately could disagree if a switch landed in between.
func parkedIDs(a *app.App) map[string]bool {
	doc, err := parkDoc(a)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(doc))
	for id, raw := range doc {
		out[id] = !isEmptyJSON(raw)
	}
	return out
}

func parkDoc(a *app.App) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	value, err := a.UIState(parkBucket)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return doc, nil
	}
	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		// A bucket somebody else wrote something unreadable into must not make the
		// switch unusable: the park is a convenience, and starting a fresh document
		// costs at most one remembered value.
		return map[string]json.RawMessage{}, nil
	}
	return doc, nil
}

// isEmptyJSON reports whether the encoded value is one of the shapes that mean
// "nothing was configured".
func isEmptyJSON(b []byte) bool {
	switch strings.TrimSpace(string(b)) {
	case "", "null", `""`, "[]", "{}", "0", "false", `"none"`:
		return true
	}
	return false
}

func extractionDetail(s settings.Settings) string {
	if !s.Extract {
		// Said explicitly, because the switch stops the next extraction and not one
		// already under way, and somebody watching a progress bar after switching
		// it off deserves to know that is expected.
		return "off; an extraction already under way finishes"
	}
	switch extract.ParseDisposal(s.ArchiveDisposal) {
	case extract.DisposalDelete:
		return "archives are deleted after a successful extraction"
	case extract.DisposalTrash:
		// The folder is named rather than the word "trash" left to stand on its
		// own: it is a hidden folder under the download directory and not a
		// recycle bin, and this row is one of the two places anybody reads what
		// the setting actually does.
		return "archives are moved to " + extract.TrashName + " after a successful extraction"
	}
	return "archives are kept after extraction"
}

func watchDetail(s settings.Settings) string {
	if dir := strings.TrimSpace(s.WatchDir); dir != "" {
		return dir
	}
	return "no folder set"
}

func reconnectDetail(s settings.Settings) string {
	if err := s.Reconnect.Validate(); err != nil {
		// The package's own sentence, not "switched off": it distinguishes an
		// unconfigured reconnect from a half-configured one, and folding both into
		// "off" is how somebody spends an evening on a form that was already saved.
		return err.Error()
	}
	return "method: " + s.Reconnect.Method
}

func jdDetail(a *app.App) string {
	if a.ContainerBackendConfigured() {
		return "reachable; encrypted containers can be opened"
	}
	return "no backend configured (KL_JD); encrypted containers are refused with that reason"
}

// captchaDetail mirrors jdDetail's own two-branch shape rather than a
// separate live check: internal/captcha.JDSource answers ErrJDNotConfigured
// for the identical reason ContainerBackendConfigured is false, so asking
// twice would only risk the two disagreeing. CaptchaChallenges is a cache
// read (its own doc comment), never a live JD call, so this costs nothing
// worth avoiding on every module-registry request.
func captchaDetail(a *app.App) string {
	if !a.ContainerBackendConfigured() {
		return "no backend configured (KL_JD); a link needing one fails with the hoster's own error instead"
	}
	return countDetail(len(a.CaptchaChallenges()), "challenge waiting right now", "challenges waiting right now")
}

// cnlPort mirrors what cmd/knightloader/main.go does with KL_CNL. It is read
// again rather than reported by the app because the listener's handle never
// reaches the app — which is also why this module has no switch. The two
// readings agreeing is a convention, so the default lives in one named constant
// on each side and this comment is the pointer between them.
func cnlPort() int {
	v := os.Getenv("KL_CNL")
	if v == "" {
		return 9666
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 9666
	}
	return n
}

func cnlDetail() string {
	p := cnlPort()
	if p <= 0 {
		return "switched off with KL_CNL=0"
	}
	// Deliberately "configured to listen" and not "listening": a port already
	// held by a running JDownloader is logged at start-up and not fatal, and this
	// handler has no way to tell the two apart. Claiming it is up would be the
	// same lie as a switch that does nothing.
	return fmt.Sprintf("configured to listen on 127.0.0.1:%d; the start-up log says whether the port was free", p)
}

func enabledConnections(s settings.Settings) int {
	n := 0
	for _, c := range s.Connections {
		if c.Enabled {
			n++
		}
	}
	return n
}

// countDetail writes "3 rules" or "1 rule", and says nothing at all for zero —
// a row already reads as off, and "0 rules" beside it is noise.
func countDetail(n int, one, many string) string {
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
