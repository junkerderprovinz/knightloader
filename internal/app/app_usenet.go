package app

// Usenet through a debrid service. An .nzb from Sonarr or Radarr, the upload
// button or a drop folder goes to a TorBox or Premiumize.me account
// (internal/usenet). Once the service has fetched and unpacked it, each file
// becomes an ordinary task in the NZB's package, which the engine downloads
// into that package's folder.

import (
	"errors"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
)

// ErrNZBNeedsUsenet is a real .nzb on an instance with no account that can
// fetch it from Usenet. It is refused rather than read for links: the only
// address in it is its XML namespace.
var ErrNZBNeedsUsenet = errors.New(
	"an .nzb is fetched from Usenet through a TorBox or Premiumize.me account, and none here can do that")

// usenetState is one App's NZB queue and the backend its files download
// through.
type usenetState struct {
	jobs  *usenet.Manager
	files *usenet.Files
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
			Finished: a.tasksFinished,
			Failed:   a.usenetJobFailed,
			Pending:  func(n int) { a.setActivityGauge(ActivityUsenet, n) },
			Taken:    a.claimUsenetJob,
		})
		st.files = usenet.NewFiles(engineHandoff{a.Engine, a}, st.jobs.Service, a.onUpdate)
		usenetReg[a] = st
	}
	return st
}

// rewireUsenet offers NZBs to the stored TorBox accounts and then the
// Premiumize.me ones, TorBox first since Usenet is part of its plans. It runs
// with every rewireBackends.
func (a *App) rewireUsenet() {
	// It claims only the links a finished job was staged under, so it is
	// registered whether or not an account is set up.
	a.Registry.Register(usenet.Resolver{})
	st := a.usenetStateFor()
	usenetMu.Lock()
	fixed := st.fixed
	usenetMu.Unlock()
	if fixed {
		return
	}
	var services []usenet.Service
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
	a.setUsenetServices(st, services)
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
	Origin   core.Origin
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

// UsenetJobFolder is where a job's files land, or would have: the folder its
// package and category point at.
func (a *App) UsenetJobFolder(j usenet.Job) string {
	return a.dirFor(&core.Task{Package: j.Package, Category: j.Category, Name: j.Name, CreatedAt: j.Added})
}

// stageUsenetFiles stages a finished job's files through the ordinary path, so
// the filter, the Packagizer and the duplicate check see them like any link. A
// file in a folder of the download keeps that folder under the package's.
//
// The files at the top come first, since the SABnzbd bridge reports the folder
// of a grab's first task as the one Sonarr imports from.
func (a *App) stageUsenetFiles(j usenet.Job, files []usenet.File) ([]string, error) {
	files = append([]usenet.File(nil), files...)
	sort.SliceStable(files, func(x, y int) bool { return files[x].Dir == "" && files[y].Dir != "" })
	links := make([]resolver.Result, 0, len(files))
	dirOf := map[string]string{}
	for _, f := range files {
		u := usenet.FileLink(j.Service, j.Remote, f)
		links = append(links, resolver.Result{DirectURL: u, Name: f.Name, Size: f.Size})
		dirOf[u] = f.Dir
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
	a.keepUsenetFolders(created, dirOf)

	ids := make([]string, 0, len(links))
	for _, l := range links {
		if id := have[l.DirectURL]; id != "" {
			ids = append(ids, id)
		}
	}
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

// tasksFinished reports whether every task is done or gone, after which the
// service's copy of a job can be deleted.
func (a *App) tasksFinished(ids []string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		if t := a.tasks[id]; t != nil && t.Status != core.StatusDone {
			return false
		}
	}
	return true
}
