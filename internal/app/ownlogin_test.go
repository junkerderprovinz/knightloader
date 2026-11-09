package app

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer"
	"github.com/junkerderprovinz/knightloader/internal/resolver/filer/filertest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share"
	"github.com/junkerderprovinz/knightloader/internal/resolver/keep2share/k2stest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare"
	"github.com/junkerderprovinz/knightloader/internal/resolver/nitroflare/nftest"
)

// useOwnHoster puts h in place of the app's hoster of the same id. The free
// client of the hoster it replaces goes too, since it speaks to the live
// site.
func useOwnHoster(a *App, h *ownHoster) {
	own := a.ownHosters(engineHandoff{a.Engine, a})
	a.bmu.Lock()
	// A fresh slice, as the app reads the old one without the lock.
	a.own = slices.Clone(own)
	for i := range a.own {
		if a.own[i].id != h.id {
			continue
		}
		if old := a.own[i].free; old != nil {
			a.Registry.Unregister(old.id)
		}
		a.own[i] = h
	}
	a.bmu.Unlock()
	a.Registry.Register(h.res)
	if h.free != nil {
		a.Registry.Register(h.free.res)
	}
	a.syncOwnLogins()
}

const k2sFile = "2cfa1607527dc"

// useKeep2Share points the app's Keep2Share client at srv.
func useKeep2Share(a *App, srv *k2stest.Server) {
	h := keep2share.Keep2Share
	b := keep2share.NewBackend(h, keep2share.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	b.Checked, b.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	useOwnHoster(a, &ownHoster{id: h.ID, site: h.Site, normalize: h.Normalize, be: b, res: keep2share.Resolver{Backend: b}})
}

func k2sServer(t *testing.T, captcha bool) *k2stest.Server {
	srv := k2stest.New(t, "k2s.cc")
	srv.AddAccount(k2stest.Account{Email: "john@example.com", Password: "hunter2", Traffic: 50 << 30,
		Expires: time.Now().Add(30 * 24 * time.Hour), Captcha: captcha})
	return srv
}

const nfFile = "CDB87F04326E3EE"

// useNitroflare points the app's Nitroflare client at srv.
func useNitroflare(a *App, srv *nftest.Server) {
	b := nitroflare.NewBackend(nitroflare.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	b.Checked, b.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	useOwnHoster(a, &ownHoster{id: nitroflare.ResolverID, site: nitroflare.Site, normalize: nitroflare.Normalize,
		be: b, res: nitroflare.Resolver{Backend: b}})
}

func nfServer(t *testing.T) *nftest.Server {
	srv := nftest.New(t)
	srv.AddAccount(nftest.Account{Email: "john@example.com", Password: "hunter2", TrafficMax: 100 << 30, TrafficLeft: 80 << 30,
		Expires: time.Now().Add(30 * 24 * time.Hour)})
	return srv
}

func TestAKeep2ShareLoginDownloadsNativelyWhereItsJDLoginSat(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(debrid.Resolver{ServiceID: "alldebrid", Prio: 49, Hosts: map[string]bool{"keep2share.cc": true, "fileboom.me": true}})
	jd.SetKnownHosts([]string{"keep2share.cc", "fileboom.me"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	useKeep2Share(a, k2sServer(t, false))
	const link = "https://k2s.cc/file/" + k2sFile + "/a.rar"
	const boom = "https://fboom.me/file/" + k2sFile

	if got := chainIDs(a, link); !slices.Equal(got[:2], []string{"alldebrid", "jd"}) {
		t.Errorf("chain without a login = %v, want the debrid service, then JD's free mode", got)
	}

	if err := a.SetHosterLogin("k2s.cc", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{keep2share.Keep2Share.ID, "alldebrid", "jd"}) {
		t.Errorf("chain = %v, want the login first, then the debrid service, then JD", got)
	}
	if got := chainIDs(a, boom); slices.Contains(got, keep2share.FileBoom.ID) || slices.Contains(got, keep2share.Keep2Share.ID) {
		t.Errorf("a FileBoom link went to %v, without a FileBoom login", got)
	}
	a.mu.Lock()
	mode := a.modeForLocked(&core.Task{URL: link}, keep2share.Keep2Share.ID)
	a.mu.Unlock()
	if mode != core.ModePremium {
		t.Errorf("mode = %q, want premium", mode)
	}

	if _, err := a.SaveResolverOrder([]string{"alldebrid", loginRowID("k2s.cc"), "jd"}); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{"alldebrid", keep2share.Keep2Share.ID, "jd"}) {
		t.Errorf("chain = %v, want the order the card was saved in", got)
	}
	if _, err := a.SaveResolverOrder([]string{"jd", "alldebrid"}); err != nil {
		t.Fatal(err)
	}
	if ids := priorityIDs(a); slices.Contains(ids, keep2share.Keep2Share.ID) || slices.Index(ids, loginRowID("k2s.cc")) != slices.Index(ids, "jd")-1 {
		t.Errorf("the card shows %v, want the login's row right above JD, where its links go, and no row for the client", ids)
	}

	waitFor(t, "Keep2Share's word on the login", func() bool {
		row, ok := loginRow(a, "k2s.cc")
		return ok && row.Status == hosterauth.StatusActive && row.Tier == "premium" && row.TrafficLeft == 50<<30
	})
	if jd.HostActive("keep2share.cc") {
		t.Error("routing counts the login as JD's")
	}
	if !a.HonoursCollisionPolicy(keep2share.Keep2Share.ID) || !a.HonoursCollisionPolicy(nitroflare.ResolverID) {
		t.Error("the files the engine writes for the new hosters skip the collision policy")
	}
}

func TestACaptchaAtSignInShowsOnTheLoginsRow(t *testing.T) {
	a := newCrawlApp(t, false)
	useKeep2Share(a, k2sServer(t, true))
	if err := a.SetHosterLogin("keep2share.cc", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the row asking for the captcha", func() bool {
		row, ok := loginRow(a, "keep2share.cc")
		return ok && row.Status == hosterauth.StatusRejected && row.Code == "ownCaptcha"
	})
}

func TestKeep2ShareAndNitroflareLinksAreNamedInTheCollectorWithoutALogin(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts([]string{"keep2share.cc", "nitroflare.com"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	k2s, nf := k2sServer(t, false), nfServer(t)
	useKeep2Share(a, k2s)
	useNitroflare(a, nf)
	k2sLink := k2s.Add(k2sFile, k2stest.File{Name: "Holiday.part1.rar", Body: make([]byte, 700<<10)})
	nfLink := nf.Add(nfFile, nftest.File{Name: "Holiday.part2.rar", Body: make([]byte, 300<<10)})

	created := a.AddLinks([]string{k2sLink, nfLink, "https://nitroflare.com/view/A1B2C3D4E5F6071"}, "")
	if len(created) != 3 || created[0].Resolver != "jd" || created[1].Resolver != "jd" {
		t.Fatalf("staged %+v, want every row with JD's free mode", created)
	}
	waitFor(t, "the APIs naming the files and finding the third gone", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		k, n, gone := a.tasks[created[0].ID], a.tasks[created[1].ID], a.tasks[created[2].ID]
		return k.Name == "Holiday.part1.rar" && k.Size == 700<<10 && k.Online == core.AvailOnline &&
			n.Name == "Holiday.part2.rar" && n.Size == 300<<10 && n.Online == core.AvailOnline &&
			gone.Online == core.AvailOffline
	})
	if slices.Contains(k2s.Requests(), "POST /api/v2/login") || slices.Contains(nf.Requests(), "GET /api/v2/getKeyInfo") {
		t.Error("a login was used where none is stored")
	}
}

func TestANitroflareFileIsNamedInTheCollectorAndDownloaded(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	srv := nfServer(t)
	a := newCrawlApp(t, false)
	useNitroflare(a, srv)
	if err := a.SetHosterLogin("nitroflare.com", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("isle of dogs "), 8192)
	link := srv.Add(nfFile, nftest.File{Name: "Isle of Dogs.mkv", Body: body})

	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 || created[0].Resolver != nitroflare.ResolverID {
		t.Fatalf("staged %+v, want one row for the Nitroflare client", created)
	}
	id := created[0].ID
	waitFor(t, "the API naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Name == "Isle of Dogs.mkv" && task.Size == int64(len(body)) && task.Online == core.AvailOnline
	})

	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	var file string
	waitFor(t, "the download finishing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		if task.Status == core.StatusError {
			t.Fatalf("the download failed: %s", task.Error)
		}
		file = task.File
		return task.Status == core.StatusDone
	})
	got, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("%s does not hold the file (%v)", file, err)
	}
}

func TestALoginTheOwnClientCannotUseGoesToJDAndComesBackWhenSavedAgain(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts([]string{"keep2share.cc"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	srv := k2sServer(t, false)
	srv.Fail("login", http.StatusBadRequest, 99, "Something the API never said before", 0)
	useKeep2Share(a, srv)
	const link = "https://k2s.cc/file/" + k2sFile + "/a.rar"

	if err := a.SetHosterLogin("k2s.cc", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the row saying JD has the login", func() bool {
		row, ok := loginRow(a, "k2s.cc")
		return ok && row.HandedOver && row.Enabled
	})
	if got := chainIDs(a, link); slices.Contains(got, keep2share.Keep2Share.ID) {
		t.Errorf("chain = %v, want the links gone from KnightLoader's client", got)
	}
	for _, r := range a.ResolverPriority("") {
		if r.ID == loginRowID("k2s.cc") && r.OwnLogin {
			t.Error("the card marks the login as KnightLoader's own")
		}
	}

	if err := a.SetHosterLogin("k2s.cc", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the login back with KnightLoader", func() bool {
		row, ok := loginRow(a, "k2s.cc")
		return ok && !row.HandedOver && row.Status == hosterauth.StatusActive && row.Tier == "premium"
	})
	if got := chainIDs(a, link); len(got) == 0 || got[0] != keep2share.Keep2Share.ID {
		t.Errorf("chain = %v, want KnightLoader's client first again", got)
	}
}

func TestADownloadTheOwnClientGaveUpOnMovesToTheNextBackendNamingTheTakeover(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts([]string{"keep2share.cc"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	task := &core.Task{ID: "1", URL: "https://k2s.cc/file/" + k2sFile, Resolver: keep2share.Keep2Share.ID, Status: core.StatusRunning, Enabled: true}
	// Halted by hand, so the task moved on starts nowhere: no JD is set up here,
	// and the hoster's clients speak to the live site. The schedule's first pass
	// may still be due, and it keeps only a halt set by hand.
	a.SetHalted(true)
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.active[task.ID] = true
	a.started[task.ID] = true
	a.mu.Unlock()

	a.onUpdate(task.ID, core.Update{Status: core.StatusError, Err: "Keep2Share: the answer does not read",
		Unsupported: true, Takeover: "Keep2Share"})
	a.mu.Lock()
	resolverID, takeover, failed := task.Resolver, task.Takeover, task.Status == core.StatusError
	a.mu.Unlock()
	if resolverID != "jd" || takeover != "Keep2Share" || failed {
		t.Errorf("task on %q with takeover %q, failed %v; want it with JD, naming Keep2Share", resolverID, takeover, failed)
	}

	a.mu.Lock()
	a.active[task.ID] = true
	a.mu.Unlock()
	a.onUpdate(task.ID, core.Update{Status: core.StatusDone})
	a.mu.Lock()
	takeover = task.Takeover
	a.mu.Unlock()
	if takeover != "" {
		t.Errorf("a finished download still names the takeover %q", takeover)
	}
}

func TestAFilerLoginDownloadsNativelyAndItsLinksAreNamedWithoutOne(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts([]string{"filer.net"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	srv := filertest.New(t)
	srv.AddAccount(filertest.Account{Email: "john@example.com", Password: "hunter2", Premium: true,
		Until: time.Now().AddDate(0, 1, 0), Traffic: 125 << 30, TrafficLeft: 100 << 30})
	b := filer.NewBackend(filer.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	b.Checked, b.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	useOwnHoster(a, &ownHoster{id: filer.ResolverID, site: filer.Site, normalize: filer.Normalize, be: b, res: filer.Resolver{Backend: b}})
	link := srv.Add("dv6qg8tfckhk7mja", filertest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 500<<10)})

	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 || created[0].Resolver != "jd" {
		t.Fatalf("staged %+v, want the row with JD's free mode", created)
	}
	waitFor(t, "the API naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[created[0].ID]
		return task.Name == "Isle of Dogs.mkv" && task.Size == 500<<10 && task.Online == core.AvailOnline
	})

	if err := a.SetHosterLogin("filer.net", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); len(got) < 2 || got[0] != filer.ResolverID || got[1] != "jd" {
		t.Errorf("chain = %v, want the login first, then JD", got)
	}
	waitFor(t, "filer.net's word on the login", func() bool {
		row, ok := loginRow(a, "filer.net")
		return ok && row.Status == hosterauth.StatusActive && row.Tier == "premium" && row.TrafficLeft == 100<<30
	})
	if !a.HonoursCollisionPolicy(filer.ResolverID) {
		t.Error("the file the engine writes for filer.net skips the collision policy")
	}
}

func TestAFreeFilerLinkGoesToTheFreeClientBelowTheDebridServicesAndAboveJD(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(debrid.Resolver{ServiceID: "alldebrid", Prio: 49, Hosts: map[string]bool{"filer.net": true}})
	jd.SetKnownHosts([]string{"filer.net"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	srv := filertest.New(t)
	srv.AddAccount(filertest.Account{Email: "john@example.com", Password: "hunter2", Premium: true,
		Until: time.Now().AddDate(0, 1, 0), Traffic: 125 << 30, TrafficLeft: 100 << 30})
	b := filer.NewBackend(filer.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	b.Checked, b.Moved = a.refreshPremiumHolds, a.ownLoginMoved
	fb := filer.NewFreeBackend(filer.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	useOwnHoster(a, &ownHoster{id: filer.ResolverID, site: filer.Site, normalize: filer.Normalize, be: b, res: filer.Resolver{Backend: b},
		free: a.newOwnFree(filer.FreeResolverID, filer.Site, filer.FreeResolver{}, fb.Free, fb, b.Handed)})
	const link = "https://filer.net/get/dv6qg8tfckhk7mja"

	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{"alldebrid", filer.FreeResolverID, "jd"}) {
		t.Errorf("chain without a login = %v, want the debrid service, the free client, then JD", got)
	}
	a.mu.Lock()
	mode := a.modeForLocked(&core.Task{URL: link}, filer.FreeResolverID)
	a.mu.Unlock()
	if mode != core.ModeFree {
		t.Errorf("mode = %q, want free", mode)
	}
	if !slices.Contains(priorityIDs(a), filer.FreeResolverID) {
		t.Errorf("the card shows %v, want a row for the free client", priorityIDs(a))
	}
	if a.backendFor(filer.FreeResolverID) != backend(fb) || !a.HonoursCollisionPolicy(filer.FreeResolverID) {
		t.Error("the free client's tasks do not reach its backend")
	}

	if err := a.SetHosterLogin("filer.net", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); len(got) < 3 || got[0] != filer.ResolverID || got[1] != "alldebrid" || got[2] != filer.FreeResolverID {
		t.Errorf("chain = %v, want the login first, then the debrid service and the free client", got)
	}

	b.Fail(errors.New("filer.net: something new"), core.ReasonUnknown)
	if got := chainIDs(a, link); slices.Contains(got, filer.FreeResolverID) || slices.Contains(got, filer.ResolverID) {
		t.Errorf("chain = %v, want both clients standing aside while JD has the login", got)
	}
}
