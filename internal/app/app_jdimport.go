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

// ErrJDImportExpired is returned for a token whose preview is too old.
var ErrJDImportExpired = errors.New("this preview has expired; read the folder again")

// ErrJDImportUnknown is returned for a token this process does not hold: one
// already applied, one read before a restart, or one dropped after expiring.
var ErrJDImportUnknown = errors.New("this preview is no longer held here, for example after a restart; read the folder again")

// ErrJDImportReplaced is returned for a token whose preview newer reads pushed
// out.
var ErrJDImportReplaced = fmt.Errorf("only the %d newest previews are kept, and newer reads pushed this one out; read the folder again", jdPendingMax)

// jdPlan is a preview with everything apply needs, the credentials included.
type jdPlan struct {
	created time.Time
	// seq orders the previews by when they were read; created can tie on a
	// coarse clock.
	seq      uint64
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
	name    string
	comment string
	// passwords are the package's archive passwords; the first goes on its
	// links and every one into the global list.
	passwords  []string
	dlPassword string
	urls       []string
}

var (
	jdPendingMu sync.Mutex
	jdPending   = map[*App]map[string]*jdPlan{}
	// jdReplaced holds when each pushed-out preview was read, until it would
	// have expired anyway, so its token is refused for the right reason.
	jdReplaced = map[*App]map[string]time.Time{}
	jdReads    uint64
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
	replaced := jdReplaced[a]
	if replaced == nil {
		replaced = map[string]time.Time{}
		jdReplaced[a] = replaced
	}
	now := time.Now()
	for k, p := range held {
		if now.Sub(p.created) > jdPendingTTL {
			delete(held, k)
		}
	}
	for k, created := range replaced {
		if now.Sub(created) > jdPendingTTL {
			delete(replaced, k)
		}
	}
	for len(held) >= jdPendingMax {
		var oldest string
		for k, p := range held {
			if oldest == "" || p.seq < held[oldest].seq {
				oldest = k
			}
		}
		replaced[oldest] = held[oldest].created
		delete(held, oldest)
	}
	plan.created = now
	jdReads++
	plan.seq = jdReads
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
	if !ok {
		// The marker stays until it ages out, so a second Apply from the same
		// dialog gets the same answer.
		created, gone := jdReplaced[a][token]
		switch {
		case !gone:
			return nil, ErrJDImportUnknown
		case time.Since(created) > jdPendingTTL:
			return nil, ErrJDImportExpired
		}
		return nil, ErrJDImportReplaced
	}
	if time.Since(p.created) > jdPendingTTL {
		// Emptied rather than removed, so the credentials go at once and a
		// second Apply still hears that the preview is too old.
		jdPending[a][token] = &jdPlan{created: p.created}
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
			p.packages[id] = jdPackagePlan{name: pkg.Name, comment: pkg.Comment, passwords: pkg.Passwords, dlPassword: pkg.DownloadPassword, urls: links.URLs}
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
	keep := hosterLoginsToKeep(in)
	// What this import has already claimed, so two debrid accounts never land
	// in the same slot.
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
		item.Blocked = hosterLoginBlocked(host, acc)
		if item.Blocked == nil && keep[host] != i {
			item.Blocked = &jdimport.Reason{Code: "secondLogin", Text: "KnightLoader keeps one login per hoster, and another account for this hoster comes over instead."}
		}
		if item.Blocked != nil {
			items = append(items, item)
			continue
		}
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

// hosterLoginsToKeep picks, per hoster, the one account that can come over:
// the first one switched on in JDownloader, or the first one at all when every
// account of that hoster is off.
func hosterLoginsToKeep(in []jdimport.Account) map[string]int {
	keep := map[string]int{}
	on := map[string]bool{}
	for i, acc := range in {
		host := hostalias.Canonical(acc.Host)
		if _, debrid := jdimport.DebridService(host); debrid || hosterLoginBlocked(host, acc) != nil {
			continue
		}
		if _, seen := keep[host]; !seen || (acc.Enabled && !on[host]) {
			keep[host] = i
			on[host] = acc.Enabled
		}
	}
	return keep
}

// hosterLoginBlocked says why a hoster account cannot come over at all.
func hosterLoginBlocked(host string, acc jdimport.Account) *jdimport.Reason {
	switch {
	case multihostersLeftOut[host]:
		return &jdimport.Reason{Code: "noClient", Text: "KnightLoader has no client for this multihoster."}
	case strings.TrimSpace(acc.User) == "" && acc.Password == "":
		return &jdimport.Reason{Code: "noSecret", Text: "This account has neither a user name nor a password in JDownloader."}
	}
	return nil
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
				item.Ticked = m.Enabled
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
	if err == nil {
		err = a.applyJDPatch(patch, check)
	}
	for _, id := range settingIDs {
		if err != nil {
			fail(id, err)
		} else {
			rep.Imported = append(rep.Imported, id)
		}
	}
	rep.FilterStops = err == nil && stops

	// The folder is a path on JDownloader's machine, so it goes on its own and
	// a path that does not fit here fails only itself.
	if want["folder"] && plan.folder != "" && plan.folder != a.Settings.Get().DownloadDir {
		dir, _ := json.Marshal(plan.folder)
		if err := a.applyJDPatch(map[string]json.RawMessage{"downloadDir": dir}, check); err != nil {
			fail("folder", err)
		} else {
			rep.Imported = append(rep.Imported, "folder")
		}
	}

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
		first := ""
		if len(pkg.passwords) > 0 {
			first = pkg.passwords[0]
		}
		created, err := a.AddLinksWithOptions(pkg.urls, pkg.name, OriginJDownloader, LinkBatchOptions{
			Password:         first,
			DownloadPassword: pkg.dlPassword,
			Comment:          pkg.comment,
			Overrule:         pkg.comment != "",
			KeepCollected:    true,
		})
		if err != nil {
			fail(item.ID, err)
			continue
		}
		if len(created) > 0 {
			a.rememberPasswords(pkg.passwords)
		}
		rep.Links += len(created)
		rep.Imported = append(rep.Imported, item.ID)
	}
	return rep, nil
}

// jdSettingsPatch builds the one settings patch the ticked passwords and rules
// make, and lists the items it carries. stops reports that the link
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

// applyJDPatch writes a settings patch after the same checks a save from the
// settings page gets.
func (a *App) applyJDPatch(patch map[string]json.RawMessage, check JDSettingsCheck) error {
	if len(patch) == 0 {
		return nil
	}
	preview, err := settings.ApplyPatch(a.Settings.Get(), patch)
	if err == nil && check != nil {
		err = check(preview, patch)
	}
	if err == nil {
		_, err = a.PatchSettings(patch)
	}
	return err
}
