package app

// The move from JDownloader: a cfg folder is read into a preview, held here
// under a token while the user ticks what to take over, and then applied.
// The folder itself is never written to. Passwords stay on the server between
// the two steps, so the preview never carries one.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/jdimport"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The groups a preview item belongs to, in the order the preview lists them.
const (
	JDGroupAccounts   = "accounts"
	JDGroupSettings   = "settings"
	JDGroupPackagizer = "packagizer"
	JDGroupFilter     = "filter"
	JDGroupDownloads  = "downloads"
)

// JDImportItem is one thing the import can write, as the preview shows it.
type JDImportItem struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	// Kind is hoster, debrid, passwords, folder, rule, exception or package.
	Kind string `json:"kind"`
	// Name is the host, the service, the rule or the package. For the folder
	// it is the folder, and for the passwords it is empty.
	Name string `json:"name"`
	// Detail is the user name of an account, or the folder stored here now.
	Detail string `json:"detail,omitempty"`
	// Slot is the named account a debrid key is stored under when the
	// service already has a default one here.
	Slot string `json:"slot,omitempty"`
	// Count is how many links a package brings, or how many archive passwords
	// are new here.
	Count int `json:"count,omitempty"`
	// Total is every archive password in JDownloader's list.
	Total int `json:"total,omitempty"`
	// Off marks an account or rule switched off in JDownloader. It arrives
	// switched off.
	Off bool `json:"off,omitempty"`
	// Replaces marks an item that overwrites something stored here.
	Replaces bool `json:"replaces,omitempty"`
	// Same marks an item that is already here exactly as it would arrive.
	Same bool `json:"same,omitempty"`
	// Blocked says why the item cannot be taken over.
	Blocked *jdimport.Reason  `json:"blocked,omitempty"`
	Notes   []jdimport.Reason `json:"notes,omitempty"`
	// Ticked is what the preview offers ticked: everything that can come over
	// and would change something, except what replaces a stored value or is
	// switched off in JDownloader.
	Ticked bool `json:"ticked"`
}

// JDImportPreview is what a cfg folder would bring.
type JDImportPreview struct {
	Token    string            `json:"token"`
	Items    []JDImportItem    `json:"items"`
	Problems []jdimport.Reason `json:"problems"`
	// Files names what was read.
	Files []string `json:"files"`
}

// JDImportFailure is an item that was ticked and could not be written.
type JDImportFailure struct {
	ID     string          `json:"id"`
	Reason jdimport.Reason `json:"reason"`
}

// JDImportReport is what an apply did.
type JDImportReport struct {
	Imported []string          `json:"imported"`
	Failed   []JDImportFailure `json:"failed"`
	// Links is how many collector rows the download list became.
	Links int `json:"links"`
	// FilterStops is set when the link filter was switched to stop at the
	// first matching rule, which JDownloader's exceptions need.
	FilterStops bool `json:"filterStops"`
}

// jdPendingTTL is how long a preview can be applied. The credentials it holds
// are dropped with it.
const jdPendingTTL = 30 * time.Minute

// jdPendingMax bounds how many previews are held at once; the oldest goes.
const jdPendingMax = 4

// ErrJDImportExpired is returned for a token that is unknown or too old.
var ErrJDImportExpired = errors.New("this preview has expired; read the folder again")

// jdPlan is a preview with everything apply needs, the credentials included.
type jdPlan struct {
	created  time.Time
	preview  JDImportPreview
	accounts map[string]jdAccountPlan
	rules    map[string]jdimport.MappedRule
	packages map[string]jdPackagePlan
	// passwords are the ones new here, in JDownloader's order.
	passwords []string
	folder    string
}

type jdAccountPlan struct {
	service string
	account string
	cred    accounts.Credential
	enabled bool
}

type jdPackagePlan struct {
	name       string
	comment    string
	password   string
	dlPassword string
	urls       []string
}

var (
	jdPendingMu sync.Mutex
	jdPending   = map[*App]map[string]*jdPlan{}
)

// ReadJDImport reads a JDownloader cfg folder and returns what it would bring.
// Nothing is written until ApplyJDImport is called with the token.
func (a *App) ReadJDImport(fsys fs.FS) (JDImportPreview, error) {
	cfg, err := jdimport.Read(fsys)
	if err != nil {
		return JDImportPreview{}, err
	}
	plan := a.planJDImport(cfg)
	token, err := newJDToken()
	if err != nil {
		return JDImportPreview{}, err
	}
	plan.preview.Token = token

	jdPendingMu.Lock()
	defer jdPendingMu.Unlock()
	held := jdPending[a]
	if held == nil {
		held = map[string]*jdPlan{}
		jdPending[a] = held
	}
	now := time.Now()
	for k, p := range held {
		if now.Sub(p.created) > jdPendingTTL {
			delete(held, k)
		}
	}
	for len(held) >= jdPendingMax {
		var oldest string
		for k, p := range held {
			if oldest == "" || p.created.Before(held[oldest].created) {
				oldest = k
			}
		}
		delete(held, oldest)
	}
	plan.created = now
	held[token] = plan
	return plan.preview, nil
}

func newJDToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// takeJDPlan removes and returns the plan held under token.
func (a *App) takeJDPlan(token string) (*jdPlan, error) {
	jdPendingMu.Lock()
	defer jdPendingMu.Unlock()
	p, ok := jdPending[a][token]
	if !ok || time.Since(p.created) > jdPendingTTL {
		delete(jdPending[a], token)
		return nil, ErrJDImportExpired
	}
	delete(jdPending[a], token)
	return p, nil
}

func (a *App) planJDImport(cfg *jdimport.Config) *jdPlan {
	p := &jdPlan{
		accounts: map[string]jdAccountPlan{},
		rules:    map[string]jdimport.MappedRule{},
		packages: map[string]jdPackagePlan{},
	}
	p.preview.Problems = append([]jdimport.Reason{}, cfg.Problems...)
	p.preview.Files = append([]string{}, cfg.Found...)
	items := []JDImportItem{}

	items = append(items, a.planJDAccounts(p, cfg.Accounts)...)

	stored := a.Settings.Get()
	if item, ok := planJDPasswords(p, cfg.ArchivePasswords, stored.ArchivePasswords); ok {
		items = append(items, item)
	}
	if dir := strings.TrimSpace(cfg.DownloadDir); dir != "" {
		p.folder = dir
		item := JDImportItem{ID: "folder", Group: JDGroupSettings, Kind: "folder", Name: dir, Detail: stored.DownloadDir}
		item.Same = stored.DownloadDir == dir
		item.Replaces = !item.Same && stored.DownloadDir != ""
		// A folder is never ticked for the user: it is a path on the machine
		// JDownloader ran on, which may not exist here.
		items = append(items, item)
	}

	items = append(items, planJDRules(p, "packagizer", JDGroupPackagizer, jdimport.MapPackagizer(cfg.Packagizer), stored.Packagizer)...)
	items = append(items, planJDRules(p, "filter", JDGroupFilter, jdimport.MapLinkFilter(cfg.LinkFilter), stored.LinkFilter)...)

	for i, pkg := range cfg.Packages {
		links := jdimport.PackageLinks(pkg)
		id := fmt.Sprintf("package:%d", i)
		item := JDImportItem{ID: id, Group: JDGroupDownloads, Kind: "package", Name: pkg.Name, Count: len(links.URLs), Notes: links.Notes}
		if len(links.URLs) == 0 {
			item.Blocked = &jdimport.Reason{Code: "packageEmpty", Text: "Nothing in this package is left to download."}
		} else {
			pw := ""
			if len(pkg.Passwords) > 0 {
				pw = pkg.Passwords[0]
			}
			p.packages[id] = jdPackagePlan{name: pkg.Name, comment: pkg.Comment, password: pw, dlPassword: pkg.DownloadPassword, urls: links.URLs}
			item.Ticked = true
		}
		items = append(items, item)
	}
	p.preview.Items = items
	return p
}

// planJDAccounts maps every account onto a debrid service of the catalogue or
// a hoster login.
func (a *App) planJDAccounts(p *jdPlan, in []jdimport.Account) []JDImportItem {
	var items []JDImportItem
	hosters := hosterauth.NewStore(a.Accounts)
	// What this import has already claimed, so two JDownloader accounts never
	// land in the same slot.
	takenHost := map[string]bool{}
	takenSlot := map[string]bool{}
	for i, acc := range in {
		id := fmt.Sprintf("account:%d", i)
		host := hostalias.Canonical(acc.Host)
		item := JDImportItem{ID: id, Group: JDGroupAccounts, Name: host, Detail: acc.User, Off: !acc.Enabled}

		if serviceID, ok := jdimport.DebridService(host); ok {
			svc, _ := accounts.Lookup(serviceID)
			item.Kind = "debrid"
			item.Name = svc.Label
			cred, ok := jdimport.DebridCredential(serviceID, acc)
			if !ok {
				item.Blocked = &jdimport.Reason{
					Code:   "noApiKey",
					Params: map[string]string{"service": svc.Label, "url": svc.WhereURL},
					Text:   fmt.Sprintf("JDownloader keeps no key for %s that KnightLoader can use. Get one at %s and add it on the Accounts page.", svc.Label, svc.WhereURL),
				}
				items = append(items, item)
				continue
			}
			slot, same := a.debridSlotFor(svc, cred, acc.User, takenSlot)
			takenSlot[serviceID+"\x00"+slot] = true
			item.Same = same
			item.Slot = slot
			if !same {
				p.accounts[id] = jdAccountPlan{service: serviceID, account: slot, cred: cred, enabled: acc.Enabled}
			}
			item.Ticked = !same && acc.Enabled
			items = append(items, item)
			continue
		}

		item.Kind = "hoster"
		switch {
		case multihostersLeftOut[host]:
			item.Blocked = &jdimport.Reason{Code: "noClient", Text: "KnightLoader has no client for this multihoster."}
		case strings.TrimSpace(acc.User) == "" && acc.Password == "":
			item.Blocked = &jdimport.Reason{Code: "noSecret", Text: "This account has neither a user name nor a password in JDownloader."}
		case takenHost[host]:
			item.Blocked = &jdimport.Reason{Code: "secondLogin", Text: "KnightLoader keeps one login per hoster, and an earlier account for this hoster is already in the list."}
		}
		if item.Blocked != nil {
			items = append(items, item)
			continue
		}
		takenHost[host] = true
		cred := accounts.Credential{Username: acc.User, Password: acc.Password}
		prev, _ := hosters.Get(host)
		item.Same = prev == cred
		item.Replaces = !item.Same && !prev.IsZero()
		if !item.Same {
			p.accounts[id] = jdAccountPlan{service: hosterauth.Service, account: host, cred: cred, enabled: acc.Enabled}
		}
		item.Ticked = !item.Same && !item.Replaces && acc.Enabled
		items = append(items, item)
	}
	return items
}

// slotChars is what an account id taken from a user name may keep.
var slotChars = regexp.MustCompile(`[^A-Za-z0-9._@-]+`)

// debridSlotFor picks the account a debrid login is stored under: the default
// one when the service has none yet, otherwise a named one, so an import never
// overwrites a debrid key that is already here. same reports a stored
// account that already holds exactly this credential.
func (a *App) debridSlotFor(svc accounts.Service, cred accounts.Credential, user string, taken map[string]bool) (slot string, same bool) {
	ids := append([]string{""}, a.Accounts.AccountIDs(svc.ID)...)
	for _, id := range ids {
		if stored, _ := a.Accounts.GetCredential(svc.ID, id); stored == cred {
			return id, true
		}
	}
	free := func(id string) bool {
		if taken[svc.ID+"\x00"+id] {
			return false
		}
		stored := a.credentialFor(svc, id)
		return stored.IsZero()
	}
	if free("") {
		return "", false
	}
	base := strings.Trim(slotChars.ReplaceAllString(strings.TrimSpace(user), "-"), "-")
	if base == "" {
		base = "jdownloader"
	}
	if len(base) > 48 {
		base = base[:48]
	}
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		if free(id) {
			return id, false
		}
	}
}

func planJDPasswords(p *jdPlan, jd, stored []string) (JDImportItem, bool) {
	have := map[string]bool{}
	for _, pw := range stored {
		have[pw] = true
	}
	total := 0
	for _, pw := range jd {
		if pw == "" {
			continue
		}
		total++
		if !have[pw] {
			have[pw] = true
			p.passwords = append(p.passwords, pw)
		}
	}
	if total == 0 {
		return JDImportItem{}, false
	}
	item := JDImportItem{ID: "passwords", Group: JDGroupSettings, Kind: "passwords", Count: len(p.passwords), Total: total}
	item.Same = len(p.passwords) == 0
	item.Ticked = !item.Same
	return item, true
}

func planJDRules(p *jdPlan, prefix, group string, mapped []jdimport.MappedRule, stored rules.Set) []JDImportItem {
	have := map[string]bool{}
	for _, r := range stored.Rules {
		if b, err := json.Marshal(r); err == nil {
			have[string(b)] = true
		}
	}
	var items []JDImportItem
	for i, m := range mapped {
		id := fmt.Sprintf("%s:%d", prefix, i)
		kind := "rule"
		if m.Accept {
			kind = "exception"
		}
		name := m.Name
		item := JDImportItem{ID: id, Group: group, Kind: kind, Name: name, Off: !m.Enabled, Blocked: m.Blocked, Notes: m.Notes}
		if m.Blocked == nil {
			if b, err := json.Marshal(m.Rule); err == nil && have[string(b)] {
				item.Same = true
			} else {
				p.rules[id] = m
				item.Ticked = true
			}
		}
		items = append(items, item)
	}
	return items
}

// JDSettingsCheck validates a settings patch before it is applied, the same
// checks a save from the settings page gets.
type JDSettingsCheck func(preview settings.Settings, patch map[string]json.RawMessage) error

// ApplyJDImport writes the ticked items of the preview held under token. The
// settings go first, so the imported rules and folder already apply to the
// links staged last; the accounts come before the links for the same reason.
func (a *App) ApplyJDImport(token string, ids []string, check JDSettingsCheck) (JDImportReport, error) {
	plan, err := a.takeJDPlan(token)
	if err != nil {
		return JDImportReport{}, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	rep := JDImportReport{Imported: []string{}, Failed: []JDImportFailure{}}
	fail := func(id string, err error) {
		rep.Failed = append(rep.Failed, JDImportFailure{ID: id, Reason: jdimport.Reason{
			Code: "writeFailed", Params: map[string]string{"error": err.Error()}, Text: err.Error(),
		}})
	}

	patch, settingIDs, stops, err := a.jdSettingsPatch(plan, want)
	if err == nil && len(patch) > 0 {
		var preview settings.Settings
		preview, err = settings.ApplyPatch(a.Settings.Get(), patch)
		if err == nil && check != nil {
			err = check(preview, patch)
		}
		if err == nil {
			_, err = a.PatchSettings(patch)
		}
	}
	for _, id := range settingIDs {
		if err != nil {
			fail(id, err)
		} else {
			rep.Imported = append(rep.Imported, id)
		}
	}
	rep.FilterStops = err == nil && stops

	hostersTouched, debridTouched := false, false
	for _, item := range plan.preview.Items {
		acc, ok := plan.accounts[item.ID]
		if !ok || !want[item.ID] {
			continue
		}
		if err := a.Accounts.SetCredential(acc.service, acc.account, acc.cred); err != nil {
			fail(item.ID, err)
			continue
		}
		a.acctHealthTracker().Reset(acc.service, acc.account)
		if !acc.enabled {
			a.setAccountEnabledQuiet(acc.service, acc.account, false)
		}
		if acc.service == hosterauth.Service {
			hostersTouched = true
		} else {
			debridTouched = true
		}
		rep.Imported = append(rep.Imported, item.ID)
	}
	if debridTouched {
		a.rewireBackends()
	}
	if hostersTouched {
		r := a.hosterAuth()
		a.spawn(func() {
			if _, err := r.Reconcile(a.ctx); err != nil {
				log.Printf("jdimport: reconcile after the import failed: %v", err)
			}
		})
	}

	for _, item := range plan.preview.Items {
		pkg, ok := plan.packages[item.ID]
		if !ok || !want[item.ID] {
			continue
		}
		created, err := a.AddLinksWithOptions(pkg.urls, pkg.name, OriginJDownloader, LinkBatchOptions{
			Password:         pkg.password,
			DownloadPassword: pkg.dlPassword,
			Comment:          pkg.comment,
			Overrule:         pkg.comment != "",
			KeepCollected:    true,
		})
		if err != nil {
			fail(item.ID, err)
			continue
		}
		rep.Links += len(created)
		rep.Imported = append(rep.Imported, item.ID)
	}
	return rep, nil
}

// jdSettingsPatch builds the one settings patch the ticked passwords, folder
// and rules make, and lists the items it carries. stops reports that the link
// filter has to stop at its first match for the imported exceptions to work.
func (a *App) jdSettingsPatch(plan *jdPlan, want map[string]bool) (map[string]json.RawMessage, []string, bool, error) {
	stored := a.Settings.Get()
	patch := map[string]json.RawMessage{}
	var ids []string
	put := func(key string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		patch[key] = b
		return nil
	}

	if want["passwords"] && len(plan.passwords) > 0 {
		next := append(slices.Clone(stored.ArchivePasswords), plan.passwords...)
		if err := put("archivePasswords", next); err != nil {
			return nil, nil, false, err
		}
		ids = append(ids, "passwords")
	}
	if want["folder"] && plan.folder != "" && plan.folder != stored.DownloadDir {
		if err := put("downloadDir", plan.folder); err != nil {
			return nil, nil, false, err
		}
		ids = append(ids, "folder")
	}

	var pkgRules []rules.Rule
	var accepts, rejects []rules.Rule
	for _, item := range plan.preview.Items {
		m, ok := plan.rules[item.ID]
		if !ok || !want[item.ID] {
			continue
		}
		switch {
		case item.Group == JDGroupPackagizer:
			pkgRules = append(pkgRules, m.Rule)
		case m.Accept:
			accepts = append(accepts, m.Rule)
		default:
			rejects = append(rejects, m.Rule)
		}
		ids = append(ids, item.ID)
	}
	if len(pkgRules) > 0 {
		set := stored.Packagizer
		set.Rules = append(slices.Clone(set.Rules), pkgRules...)
		if err := put("packagizer", set); err != nil {
			return nil, nil, false, err
		}
	}
	stops := false
	if len(accepts) > 0 || len(rejects) > 0 {
		set := stored.LinkFilter
		// JDownloader keeps a link any exception matches, whatever the filter
		// rules say. Here the first matching rule decides once the set stops
		// at its first match, so the exceptions go on top.
		var next []rules.Rule
		next = append(next, accepts...)
		next = append(next, set.Rules...)
		next = append(next, rejects...)
		set.Rules = next
		if len(accepts) > 0 && !set.StopAfterMatch {
			set.StopAfterMatch = true
			stops = true
		}
		if err := put("linkFilter", set); err != nil {
			return nil, nil, false, err
		}
	}
	return patch, ids, stops, nil
}
