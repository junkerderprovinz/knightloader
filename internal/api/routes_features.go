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
	"os"
	"reflect"
	"strconv"
	"strings"

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

	// Reason is why the verdict is what it is, or why there is no switch. It is
	// untranslated English, like a Go error, because it is a fact about this
	// build.
	Reason string `json:"reason,omitempty"`

	// Detail is one line of live state (the folder being watched, the port, how
	// many rules there are).
	Detail string `json:"detail,omitempty"`
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
			if err := setFeature(a, r.PathValue("id"), body.Enabled); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
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
			ID: "feeds", Verdict: VerdictShipped, Page: "downloads",
			Switch: SwitchParked, Enabled: len(s.Feeds) > 0,
			Parked: parked["feeds"], Detail: countDetail(len(s.Feeds), "subscription", "subscriptions"),
		},
		{
			// Enabled counts the targets that would send, not the rows that
			// exist.
			ID: "eventtargets", Verdict: VerdictShipped, Page: "eventtargets",
			Switch: SwitchParked, Enabled: enabledEventTargets(s) > 0,
			Parked: parked["eventtargets"],
			Detail: countDetail(enabledEventTargets(s), "target sending", "targets sending"),
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
			// On the General tab: Click'n'Load is how links get in, while the
			// access tab is about who gets in.
			ID: "cnl", Verdict: VerdictShipped, Page: "look",
			Switch: cnlSwitch(a), Enabled: cnlEnabled(a),
			Reason: cnlReason(a),
			Detail: cnlDetail(a),
		},
		{
			ID: "federation", Verdict: VerdictShipped, Page: "instances",
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
			ID: "ytdlp", Verdict: VerdictShipped, Page: "resolvers",
			Switch: SwitchNone, Enabled: resolverRegistered(a, "ytdlp"),
			Reason: "the yt-dlp binary is detected at start-up (KL_YTDLP, or \"yt-dlp\" on PATH); " +
				"missing it here needs a restart, the same as the jd backend above",
			Detail: ytdlpDetail(a, s),
		},
		{
			ID: "torrents", Verdict: VerdictShipped, Page: "torrents",
			Switch: SwitchNone, Enabled: resolverRegistered(a, "torrent"),
			Reason: "a magnet link or .torrent upload is routed by matching the live resolver table above; " +
				"there is no separate flag here to disagree with it",
			Detail: torrentsDetail(a),
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
			// On the access tab: it decides who may reach in and create
			// downloads, not how downloads behave.
			ID: "downloadclient", Verdict: VerdictShipped, Page: "access",
			Switch: SwitchSetting, Enabled: s.DownloadClientAPI,
			Detail: downloadClientDetail(a, s),
		},
		{
			ID: "metrics", Verdict: VerdictShipped, Page: "health",
			Switch: SwitchSetting, Enabled: s.Metrics,
			Detail: metricsDetail(a, s),
		},
		{
			ID: "scripting", Verdict: VerdictShipped, Page: "scripts",
			Switch: SwitchNone, Enabled: enabledScripts(a) > 0,
			Reason: "a script is switched off by its own enabled field, edited on the Scripts page; there is no second flag here to disagree with it",
			Detail: countDetail(enabledScripts(a), "script enabled", "scripts enabled"),
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
			ID: "updater", Verdict: updaterVerdict(), Page: "look",
			Switch: SwitchNone,
			Reason: updaterReason(),
		},
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

func updaterReason() string {
	if buildinfo.Deployment == "desktop" {
		return "checks GitHub for a newer release on demand from the General tab, or automatically on load there if its toggle is on; " +
			"downloading and installing it is still a manual step there, same as any other desktop app before it grows a silent auto-apply"
	}
	return "a container cannot replace itself from the inside, so the General tab's check-and-notify only tells you a newer release exists " +
		"and points at it, same as on desktop - updating still means pulling the new image the way you deployed this one " +
		"(docker pull, Unraid Community Applications, Watchtower, ...), which is what the deployment that runs it already does or lets you do"
}

// featurePages is the sub-page list, in rail order. Pages without a module row
// (appearance, categories, shortcuts, diagnostics, help, browsertools) hold
// preferences or tools rather than a subsystem with an on/off state.
func featurePages() []FeaturePage {
	return []FeaturePage{
		// The General tab keeps the id "look" so bookmarked addresses and the
		// stored tab order still resolve.
		{ID: "look", Modules: []string{"updater", "cnl"}},
		{ID: "appearance"},
		{ID: "modules"},
		{ID: "downloads", Modules: []string{"watch", "feeds", "crawler", "checksums"}},
		{ID: "archives", Modules: []string{"extraction"}},
		{ID: "rules", Modules: []string{"packagizer", "linkfilter"}},
		// Right after rules, because a Packagizer rule naming a missing
		// category is refused and the table should be one step away.
		{ID: "categories"},
		{ID: "connections", Modules: []string{"connections"}},
		{ID: "reconnect", Modules: []string{"reconnect"}},
		{ID: "accounts", Modules: []string{"jd"}},
		{ID: "instances", Modules: []string{"federation"}},
		{ID: "resolvers", Modules: []string{"ytdlp"}},
		{ID: "torrents", Modules: []string{"torrents"}},
		{ID: "captcha", Modules: []string{"captcha"}},
		{ID: "schedule", Modules: []string{"scheduler"}},
		// Not under downloads: most of the events a target reports on are
		// not about a download.
		{ID: "eventtargets", Modules: []string{"eventtargets"}},
		{ID: "shortcuts"},
		{ID: "access", Modules: []string{"downloadclient"}},
		{ID: "scripts", Modules: []string{"scripting"}},
		{ID: "advanced"},
		// Health comes before diagnostics: one says whether the instance works
		// now, the other hands over a bundle for a report about why it did not.
		{ID: "health", Modules: []string{"metrics"}},
		{ID: "diagnostics"},
		{ID: "help"},
		{ID: "browsertools"},
	}
}

// setFeature switches one module. Every branch changes state the subsystem
// itself reads; no branch stores a flag of its own.
func setFeature(a *app.App, id string, on bool) error {
	// The stored settings, not the redacted ones a client was shown, so the
	// router and proxy passwords survive this save.
	next := a.Settings.Get()

	switch id {
	case "cnl":
		// A live listener rather than a setting, so it skips ApplySettings
		// (see app.App.CnLToggle).
		if a.CnLToggle == nil {
			return fmt.Errorf("%s: %w", id, errNoSwitch)
		}
		return a.CnLToggle(on)

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
			return errors.New("there is no event target to switch back on; add one on the Event targets page")
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

func extractionDetail(s settings.Settings) string {
	if !s.Extract {
		return "off; an extraction already under way finishes"
	}
	switch extract.ParseDisposal(s.ArchiveDisposal) {
	case extract.DisposalDelete:
		return "archives are deleted after a successful extraction"
	case extract.DisposalTrash:
		// Named, because it is a hidden folder under the download directory
		// and not a recycle bin.
		return "archives are moved to " + extract.TrashName + " after a successful extraction"
	}
	return "archives are kept after extraction"
}

// downloadClientDetail is the live line of the SABnzbd bridge row. It warns
// when no API token exists, since the route then refuses every call, and when
// per-package folders are off, since the importer then finds several releases
// in the folder the bridge reports.
func downloadClientDetail(a *app.App, s settings.Settings) string {
	if !s.DownloadClientAPI {
		return "off; Sonarr and Radarr get a 404 from it, the same as for an endpoint that does not exist"
	}
	var notes []string
	if len(a.APITokens.List()) == 0 {
		notes = append(notes, "no API token exists yet, so every call is refused; create one on this page")
	}
	if !s.SubfolderByPackage {
		notes = append(notes, "per-package folders are off, so every grab lands in one folder and the importer cannot tell them apart")
	}
	if len(notes) == 0 {
		return "reachable at /api/sabnzbd/api; set Sonarr's or Radarr's URL Base to \"api/sabnzbd\" and its API key to one of this instance's tokens"
	}
	return strings.Join(notes, "; ")
}

func watchDetail(s settings.Settings) string {
	if dir := strings.TrimSpace(s.WatchDir); dir != "" {
		return dir
	}
	return "no folder set"
}

func reconnectDetail(s settings.Settings) string {
	if err := s.Reconnect.Validate(); err != nil {
		// Validate's sentence tells an unconfigured reconnect from a
		// half-configured one.
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
func ytdlpDetail(a *app.App, s settings.Settings) string {
	// The cached snapshot the Resolvers page and the diagnostics bundle read,
	// so the three cannot disagree.
	tools := a.MediaTools()
	if !resolverRegistered(a, "ytdlp") {
		detail := "yt-dlp binary not found (a copy fetched on the Resolvers page, KL_YTDLP, or \"yt-dlp\" on PATH); media pages fail with the hoster's own error instead"
		if tools.Ytdlp.Detail != "" {
			detail += " (" + tools.Ytdlp.Detail + ")"
		}
		return detail
	}
	out := "quality: " + string(s.Ytdlp.Quality)
	if tools.Ytdlp.Version != "" {
		out = "yt-dlp " + tools.Ytdlp.Version + " (" + string(tools.Ytdlp.Source) + "); " + out
	}
	return out
}

// torrentsDetail reads the live routing table like the other resolver rows.
// The torrent resolver is registered unconditionally at boot, so the first
// branch only matters if that ever becomes conditional.
func torrentsDetail(a *app.App) string {
	if !resolverRegistered(a, "torrent") {
		return "the torrent resolver is not registered on the live routing table; " +
			"magnet links and .torrent uploads are refused as unsupported"
	}
	return "magnet links and uploaded .torrent files are routed to the embedded torrent engine"
}

// captchaDetail checks the same condition as jdDetail, since captcha.JDSource
// fails for the same reason. CaptchaChallenges is a cache read, not a JD call.
func captchaDetail(a *app.App) string {
	if !a.ContainerBackendConfigured() {
		return "no backend configured (KL_JD); a link needing one fails with the hoster's own error instead"
	}
	return countDetail(len(a.CaptchaChallenges()), "challenge waiting right now", "challenges waiting right now")
}

// cnlPort reads KL_CNL the way cmd/knightloader/main.go does. It is the
// fallback for an App that does not wire a.CnLPort, such as the desktop build
// or a test.
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

// cnlSwitch and the helpers after it depend on a.CnLPort and a.CnLToggle,
// which only cmd/knightloader/main.go sets; without them the row falls back to
// the environment.
func cnlSwitch(a *app.App) FeatureSwitch {
	if a.CnLToggle == nil {
		return SwitchNone
	}
	return SwitchSetting
}

func cnlEnabled(a *app.App) bool {
	if a.CnLPort != nil {
		return a.CnLPort() > 0
	}
	return cnlPort() > 0
}

func cnlReason(a *app.App) string {
	if a.CnLToggle != nil {
		return "the standard Click'n'Load port, 127.0.0.1:9666 unless KL_CNL names another - " +
			"switching this off here does not change KL_CNL itself, so a restart still comes back up the way the environment says"
	}
	return "the listener is started by the process, not by the app: KL_CNL picks the port " +
		"(KL_CNL=0 switches it off) and closing it needs a restart"
}

func cnlDetail(a *app.App) string {
	if a.CnLPort != nil {
		if p := a.CnLPort(); p > 0 {
			// CnLPort is only non-zero once the port is actually bound.
			return fmt.Sprintf("listening on 127.0.0.1:%d", p)
		}
		return "switched off"
	}
	if cnlPort() <= 0 {
		return "switched off with KL_CNL=0"
	}
	// Only "configured": a port already held by a running JDownloader is
	// logged at start-up, and this handler cannot tell the two apart.
	return fmt.Sprintf("configured to listen on 127.0.0.1:%d; the start-up log says whether the port was free", cnlPort())
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
// row already reads as off.
func countDetail(n int, one, many string) string {
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
