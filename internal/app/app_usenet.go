package app

// Usenet. An .nzb from Sonarr or Radarr, the upload button or a drop folder
// goes to the first account on the priority card that takes it
// (internal/usenet): the user's own Usenet servers or a TorBox or
// Premiumize.me account. A service fetches and unpacks it first, and each
// file it hands back becomes an ordinary task the engine downloads. With the
// own servers each file of the .nzb is a task at once, whose articles
// internal/usenet/local fetches; a job they cannot complete goes on to the
// next account.

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/nzb"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
)

// ErrNZBNeedsUsenet is a real .nzb on an instance with nothing that can fetch
// it from Usenet. It is refused rather than read for links: the only address
// in it is its XML namespace.
var ErrNZBNeedsUsenet = errors.New(
	"an .nzb needs a Usenet server, or a TorBox or Premiumize.me account, and none is set up here")

// usenetState is one App's NZB queue and the backends its files download
// through.
type usenetState struct {
	jobs  *usenet.Manager
	files *usenet.Files
	// own keeps the .nzb of each job the own servers took, and articles
	// downloads their files.
	own      *local.Service
	articles *local.Backend
	// fixed is set once SetUsenetServices has put services in place, which
	// rewireUsenet then leaves alone.
	fixed bool
}

var (
	usenetMu  sync.Mutex
	usenetReg = map[*App]*usenetState{}
)

// usenetStateFor returns this App's queue, building it on first use.
func (a *App) usenetStateFor() *usenetState {
	usenetMu.Lock()
	defer usenetMu.Unlock()
	st, ok := usenetReg[a]
	if !ok {
		st = &usenetState{}
		st.jobs = usenet.New(usenet.Options{
			Dir:      filepath.Join(a.DataDir, "usenet"),
			Stage:    a.stageUsenetFiles,
			Finished: a.jobFinished,
			Done:     a.usenetDone,
			Failed:   a.usenetJobFailed,
			Pending:  func(n int) { a.setActivityGauge(ActivityUsenet, n) },
			Taken:    a.claimUsenetJob,
		})
		st.files = usenet.NewFiles(engineHandoff{a.Engine, a}, st.jobs.Service, a.onUpdate)
		st.own = local.NewService(filepath.Join(a.DataDir, "usenet", "own"))
		st.articles = local.NewBackend(st.own, a.nntpClient, a.dlDir, a.onUpdate)
		st.articles.Dir = a.taskDir
		st.articles.Incomplete = a.usenetIncomplete
		usenetReg[a] = st
	}
	return st
}

// rewireUsenet offers NZBs to the own Usenet servers, the stored TorBox
// accounts and the Premiumize.me ones, in the order the priority card ranks
// the three. It runs with every rewireBackends and every saved settings
// document, since the card and the servers are both kept there.
func (a *App) rewireUsenet() {
	// Both claim only the links a job's files were staged under, so they are
	// registered whether or not anything is set up. The own servers' resolver
	// is also their row on the priority card, so it is there only with a
	// server switched on.
	a.Registry.Register(usenet.Resolver{})
	ownServers := a.usenetServersSet()
	if ownServers {
		a.Registry.Register(local.Resolver{})
	} else {
		a.Registry.Unregister(local.ResolverID)
	}
	st := a.usenetStateFor()
	usenetMu.Lock()
	fixed := st.fixed
	usenetMu.Unlock()
	if fixed {
		return
	}
	var services []usenet.Service
	if ownServers {
		services = append(services, st.own)
	}
	for _, acct := range a.routedAccounts("torbox") {
		if acct.cred.APIKey != "" {
			services = append(services, usenet.NewTorBox(usenet.TorBoxAPI, resolver.SlotID("torbox", acct.account), acct.cred.APIKey))
		}
	}
	for _, acct := range a.routedAccounts("premiumize") {
		if acct.cred.APIKey != "" {
			services = append(services, usenet.NewPremiumize(usenet.PremiumizeAPI, resolver.SlotID("premiumize", acct.account), acct.cred.APIKey))
		}
	}
	order := a.Settings.Get().ResolverOrder
	slices.SortStableFunc(services, func(x, y usenet.Service) int {
		return usenetRank(y.Slot(), order) - usenetRank(x.Slot(), order)
	})
	a.setUsenetServices(st, services)
}

// usenetRank is where the priority card puts the service behind slot, ranked
// as dynamicPrio ranks the resolvers: a hand-arranged order first, the
// resolvers' own priorities otherwise, the own servers above TorBox above
// Premiumize.me.
func usenetRank(slot string, order []string) int {
	service, _ := resolver.SplitSlot(slot)
	if i := slices.Index(order, service); i >= 0 {
		return orderBase - i
	}
	switch service {
	case local.ResolverID:
		return local.Resolver{}.Info().Prio
	case "torbox":
		return torbox.Resolver{}.Info().Prio
	}
	for _, s := range debridServices {
		if s.id == service {
			return s.prio
		}
	}
	return 0
}

// SetUsenetServices puts services in place of the ones built from the stored
// accounts, and keeps them there when the accounts change. It is how a test
// hands the app a service of its own.
func (a *App) SetUsenetServices(services ...usenet.Service) {
	st := a.usenetStateFor()
	usenetMu.Lock()
	st.fixed = true
	usenetMu.Unlock()
	a.setUsenetServices(st, services)
}

// OwnUsenetServers is the account the own Usenet servers are in the queue, for
// a test that hands SetUsenetServices a list with them in it.
func (a *App) OwnUsenetServers() usenet.Service {
	return a.usenetStateFor().own
}

// setUsenetServices hands the queue its accounts. When the first account able
// to take an NZB arrives, the drop folders look again at the files they left
// lying for want of one.
func (a *App) setUsenetServices(st *usenetState, services []usenet.Service) {
	_, before := st.jobs.Available()
	st.jobs.SetServices(services)
	if _, after := st.jobs.Available(); after && !before {
		a.retryWatchFiles()
	}
}

// startUsenet works the queue until Close. It runs once the task list is
// whole, since a finished job stages tasks.
func (a *App) startUsenet() {
	jobs := a.usenetStateFor().jobs
	a.spawn(func() { jobs.Run(a.ctx) })
	a.spawn(func() {
		<-a.ctx.Done()
		nntpMu.Lock()
		if c := nntpClients[a]; c != nil {
			c.c.Close()
			delete(nntpClients, a)
		}
		nntpMu.Unlock()
	})
}

// UsenetService names the service an NZB would be offered to first, and
// reports false when no account can take one.
func (a *App) UsenetService() (string, bool) {
	return a.usenetStateFor().jobs.Available()
}

// NZB is one .nzb for AddNZB.
type NZB struct {
	// Name is the release, which the NZB goes to the service under.
	Name string
	Data []byte
	// Package is the package its files are staged in, Name when empty.
	Package string
	// Category is the id of the category its files are filed in.
	Category string
	// Dir is the folder its files download into, the package's when empty.
	Dir    string
	Origin core.Origin
	// Start queues the files as soon as they are staged, as a grab from Sonarr
	// does. Otherwise auto-confirm decides, as for a paste.
	Start bool
}

// AddNZB queues an NZB for the first account that takes it. It fails with
// usenet.ErrNoService when no account can take one.
func (a *App) AddNZB(n NZB) (usenet.Job, error) {
	name := strings.TrimSpace(n.Name)
	pkg := strings.TrimSpace(n.Package)
	if pkg == "" {
		pkg = name
	}
	return a.usenetStateFor().jobs.Add(usenet.Job{
		Name:     name,
		Package:  pkg,
		Category: n.Category,
		Dir:      n.Dir,
		Origin:   string(n.Origin),
		Start:    n.Start,
	}, n.Data)
}

// UsenetJob returns one NZB as it stands.
func (a *App) UsenetJob(id string) (usenet.Job, bool) {
	return a.usenetStateFor().jobs.Get(id)
}

// CancelUsenetJob drops an NZB whose files are not tasks yet, at the service
// too. For one whose files are tasks already it returns their ids, which the
// caller removes like any others.
func (a *App) CancelUsenetJob(id string) []string {
	return a.usenetStateFor().jobs.Cancel(id)
}

// stageUsenetFiles stages a finished job's files through the ordinary path, so
// the filter, the Packagizer and the duplicate check see them like any link. A
// file in a folder of the download keeps that folder under the package's, or
// under the job's own folder when it has one.
func (a *App) stageUsenetFiles(j usenet.Job, files []usenet.File) ([]string, error) {
	kept := a.keptUsenetFiles(j.Kept)
	links := make([]resolver.Result, 0, len(files))
	dirOf := map[string]string{}
	held := map[string]bool{}
	for _, f := range files {
		if _, ok := kept[f.Name]; ok {
			continue
		}
		u := f.Link
		if u == "" {
			u = usenet.FileLink(j.Service, j.Remote, f)
		}
		links = append(links, resolver.Result{DirectURL: u, Name: f.Name, Size: f.Size})
		dirOf[u] = f.Dir
		held[u] = f.Held
	}

	// A restart between staging and the job list's save offers the same files
	// again; the tasks they already became are taken as they are.
	have := a.taskIDsByURL(links)
	var fresh []resolver.Result
	for _, l := range links {
		if have[l.DirectURL] == "" {
			fresh = append(fresh, l)
		}
	}
	origin, ok := KnownOrigin(j.Origin)
	if !ok {
		origin = OriginPaste
	}
	created := a.stageResolvedLinks(fresh, intake{pkg: j.Package, origin: origin, category: j.Category, jobLinks: true})
	for _, t := range created {
		have[t.URL] = t.ID
	}
	if j.Dir != "" {
		// Before anything starts them, and before keepUsenetFolders, which
		// builds on the folder.
		if err := a.SetTaskOptions(idsOf(created), TaskOptions{Dir: &j.Dir}); err != nil {
			a.RemoveTasks(idsOf(created), false)
			return nil, err
		}
	}
	a.keepUsenetFolders(created, dirOf)

	ids := make([]string, 0, len(links)+len(kept))
	for _, l := range links {
		if id := have[l.DirectURL]; id != "" {
			ids = append(ids, id)
		}
	}
	for _, id := range kept {
		ids = append(ids, id)
	}
	var off []string
	for _, t := range created {
		if held[t.URL] {
			off = append(off, t.ID)
		}
	}
	a.SetEnabled(off, false)
	started := idsOf(created)
	if j.Start {
		// StartTasks rather than by hand, so a queue halted by hand stays
		// halted, as for every other grab.
		a.StartTasks(started)
	} else {
		a.autoConfirm(started)
	}
	log.Printf("usenet: %s is ready at %s; %d of its %d files staged", j.Name, j.Label, len(ids), len(files))
	return ids, nil
}

// keptUsenetFiles maps the file name of each task in ids that is still done
// to its id. They are the files of a job handed on that the own servers had
// finished.
func (a *App) keptUsenetFiles(ids []string) map[string]string {
	out := map[string]string{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		if t := a.tasks[id]; t != nil && t.Status == core.StatusDone {
			if name := local.LinkName(t.URL); name != "" {
				out[name] = id
			}
		}
	}
	return out
}

// usenetDone picks the tasks among ids whose files are downloaded.
func (a *App) usenetDone(ids []string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, id := range ids {
		if t := a.tasks[id]; t != nil && t.Status == core.StatusDone {
			out = append(out, id)
		}
	}
	return out
}

// taskIDsByURL finds the tasks already in the list for links, keyed by link.
func (a *App) taskIDsByURL(links []resolver.Result) map[string]string {
	want := make(map[string]bool, len(links))
	for _, l := range links {
		want[l.DirectURL] = true
	}
	out := make(map[string]string, len(links))
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		if want[t.URL] {
			out[t.URL] = t.ID
		}
	}
	return out
}

// keepUsenetFolders puts each file that sat in a folder of the download into
// the same folder under its task's own, so files of one name in different
// folders do not land on each other.
func (a *App) keepUsenetFolders(created []*core.Task, dirOf map[string]string) {
	for _, t := range created {
		sub := dirOf[t.URL]
		if sub == "" {
			continue
		}
		parts := strings.Split(sub, "/")
		for i, p := range parts {
			parts[i] = sanitizeSegment(p)
		}
		dir := filepath.Join(append([]string{a.TaskFolder(t.ID)}, parts...)...)
		if err := a.SetTaskOptions([]string{t.ID}, TaskOptions{Dir: &dir}); err != nil {
			log.Printf("usenet: %s stays in its package's folder: %v", t.Name, err)
		}
	}
}

// usenetJobFailed puts a job that ended without files beside the other links
// that did not make it, so an .nzb from the upload button or a drop folder
// does not fail where nobody looks. Sonarr also reads it from its history.
func (a *App) usenetJobFailed(j usenet.Job) {
	a.recordSkippedReason(j.Name+".nzb", "nzb", j.Reason)
}

// jobFinished reports whether the service's copy of a job can be deleted:
// every task is done or gone, and no undo can bring a removed one back. The
// own servers' .nzb stays while any task is listed, since a restart or a held
// recovery volume is fetched from it.
func (a *App) jobFinished(j usenet.Job) bool {
	own := j.Service == local.ResolverID
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for _, id := range j.TaskIDs {
		t := a.tasks[id]
		switch {
		case t == nil:
			if a.undoableUntilLocked(id).After(now) {
				return false
			}
		case own, t.Status != core.StatusDone:
			return false
		}
	}
	return true
}

// nzbGoneLocked reports whether t is a file of the own Usenet servers whose
// .nzb they no longer keep, as after its job went on to a debrid service.
// Fetching it again would start by deleting the file it has. Caller holds
// a.mu.
func (a *App) nzbGoneLocked(t *core.Task) bool {
	return local.Resolver{}.Match(t.URL) && !a.usenetStateFor().own.Keeps(t.URL)
}

// HeldSpare reports whether t is a par2 recovery volume from the own Usenet
// servers that is held back, switched off until a repair needs it. It is not
// a download that failed or waits, so nothing that sums up a job counts it.
func HeldSpare(t *core.Task) bool {
	return !t.Enabled && t.Status != core.StatusDone && strings.HasPrefix(t.URL, local.ResolverID+"://") && nzb.IsRecoveryVolume(t.Name)
}

// usenetIncomplete hands a job some of whose articles no own server has to the
// next account, and removes the tasks it had become, files and all, but for
// the finished ones: their files stay, and the next account's copies of them
// are not staged. It reports false when no other account is left, and the
// file then fails.
func (a *App) usenetIncomplete(job string, missing int) bool {
	reason := fmt.Sprintf("%d articles are on none of your Usenet servers", missing)
	if missing == 1 {
		reason = "1 article is on none of your Usenet servers"
	}
	ids, ok := a.usenetStateFor().jobs.Fallback(local.ResolverID, job, reason)
	if !ok {
		return false
	}
	// Apart from the caller, which is one of these tasks' downloads and
	// returns once it has its answer.
	a.spawn(func() { a.RemoveTasks(ids, true) })
	return true
}
