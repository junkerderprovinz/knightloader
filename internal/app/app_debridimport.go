package app

// The import from a debrid account. What is added to the account outside this
// instance comes into the collector like any other link, staged under a
// debrid.JobLink, so it is fetched from that account and never added anywhere
// again. That is mostly what the user adds on the service's website, but the
// service does not say who added a download, so another app's comes in too.
// debrid.AccountImport decides what is new; this file decides which
// accounts are followed, keeps what each one remembers in debrid_imports.json,
// and deletes an imported download on the service once its task is gone.

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
)

// importPoll is how often an account's list is read. It is far inside every
// service's limit: Real-Debrid allows 250 calls a minute and AllDebrid 600,
// and TorBox, with a list for each kind of download, gets three calls.
//
// importDropDelay is how long a removed download waits before it is deleted
// on the service. It is the undo window, since an undone removal brings the
// task back to fetch from the same download.
//
// Both are variables so tests need not wait them out.
var (
	importPoll      = time.Minute
	importDropDelay = UndoWindow
)

// accountImports is one App's import from its accounts. Its zero value is
// ready to use.
type accountImports struct {
	mu sync.Mutex
	// started is set once the task list is whole, before which nothing polls.
	started bool
	// accounts holds what each account's import remembers, by slot, made on
	// first use.
	accounts map[string]*debrid.AccountImport
	// polling stops the loop following each account, by slot.
	polling map[string]context.CancelFunc
	// dropping is every removed task whose download is waiting to be deleted
	// on the service, so a wait taken up again is never run twice.
	dropping map[string]bool
	// fileMu serialises read-modify-writes of debrid_imports.json and
	// debrid_drops.json.
	fileMu sync.Mutex
}

// startAccountImports follows the accounts whose import is on. It runs once
// the mirror set is seeded, so a download staged from an earlier poll is
// recognised.
func (a *App) startAccountImports() {
	a.imports.mu.Lock()
	a.imports.started = true
	a.imports.mu.Unlock()
	a.applyAccountImports()
	a.resumeImportDrops()
}

// applyAccountImports follows every account whose import is on and whose
// service can list its downloads, and stops following the rest. It runs on
// every rewire and every switch of an account's import, and the lock is taken
// before the accounts are read, so a call that read an older state cannot
// finish last.
func (a *App) applyAccountImports() {
	a.imports.mu.Lock()
	defer a.imports.mu.Unlock()
	if !a.imports.started {
		return
	}
	want := map[string]bool{}
	for slot := range a.importListers() {
		if a.importOn(resolver.SplitSlot(slot)) {
			want[slot] = true
		}
	}
	for slot, stop := range a.imports.polling {
		if !want[slot] {
			stop()
			delete(a.imports.polling, slot)
		}
	}
	for slot := range want {
		if a.imports.polling[slot] != nil {
			continue
		}
		if a.imports.polling == nil {
			a.imports.polling = map[string]context.CancelFunc{}
		}
		ctx, stop := context.WithCancel(a.ctx)
		a.imports.polling[slot] = stop
		imp := a.accountImportLocked(slot)
		a.spawn(func() { a.followAccount(ctx, slot, imp) })
	}
}

// followAccount polls one account until ctx ends. The lister is looked up for
// every poll, since a rewire builds a new one. A read that fails is logged
// when it starts failing and when it works again, not once a minute.
func (a *App) followAccount(ctx context.Context, slot string, imp *debrid.AccountImport) {
	failing := false
	for {
		if l := a.importListers()[slot]; l != nil {
			err := imp.Poll(ctx, l)
			switch {
			case ctx.Err() != nil:
				return
			case err != nil && !failing:
				log.Printf("%s: the import could not read the downloads on the account: %v", slotLabel(slot), err)
			case err == nil && failing:
				log.Printf("%s: the import reads the downloads on the account again", slotLabel(slot))
			}
			failing = err != nil
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(importPoll):
		}
	}
}

// importListers is every wired account whose service can list its downloads,
// by slot.
func (a *App) importListers() map[string]debrid.Lister {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	out := map[string]debrid.Lister{}
	for slot, be := range a.debrid {
		if tb, ok := be.(*debrid.TorrentBackend); ok {
			if l, ok := tb.Service().(debrid.Lister); ok {
				out[slot] = l
			}
		}
	}
	return out
}

// importService is the service an imported download is deleted through, or
// nil once its account is gone.
func (a *App) importService(slot string) debrid.TorrentService {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	if tb, ok := a.debrid[slot].(*debrid.TorrentBackend); ok {
		return tb.Service()
	}
	return nil
}

func (a *App) accountImport(slot string) *debrid.AccountImport {
	a.imports.mu.Lock()
	defer a.imports.mu.Unlock()
	return a.accountImportLocked(slot)
}

func (a *App) accountImportLocked(slot string) *debrid.AccountImport {
	if imp := a.imports.accounts[slot]; imp != nil {
		return imp
	}
	imp := &debrid.AccountImport{
		Load:  func() debrid.ImportRecord { return a.loadImportRecord(slot) },
		Save:  func(r debrid.ImportRecord) { a.saveImportRecord(slot, r) },
		Known: a.importKnown,
		Hand:  func(j debrid.Listed) { a.stageImported(slot, j) },
	}
	if a.imports.accounts == nil {
		a.imports.accounts = map[string]*debrid.AccountImport{}
	}
	a.imports.accounts[slot] = imp
	return imp
}

// stageImported stages one download added to the account outside this
// instance. It goes through the ordinary path, so the filter, the Packagizer,
// the duplicate check and auto-confirm treat it like any link.
func (a *App) stageImported(slot string, j debrid.Listed) {
	name := strings.TrimSpace(j.Name)
	if name == "" {
		name = j.ID
	}
	link := resolver.Result{DirectURL: debrid.JobLink(slot, j.ID), Name: name, Size: j.Size}
	if len(a.addResolvedLinksFrom([]resolver.Result{link}, intake{origin: OriginAccount, jobLinks: true})) > 0 {
		log.Printf("%s: imported %s from the account", slotLabel(slot), name)
	}
}

// importKnown reports whether a download on an account is in the list by
// another way in: a torrent not yet done with the same info hash, such as one
// this instance is still adding to that account, or one whose add failed after
// the service had taken it.
func (a *App) importKnown(j debrid.Listed) bool {
	if j.Hash == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		if t.Status != core.StatusDone && strings.EqualFold(t.InfoHash, j.Hash) {
			return true
		}
	}
	return false
}

// importedTask reports whether t fetches a download imported from a debrid
// account.
func importedTask(t *core.Task) bool {
	_, _, ok := debrid.ParseJobLink(t.URL)
	return ok
}

// claimImported records a job this instance added to an account, so the
// import never takes it for one the user added there.
func (a *App) claimImported(slot, job string) {
	a.accountImport(slot).Claim(job)
}

// claimUsenetJob is claimImported for an .nzb the Usenet queue sent to an
// account, which TorBox lists apart from its torrents.
func (a *App) claimUsenetJob(slot, remote string) {
	if service, _ := resolver.SplitSlot(slot); service == "torbox" {
		remote = torbox.UsenetJob(remote)
	}
	a.claimImported(slot, remote)
}

// importDrop is an imported download waiting out the undo window before it is
// deleted on the service.
type importDrop struct {
	Task string    `json:"task"`
	Slot string    `json:"slot"`
	Job  string    `json:"job"`
	Due  time.Time `json:"due"`
}

// dropImported deletes an imported download on the service once its task is
// gone for good, unless downloads are kept there. A finished task's download
// was dealt with when it finished, and one the link filter held was never a
// download here at all. The wait is noted in debrid_drops.json, so a restart
// inside it does not leave the download on the account.
func (a *App) dropImported(t *core.Task) {
	slot, job, ok := debrid.ParseJobLink(t.URL)
	if !ok || t.Status == core.StatusDone || t.Skipped || a.Settings.Get().Torrent.KeepOnService {
		return
	}
	d := importDrop{Task: t.ID, Slot: slot, Job: job, Due: time.Now().Add(importDropDelay)}
	a.editImportDrops(func(m map[string]importDrop) { m[d.Task] = d })
	a.awaitImportDrop(d)
}

// resumeImportDrops takes up the waits a restart cut short.
func (a *App) resumeImportDrops() {
	a.imports.fileMu.Lock()
	m := a.loadImportDropsLocked()
	a.imports.fileMu.Unlock()
	for _, d := range m {
		a.awaitImportDrop(d)
	}
}

// awaitImportDrop deletes one imported download once its wait is over, unless
// its task has come back. A wait that shutdown cuts short stays noted for the
// next start, and so does one whose account is not wired when it ends.
func (a *App) awaitImportDrop(d importDrop) {
	a.imports.mu.Lock()
	if a.imports.dropping[d.Task] {
		a.imports.mu.Unlock()
		return
	}
	if a.imports.dropping == nil {
		a.imports.dropping = map[string]bool{}
	}
	a.imports.dropping[d.Task] = true
	a.imports.mu.Unlock()
	a.spawn(func() {
		defer func() {
			a.imports.mu.Lock()
			delete(a.imports.dropping, d.Task)
			a.imports.mu.Unlock()
		}()
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(time.Until(d.Due)):
		}
		a.mu.Lock()
		back := a.tasks[d.Task] != nil
		a.mu.Unlock()
		if back {
			a.editImportDrops(func(m map[string]importDrop) { delete(m, d.Task) })
			return
		}
		svc := a.importService(d.Slot)
		if svc == nil {
			return
		}
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		if err := svc.DeleteTorrent(ctx, d.Job); err != nil {
			if a.ctx.Err() != nil {
				return
			}
			log.Printf("%s: could not delete %s from the account: %v", slotLabel(d.Slot), d.Job, err)
		}
		a.editImportDrops(func(m map[string]importDrop) { delete(m, d.Task) })
	})
}

// importDropsPath is beside debrid_imports.json.
func (a *App) importDropsPath() string {
	return filepath.Join(filepath.Dir(a.dlDir), "debrid_drops.json")
}

// loadImportDropsLocked reads the noted waits by task. A damaged file reads as
// empty. Caller holds a.imports.fileMu.
func (a *App) loadImportDropsLocked() map[string]importDrop {
	m := map[string]importDrop{}
	b, err := os.ReadFile(a.importDropsPath())
	if err != nil {
		return m
	}
	if json.Unmarshal(b, &m) != nil {
		return map[string]importDrop{}
	}
	return m
}

// editImportDrops changes the noted waits and writes them back.
func (a *App) editImportDrops(edit func(map[string]importDrop)) {
	a.imports.fileMu.Lock()
	defer a.imports.fileMu.Unlock()
	m := a.loadImportDropsLocked()
	edit(m)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(a.importDropsPath(), b, 0o600); err != nil {
		log.Printf("could not note which imported downloads are to be deleted: %v", err)
	}
}

// SetAccountImport switches the import from one account on or off. Either
// way it starts afresh the next time it is on, taking what is on the account
// then as already there.
func (a *App) SetAccountImport(service, account string, on bool) {
	acctMetaMu.Lock()
	m := a.loadAcctMetaLocked()
	key := metaKey(service, account)
	meta, ok := m[key]
	if !ok {
		// An account without metadata routes, and gaining some must not
		// switch it off.
		meta.Enabled = true
	}
	was := meta.Import
	meta.Import = on
	m[key] = meta
	a.saveAcctMetaLocked(m)
	acctMetaMu.Unlock()
	if was != on {
		a.forgetAccountImport(service, account)
	}
	a.applyAccountImports()
}

// importOn reports whether the import from one account is switched on.
func (a *App) importOn(service, account string) bool {
	acctMetaMu.Lock()
	defer acctMetaMu.Unlock()
	return a.loadAcctMetaLocked()[metaKey(service, account)].Import
}

// forgetAccountImport drops what the import from one account remembers, as
// when its credential changes and the account may be another one.
func (a *App) forgetAccountImport(service, account string) {
	a.accountImport(resolver.SlotID(service, account)).Forget()
}

// fillImport says on an account's row whether its downloads can be imported
// and whether they are.
func (a *App) fillImport(st *AccountState, cred accounts.Credential) {
	st.CanImport = importable(st.Service, cred)
	st.Import = st.CanImport && a.importOn(st.Service, st.Account)
}

// importable reports whether a service's client can list the downloads on an
// account: TorBox's, and those debrid clients that implement debrid.Lister.
func importable(service string, cred accounts.Credential) bool {
	if service == "torbox" {
		return true
	}
	_, ok := newDebridClient(service, cred).(debrid.Lister)
	return ok
}

// slotLabel names an account in a log line: the service, and the account id
// for a further one.
func slotLabel(slot string) string {
	service, account := resolver.SplitSlot(slot)
	label := service
	if svc, ok := accounts.Lookup(service); ok {
		label = svc.Label
	}
	return label + accountSuffix(account)
}

// importRecordsPath is beside account_meta.json, since every record belongs
// to an account.
func (a *App) importRecordsPath() string {
	return filepath.Join(filepath.Dir(a.dlDir), "debrid_imports.json")
}

// loadImportRecordsLocked reads every account's record. A damaged file reads
// as empty, which only means the next poll takes what is on each account as
// already there. Caller holds a.imports.fileMu.
func (a *App) loadImportRecordsLocked() map[string]debrid.ImportRecord {
	m := map[string]debrid.ImportRecord{}
	b, err := os.ReadFile(a.importRecordsPath())
	if err != nil {
		return m
	}
	if json.Unmarshal(b, &m) != nil {
		return map[string]debrid.ImportRecord{}
	}
	return m
}

func (a *App) loadImportRecord(slot string) debrid.ImportRecord {
	a.imports.fileMu.Lock()
	defer a.imports.fileMu.Unlock()
	return a.loadImportRecordsLocked()[slot]
}

func (a *App) saveImportRecord(slot string, r debrid.ImportRecord) {
	a.imports.fileMu.Lock()
	defer a.imports.fileMu.Unlock()
	m := a.loadImportRecordsLocked()
	if r.Since.IsZero() {
		delete(m, slot)
	} else {
		m[slot] = r
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(a.importRecordsPath(), b, 0o600); err != nil {
		log.Printf("%s: the import could not note what it has seen: %v", slotLabel(slot), err)
	}
}
