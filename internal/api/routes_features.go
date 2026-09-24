package api

// The module registry: one row per subsystem, with a verdict about this build
// and a kill switch for the ones that have a real one. It is compiled in
// because it answers "what is in this binary", which settings.json cannot.
//
// Enabled is derived from live state on every request and never read back
// from a stored flag, so the switch and the thing it switches cannot disagree.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/extract"
	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// FeatureVerdict is what this build can say about a subsystem. "Not there" and
// "not there in this build" are different facts.
type FeatureVerdict string

const (
	// VerdictShipped means the code is in this binary and reachable from the
	// server.
	VerdictShipped FeatureVerdict = "shipped"
	// VerdictDesktop means the module is only reachable in the desktop bundle.
	VerdictDesktop FeatureVerdict = "desktop"
	// VerdictNotBuilt means the module is absent and Reason says why. Go has no
	// portable plugin loading, so the set of modules is fixed at build time.
	VerdictNotBuilt FeatureVerdict = "not-built"
)

// FeatureSwitch is how a module is switched, if it can be switched from here
// at all.
type FeatureSwitch string

const (
	// SwitchNone means there is no switch; the interface shows the control
	// disabled with Reason. A switch that stores a boolean nothing reads would
	// look like it worked.
	SwitchNone FeatureSwitch = "none"

	// SwitchSetting is a boolean the subsystem re-reads before each action, so
	// clearing it stops the next one. Nothing in flight is killed, and none of
	// these subsystems holds a goroutine open between actions.
	SwitchSetting FeatureSwitch = "setting"

	// SwitchParked is for a subsystem configured by a value rather than a flag:
	// "off" clears the value, which tears it down like any other save, and the
	// old value is parked so switching back on restores it.
	SwitchParked FeatureSwitch = "parked"
)

// Feature is one subsystem as this build has it.
type Feature struct {
	// ID is what the interface looks the translated label up by.
	ID string `json:"id"`

	Verdict FeatureVerdict `json:"verdict"`

	// Page is the settings sub-page this module is configured on, empty when it
	// has none.
	Page string `json:"page"`

	// Enabled is computed from live state on every request.
	Enabled bool `json:"enabled"`

	Switch FeatureSwitch `json:"switch"`

	// Parked is whether a SwitchParked module has a value waiting to come back.
	// Without it "switched off" and "never set up" look the same, and on a fresh
	// install the page would disable the field while the switch refuses to turn
	// on for lack of a value.
	Parked bool `json:"parked"`

	// Reason is why the verdict is what it is, or why there is no switch, in
	// English for curl, a script and the diagnostics bundle.
	Reason string `json:"reason,omitempty"`

	// ReasonCode is Reason as a value, for an interface with words of its own,
	// and ReasonArgs holds the values its wording needs. The server cannot
	// translate the sentence, since a settings request does not carry the
	// reader's language.
	ReasonCode string            `json:"reasonCode,omitempty"`
	ReasonArgs map[string]string `json:"reasonArgs,omitempty"`

	// Detail is one line of live state (the folder being watched, the port, how
	// many rules there are), in English like Reason.
	Detail string `json:"detail,omitempty"`

	// DetailCode and DetailArgs are Detail as a value, as ReasonCode is for
	// Reason.
	DetailCode string            `json:"detailCode,omitempty"`
	DetailArgs map[string]string `json:"detailArgs,omitempty"`
}

// line is one sentence of a module row: the English and the code with the
// values an interface words it by. The zero line is no sentence at all.
type line struct {
	text string
	code string
	args map[string]string
}

// withDetail is f with its detail line, so a row can be written as one literal.
func withDetail(f Feature, l line) Feature {
	f.Detail, f.DetailCode, f.DetailArgs = l.text, l.code, l.args
	return f
}

// withReason is f with its reason line.
func withReason(f Feature, l line) Feature {
	f.Reason, f.ReasonCode, f.ReasonArgs = l.text, l.code, l.args
	return f
}

// FeaturePage is one settings sub-page as registered. Every page is listed even
// when it is empty, so its address and place in the rail stay stable. Whether
// a page has controls yet is for the interface to know.
type FeaturePage struct {
	ID string `json:"id"`
	// Modules are the module ids configured on this page.
	Modules []string `json:"modules"`
}

// FeatureState is the whole registry as one document, so the modules page and
// the rail cannot disagree.
type FeatureState struct {
	Modules []Feature     `json:"modules"`
	Pages   []FeaturePage `json:"pages"`
}

// parkBucket is the interface-state bucket where a kill switch keeps the value
// it cleared. The value is not configuration, since only the switch reads it,
// and a bucket of its own keeps the browser's whole-bucket layout writes off it.
const parkBucket = "features"

// errNoSwitch is a module the caller tried to switch that has no switch. The
// table already says which rows are switchable, so this is a client bug and
// answers 400.
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
			id := r.PathValue("id")
			if err := setFeature(a, id, body.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if id == "cnl" {
				// The listener is not a setting, so no settings save announced it.
				a.Hub.Broadcast("settings", nil)
			}
			// The whole table, since one switch can change what other rows and
			// pages may offer.
			writeJSON(w, featureState(a))
		})

	reg.Add(http.MethodGet, "/api/settings/defaults", "the factory settings and the type of every settings key, for the advanced table's per-row reset",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, settingsSchema{
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

// settingsKinds reads the type of every settings field off the struct. The
// values alone cannot tell: an empty []string encodes as null, and a key
// dropped by omitempty would not appear at all.
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
			// Not walked into: a rule list is ordered and cannot be edited
			// safely one condition at a time.
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
		withDetail(Feature{
			ID: "extraction", Verdict: VerdictShipped, Page: "archives",
			Switch: SwitchSetting, Enabled: s.Extract,
		}, extractionDetail(s)),
		withDetail(Feature{
			ID: "watch", Verdict: VerdictShipped, Page: "collector",
			Switch: SwitchParked, Enabled: strings.TrimSpace(s.WatchDir) != "",
			Parked: parked["watch"],
		}, watchDetail(s)),
		withDetail(Feature{
			ID: "feeds", Verdict: VerdictShipped, Page: "downloads",
			Switch: SwitchParked, Enabled: len(s.Feeds) > 0,
			Parked: parked["feeds"],
		}, countDetail(len(s.Feeds), "subscriptions", "subscription", "subscriptions")),
		withDetail(Feature{
			// Enabled counts the targets that would send, not the rows that
			// exist.
			ID: "eventtargets", Verdict: VerdictShipped, Page: "automation",
			Switch: SwitchParked, Enabled: enabledEventTargets(s) > 0,
			Parked: parked["eventtargets"],
		}, countDetail(enabledEventTargets(s), "targetsSending", "target sending", "targets sending")),
		{
			ID: "crawler", Verdict: VerdictShipped, Page: "collector",
			Switch: SwitchSetting, Enabled: s.Crawl,
		},
		{
			ID: "checksums", Verdict: VerdictShipped, Page: "downloads",
			Switch: SwitchSetting, Enabled: s.VerifyChecksums,
		},
		withDetail(Feature{
			ID: "scheduler", Verdict: VerdictShipped, Page: "automation",
			Switch: SwitchParked, Enabled: len(s.Schedule) > 0,
			Parked: parked["scheduler"],
		}, countDetail(len(s.Schedule), "windows", "window", "windows")),
		withDetail(Feature{
			ID: "reconnect", Verdict: VerdictShipped, Page: "network",
			Switch: SwitchParked, Enabled: a.ReconnectState().Configured,
			Parked: parked["reconnect"],
		}, reconnectDetail(s)),
		withDetail(Feature{
			// The same flag as the list's own switch on the Rules page.
			ID: "packagizer", Verdict: VerdictShipped, Page: "rules",
			Switch: SwitchSetting, Enabled: !s.Packagizer.Disabled,
		}, offDetail(s.Packagizer.Disabled,
			line{text: "off; new links are not sorted by the rules", code: "packagizerOff"},
			countDetail(len(s.Packagizer.Rules), "rules", "rule", "rules"))),
		withDetail(Feature{
			ID: "linkfilter", Verdict: VerdictShipped, Page: "rules",
			Switch: SwitchSetting, Enabled: !s.LinkFilter.Disabled,
		}, offDetail(s.LinkFilter.Disabled,
			line{text: "off; every link is taken in", code: "linkfilterOff"},
			countDetail(len(s.LinkFilter.Rules), "rules", "rule", "rules"))),
		withDetail(Feature{
			ID: "connections", Verdict: VerdictShipped, Page: "network",
			Switch: SwitchSetting, Enabled: !s.ModuleOff("connections"),
		}, offDetail(s.ModuleOff("connections"),
			line{text: "off; new downloads go out over this machine's own address", code: "connectionsOff"},
			countDetail(enabledConnections(s), "connectionsInUse", "connection in use", "connections in use"))),
		cnlFeature(a),
		withDetail(Feature{
			ID: "federation", Verdict: VerdictShipped, Page: "instances",
			Switch: SwitchSetting, Enabled: !s.ModuleOff("federation"),
		}, offDetail(s.ModuleOff("federation"),
			line{text: "off; peers stay saved, but this instance neither lists nor contacts them", code: "federationOff"},
			countDetail(len(a.Federation.List()), "peers", "peer", "peers"))),
		jdFeature(a, s),
		ytdlpFeature(a, s),
		withDetail(Feature{
			ID: "torrents", Verdict: VerdictShipped, Page: "torrents",
			Switch: SwitchSetting, Enabled: !s.ModuleOff("torrents"),
		}, offDetail(s.ModuleOff("torrents"),
			line{text: "off; running torrents carry on and keep seeding, new ones wait until it is switched back on", code: "torrentsOff"},
			torrentsDetail(a))),
		captchaFeature(a, s),
		withDetail(Feature{
			// On the access tab: it decides who may reach in and create
			// downloads, not how downloads behave.
			ID: "downloadclient", Verdict: VerdictShipped, Page: "access",
			Switch: SwitchSetting, Enabled: s.DownloadClientAPI,
		}, downloadClientDetail(a, s)),
		withDetail(Feature{
			ID: "metrics", Verdict: VerdictShipped, Page: "health",
			Switch: SwitchSetting, Enabled: s.Metrics,
		}, metricsDetail(a, s)),
		withDetail(Feature{
			// Over the scripts' own switches: off here, no event starts any of
			// them, and each keeps its own setting for when this is back on.
			ID: "scripting", Verdict: VerdictShipped, Page: "automation",
			Switch: SwitchSetting, Enabled: !s.ModuleOff("scripting"),
		}, offDetail(s.ModuleOff("scripting"),
			line{text: "off; no event starts a script, and a script already running finishes", code: "scriptingOff"},
			countDetail(enabledScripts(a), "scriptsEnabled", "script enabled", "scripts enabled"))),
		withReason(Feature{
			ID: "tray", Verdict: VerdictDesktop, Page: "",
			Switch: SwitchNone,
		}, line{
			text: "a tray icon needs a desktop session, which the container build does not have; " +
				"the browser tab's title and icon carry the same information there",
			code: "tray",
		}),
		withReason(Feature{
			ID: "windowpolicy", Verdict: VerdictDesktop, Page: "",
			Switch: SwitchNone,
		}, line{
			text: "what closing or minimising the window does is a property of one installation on one machine, " +
				"so it is not served from here at all",
			code: "windowpolicy",
		}),
		withReason(Feature{
			ID: "updater", Verdict: updaterVerdict(), Page: "look",
			Switch: SwitchNone,
		}, updaterReason()),
	}
}

// updaterVerdict depends on the deployment: both builds can check for a newer
// release, but only the desktop build can hand over an installer, since a
// container cannot replace itself from the inside.
func updaterVerdict() FeatureVerdict {
	if buildinfo.Deployment == "desktop" {
		return VerdictDesktop
	}
	return VerdictNotBuilt
}

func updaterReason() line {
	if buildinfo.Deployment == "desktop" {
		return line{
			text: "checks GitHub for a newer release on demand from the General tab, or automatically on load there if its toggle is on; " +
				"downloading and installing it is a manual step there, and nothing is applied silently",
			code: "updaterDesktop",
		}
	}
	return line{
		text: "a container cannot replace itself from the inside, so the General tab's update check only tells you a newer release exists " +
			"and points at it, same as on desktop; to update, pull the new image the way you deployed this one " +
			"(docker pull, Unraid Community Applications, Watchtower, ...), which your deployment already does for you or lets you do",
		code: "updaterContainer",
	}
}

// featurePages is the sub-page list, in rail order. Pages without a module row
// (appearance, accounts, shortcuts, diagnostics, help, browsertools) hold
// preferences or tools rather than a subsystem with an on/off state.
// JDownloader is switched only on the modules page, since the accounts page
// holds the logins it uses and not the backend itself.
func featurePages() []FeaturePage {
	return []FeaturePage{
		// The General tab keeps the id "look" so bookmarked addresses and the
		// stored tab order still resolve.
		{ID: "look", Modules: []string{"updater"}},
		{ID: "appearance"},
		{ID: "modules"},
		// Everything that decides how a link gets in and what happens to it
		// before it becomes a download.
		{ID: "collector", Modules: []string{"cnl", "watch", "crawler"}},
		{ID: "downloads", Modules: []string{"feeds", "checksums"}},
		{ID: "archives", Modules: []string{"extraction"}},
		// The categories table sits on this page, because a Packagizer rule
		// naming a missing category is refused and the table should be in reach.
		{ID: "rules", Modules: []string{"packagizer", "linkfilter"}},
		{ID: "network", Modules: []string{"connections", "reconnect"}},
		{ID: "accounts"},
		{ID: "instances", Modules: []string{"federation"}},
		{ID: "resolvers", Modules: []string{"ytdlp"}},
		{ID: "torrents", Modules: []string{"torrents"}},
		{ID: "captcha", Modules: []string{"captcha"}},
		// What runs with nobody at the screen. Event targets are not under
		// downloads, since most of the events they report on are not about a
		// download.
		{ID: "automation", Modules: []string{"scheduler", "eventtargets", "scripting"}},
		{ID: "shortcuts"},
		{ID: "access", Modules: []string{"downloadclient"}},
		{ID: "advanced"},
		// Health comes before diagnostics: one says whether the instance works
		// now, the other hands over a bundle for a report about why it did not.
		{ID: "health", Modules: []string{"metrics"}},
		{ID: "diagnostics"},
		{ID: "help"},
		{ID: "browsertools"},
	}
}

// featureMu makes each switch one read, edit and write of the settings, so two
// switches flipped together cannot each write the list the other one read.
var featureMu sync.Mutex

// setFeature switches one module. Every branch changes state the subsystem
// itself reads; no branch stores a flag of its own.
func setFeature(a *app.App, id string, on bool) error {
	featureMu.Lock()
	defer featureMu.Unlock()
	// The stored settings, not the redacted ones a client was shown, so the
	// router and proxy passwords survive this save.
	next := a.Settings.Get()

	// A row can lose its switch at run time (no JD wired, no yt-dlp found);
	// the table is the one place that knows.
	for _, f := range featureList(a) {
		if f.ID == id && f.Switch == SwitchNone {
			return fmt.Errorf("%s: %w", id, errNoSwitch)
		}
	}

	switch id {
	case "cnl":
		// A live listener rather than a setting, so it skips ApplySettings
		// (see app.App.CnL). Without a listener the loop above has already
		// refused, since the row then has no switch.
		return a.CnL.Toggle(on)

	case "extraction":
		next.Extract = on
	case "crawler":
		next.Crawl = on
	case "checksums":
		next.VerifyChecksums = on
	case "downloadclient":
		// The route re-reads the flag on every request, so clearing it closes
		// the door on the next call.
		next.DownloadClientAPI = on
	case "metrics":
		next.Metrics = on
	case "packagizer":
		next.Packagizer.Disabled = !on
	case "linkfilter":
		next.LinkFilter.Disabled = !on
	case "connections", "federation", "jd", "ytdlp", "torrents", "captcha", "scripting":
		next.ModulesOff = switchModule(next.ModulesOff, id, on)

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
			return errors.New("there is no watch folder to switch back on; set one on the Link collector page")
		}
		next.WatchDir = dir

	case "feeds":
		if !on {
			if err := parkValue(a, id, next.Feeds); err != nil {
				return err
			}
			next.Feeds = nil
			break
		}
		var subs []feed.Subscription
		if !unparkValue(a, id, &subs) || len(subs) == 0 {
			return errors.New("there is no subscription to switch back on; add a feed on the Downloads page")
		}
		next.Feeds = subs

	case "eventtargets":
		if !on {
			if err := parkValue(a, id, next.EventTargets); err != nil {
				return err
			}
			next.EventTargets = nil
			break
		}
		var targets []notify.Target
		if !unparkValue(a, id, &targets) || len(targets) == 0 {
			return errors.New("there is no event target to switch back on; add one on the Automation page")
		}
		next.EventTargets = targets

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
			return errors.New("there is no timetable to switch back on; add a window on the Automation page")
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
			return errors.New("there is no reconnect method to switch back on; pick one on the Network page")
		}
		next.Reconnect.Method = method

	default:
		return fmt.Errorf("%s: %w", id, errNoSwitch)
	}

	// ApplySettings restarts the watcher, re-arms the timetable and recompiles
	// the rules; writing the store directly would leave them on the old value.
	_, err := a.ApplySettings(next)
	return err
}

// parkValue remembers the value a kill switch is about to clear. A zero value
// is never parked, so switching an already-off module off again cannot
// overwrite the value parked earlier.
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

// unparkValue reads a parked value back and reports whether there was one.
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

// parkedIDs is which modules have a value waiting to come back. It is read once
// per table build so the rows cannot disagree with each other.
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
		// An unreadable bucket must not make the switch unusable; starting over
		// costs at most the remembered values.
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

func extractionDetail(s settings.Settings) line {
	if !s.Extract {
		return line{text: "off; an extraction already under way finishes", code: "extractionOff"}
	}
	switch extract.ParseDisposal(s.ArchiveDisposal) {
	case extract.DisposalDelete:
		return line{text: "archives are deleted after a successful extraction", code: "extractionDelete"}
	case extract.DisposalTrash:
		// Named, because it is a hidden folder under the download directory
		// and not a recycle bin.
		return line{
			text: "archives are moved to " + extract.TrashName + " after a successful extraction",
			code: "extractionTrash", args: map[string]string{"folder": extract.TrashName},
		}
	}
	return line{text: "archives are kept after extraction", code: "extractionKeep"}
}

// downloadClientDetail is the live line of the SABnzbd bridge row. It warns
// when no API token exists, since the route then refuses every call, and when
// per-package folders are off, since the importer then finds several releases
// in the folder the bridge reports.
func downloadClientDetail(a *app.App, s settings.Settings) line {
	if !s.DownloadClientAPI {
		return line{
			text: "off; Sonarr and Radarr get a 404 from it, the same as for an endpoint that does not exist",
			code: "downloadclientOff",
		}
	}
	const (
		noToken     = "no API token exists yet, so every call is refused; create one on this page"
		noSubfolder = "per-package folders are off, so every grab lands in one folder and the importer cannot tell them apart"
	)
	tokenless, flat := len(a.APITokens.List()) == 0, !s.SubfolderByPackage
	switch {
	case tokenless && flat:
		return line{text: noToken + "; " + noSubfolder, code: "downloadclientNoTokenNoSubfolders"}
	case tokenless:
		return line{text: noToken, code: "downloadclientNoToken"}
	case flat:
		return line{text: noSubfolder, code: "downloadclientNoSubfolders"}
	}
	return line{
		text: "reachable at /api/sabnzbd/api; set Sonarr's or Radarr's URL Base to \"api/sabnzbd\" and its API key to one of this instance's tokens",
		code: "downloadclientReady",
	}
}

func watchDetail(s settings.Settings) line {
	if dir := strings.TrimSpace(s.WatchDir); dir != "" {
		return line{text: dir, code: "watchFolder", args: map[string]string{"folder": dir}}
	}
	return line{text: "no folder set", code: "watchNone"}
}

// reconnectDetail says which method runs, or what is missing. A problem
// carries the code the Reconnect page already translates, under the prefix
// "reconnect.".
func reconnectDetail(s settings.Settings) line {
	if err := s.Reconnect.Validate(); err != nil {
		// Validate's sentence tells an unconfigured reconnect from a
		// half-configured one.
		l := line{text: err.Error()}
		var p *reconnect.ConfigProblem
		if errors.As(err, &p) {
			l.code = "reconnect." + p.Code
			l.args = map[string]string{"n": strconv.Itoa(p.N), "method": p.Method, "var": p.Var}
		}
		return l
	}
	return line{
		text: "method: " + s.Reconnect.Method,
		code: "reconnectMethod", args: map[string]string{"method": s.Reconnect.Method},
	}
}

// offDetail is the detail line of a switchable row: what switching it off
// means while it is off, the live state otherwise.
func offDetail(off bool, whenOff, whenOn line) line {
	if off {
		return whenOff
	}
	return whenOn
}

// jdFeature has a switch only when a JD backend is wired, since without one
// there is nothing for it to switch on.
func jdFeature(a *app.App, s settings.Settings) Feature {
	f := Feature{ID: "jd", Verdict: VerdictShipped}
	if !a.ContainerBackendConfigured() {
		f.Switch = SwitchNone
		f = withReason(f, line{
			text: "no JDownloader backend is configured; set KL_JD to a reachable JDownloader and restart",
			code: "jdNotConfigured",
		})
		return withDetail(f, line{
			text: "encrypted containers are refused, and hoster links go to a debrid service or fail",
			code: "jdMissing",
		})
	}
	off := s.ModuleOff("jd")
	f.Switch, f.Enabled = SwitchSetting, !off
	return withDetail(f, offDetail(off,
		line{text: "off; downloads already in JDownloader finish, and new hoster links wait until it is switched back on", code: "jdOff"},
		line{text: "reachable; encrypted containers can be opened", code: "jdReachable"}))
}

// ytdlpFeature has a switch only when a yt-dlp binary was found.
func ytdlpFeature(a *app.App, s settings.Settings) Feature {
	f := withDetail(Feature{ID: "ytdlp", Verdict: VerdictShipped, Page: "resolvers"}, ytdlpDetail(a, s))
	if !resolverRegistered(a, "ytdlp") {
		f.Switch = SwitchNone
		return withReason(f, line{
			text: "no yt-dlp binary was found; fetch one on the Resolvers page, or set KL_YTDLP",
			code: "ytdlpMissing",
		})
	}
	off := s.ModuleOff("ytdlp")
	f.Switch, f.Enabled = SwitchSetting, !off
	if off {
		f = withDetail(f, line{
			text: "off; a running download finishes, and video links wait until it is switched back on",
			code: "ytdlpOff",
		})
	}
	return f
}

// captchaFeature follows the JD row: JD is the only source of challenges
// (internal/captcha.JDSource), so switching JD off silences this too.
func captchaFeature(a *app.App, s settings.Settings) Feature {
	f := Feature{ID: "captcha", Verdict: VerdictShipped, Page: "captcha"}
	if !a.ContainerBackendConfigured() {
		f.Switch = SwitchNone
		return withReason(f, line{
			text: "challenges come only from the JDownloader backend, and none is configured (KL_JD)",
			code: "captchaNoJD",
		})
	}
	if s.ModuleOff("jd") {
		f.Switch = SwitchNone
		return withReason(f, line{
			text: "challenges come only from JDownloader, which is switched off; switch it back on first",
			code: "captchaJDOff",
		})
	}
	off := s.ModuleOff("captcha")
	f.Switch, f.Enabled = SwitchSetting, !off
	return withDetail(f, offDetail(off,
		line{text: "off; no prompt opens, and JDownloader gives up on a link once its captcha expires", code: "captchaOff"},
		captchaDetail(a)))
}

// switchModule returns a copy of the ModulesOff list with id added or taken
// out. A copy, because the slice is shared with the stored settings.
func switchModule(off []string, id string, on bool) []string {
	out := slices.DeleteFunc(slices.Clone(off), func(m string) bool { return m == id })
	if !on {
		out = append(out, id)
	}
	return out
}

// resolverRegistered reports whether a resolver with this id is in the live
// routing table right now. The ytdlp resolver is only registered once the
// binary has actually run.
func resolverRegistered(a *app.App, id string) bool {
	for _, rid := range a.Registry.IDs() {
		if rid == id {
			return true
		}
	}
	return false
}

// ytdlpDetail says why yt-dlp is unavailable, or which one runs and with what
// quality.
func ytdlpDetail(a *app.App, s settings.Settings) line {
	// The cached snapshot the Resolvers page and the diagnostics bundle read,
	// so the three cannot disagree.
	tools := a.MediaTools()
	if !resolverRegistered(a, "ytdlp") {
		text := "yt-dlp binary not found (a copy fetched on the Resolvers page, KL_YTDLP, or \"yt-dlp\" on PATH); media pages fail with the hoster's own error instead"
		// The probe's own words stay out of the translated line: they are
		// English from the tool check, and the Resolvers page shows them.
		if tools.Ytdlp.Detail != "" {
			text += " (" + tools.Ytdlp.Detail + ")"
		}
		return line{text: text, code: "ytdlpNotFound"}
	}
	quality := string(s.Ytdlp.Quality)
	if tools.Ytdlp.Version == "" {
		return line{text: "quality: " + quality, code: "ytdlpQuality", args: map[string]string{"quality": quality}}
	}
	return line{
		text: "yt-dlp " + tools.Ytdlp.Version + " (" + string(tools.Ytdlp.Source) + "); quality: " + quality,
		code: "ytdlpVersion",
		args: map[string]string{"version": tools.Ytdlp.Version, "source": string(tools.Ytdlp.Source), "quality": quality},
	}
}

// torrentsDetail reads the live routing table like the other resolver rows.
// The torrent resolver is registered unconditionally at boot, so the first
// branch only matters if that ever becomes conditional.
func torrentsDetail(a *app.App) line {
	if !resolverRegistered(a, "torrent") {
		return line{
			text: "the torrent resolver is not registered on the live routing table; " +
				"magnet links and .torrent uploads are refused as unsupported",
			code: "torrentsUnregistered",
		}
	}
	return line{
		text: "magnet links and uploaded .torrent files are routed to the embedded torrent engine",
		code: "torrentsRouted",
	}
}

// captchaDetail counts the open challenges. CaptchaChallenges is a cache
// read, not a JD call.
func captchaDetail(a *app.App) line {
	return countDetail(len(a.CaptchaChallenges()), "challenges", "challenge waiting right now", "challenges waiting right now")
}

// cnlFeature describes a.CnL, the listener the embedding started. Without one
// the row says so and has no switch, rather than reading KL_CNL and claiming a
// listener nobody opened.
func cnlFeature(a *app.App) Feature {
	// On the link collector tab: Click'n'Load is how links get in, while the
	// access tab is about who gets in.
	f := Feature{ID: "cnl", Verdict: VerdictShipped, Page: "collector", Switch: SwitchNone}
	l := a.CnL
	if l == nil {
		return withReason(f, line{
			text: "this process starts no Click'n'Load listener",
			code: "cnlNoListener",
		})
	}
	addr := map[string]string{"address": l.Address()}
	f.Switch, f.Enabled = SwitchSetting, l.Port() > 0
	f = withReason(f, line{
		text: "the standard Click'n'Load port, 127.0.0.1:9666 unless KL_CNL names another; " +
			"switching this off here does not change KL_CNL itself, so after a restart the listener comes back up the way the environment says",
		code: "cnlSwitch",
	})
	switch err := l.Err(); {
	case f.Enabled:
		return withDetail(f, line{text: "listening on " + l.Address(), code: "cnlListening", args: addr})
	case err != nil:
		// Usually a running JDownloader holding the port.
		return withDetail(f, line{
			text: "cannot listen on " + l.Address() + ": " + err.Error(),
			code: "cnlUnavailable", args: addr,
		})
	case l.OffByEnv():
		return withDetail(f, line{text: "switched off with KL_CNL=0", code: "cnlOffByEnv"})
	}
	return withDetail(f, line{text: "switched off", code: "cnlOff"})
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

// enabledScripts counts the enabled scripts in scripts.json, which is not part
// of the settings.
func enabledScripts(a *app.App) int {
	n := 0
	for _, sc := range a.Scripts.ListScripts() {
		if sc.Enabled {
			n++
		}
	}
	return n
}

// countDetail writes "3 rules" or "1 rule", and nothing for zero, where the
// row already reads as off. The code is code for many and code+"One" for one,
// since the interface has no plural rules of its own.
func countDetail(n int, code, one, many string) line {
	switch n {
	case 0:
		return line{}
	case 1:
		return line{text: "1 " + one, code: code + "One"}
	}
	return line{text: strconv.Itoa(n) + " " + many, code: code, args: map[string]string{"n": strconv.Itoa(n)}}
}
