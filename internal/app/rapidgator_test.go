package app

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator"
	"github.com/junkerderprovinz/knightloader/internal/resolver/rapidgator/rgtest"
)

const rgFile = "0001234422b3e37d648ea406d833c1d6"

// useRapidgator points the app's Rapidgator client at srv.
func useRapidgator(a *App, srv *rgtest.Server) {
	b := rapidgator.NewBackend(rapidgator.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a}, a.onUpdate)
	b.Checked = a.refreshPremiumHolds
	a.bmu.Lock()
	a.rapidgator = b
	a.bmu.Unlock()
	a.Registry.Register(rapidgator.Resolver{Backend: b})
	a.syncRapidgatorLogin()
}

func rgServer(t *testing.T) *rgtest.Server {
	srv := rgtest.New(t)
	srv.AddAccount(rgtest.Account{Email: "john@example.com", Password: "hunter2", Premium: true, TrafficTotal: 1 << 40, TrafficLeft: 1 << 40})
	return srv
}

func loginRow(a *App, host string) (hosterauth.LoginState, bool) {
	i := slices.IndexFunc(a.HosterLogins(), func(l hosterauth.LoginState) bool { return l.Host == host })
	if i < 0 {
		return hosterauth.LoginState{}, false
	}
	return a.HosterLogins()[i], true
}

func TestARapidgatorLoginDownloadsNativelyWhereItsJDLoginSat(t *testing.T) {
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(debrid.Resolver{ServiceID: "alldebrid", Prio: 49, Hosts: map[string]bool{"rapidgator.net": true}})
	jd.SetKnownHosts([]string{"rapidgator.net"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	srv := rgServer(t)
	useRapidgator(a, srv)
	const link = "https://rg.to/file/" + rgFile + "/a.rar.html"

	if got := chainIDs(a, link); !slices.Equal(got[:2], []string{"alldebrid", "jd"}) {
		t.Errorf("chain without a login = %v, want the debrid service, then JD's free mode", got)
	}

	if err := a.SetHosterLogin("rg.to", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{rapidgator.ResolverID, "alldebrid", "jd"}) {
		t.Errorf("chain = %v, want the login first, then the debrid service, then JD", got)
	}
	if be := a.backendFor(rapidgator.ResolverID); be == a.Engine {
		t.Error("Rapidgator tasks were routed to the engine, which would save the file page")
	}
	a.mu.Lock()
	mode := a.modeForLocked(&core.Task{URL: link}, rapidgator.ResolverID)
	a.mu.Unlock()
	if mode != core.ModePremium {
		t.Errorf("mode = %q, want premium", mode)
	}

	// The login's own row on the priority card stands for the client.
	rows := a.ResolverPriority("")
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	if slices.Contains(ids, rapidgator.ResolverID) || !slices.Contains(ids, loginRowID("rg.to")) {
		t.Errorf("the card shows %v, want the login's row and no second one for the client", ids)
	}
	if _, err := a.SaveResolverOrder([]string{"alldebrid", loginRowID("rg.to"), "jd"}); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{"alldebrid", rapidgator.ResolverID, "jd"}) {
		t.Errorf("chain = %v, want the order the card was saved in", got)
	}
	if _, err := a.SaveResolverOrder([]string{"jd", "alldebrid"}); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); got[0] != rapidgator.ResolverID || got[1] != "jd" {
		t.Errorf("chain = %v, want a login the order does not name just above JD", got)
	}
	if ids := priorityIDs(a); slices.Index(ids, loginRowID("rg.to")) != slices.Index(ids, "jd")-1 {
		t.Errorf("the card shows %v, want the login right above JD, where its links go", ids)
	}

	waitFor(t, "Rapidgator's word on the login", func() bool {
		row, ok := loginRow(a, "rg.to")
		return ok && row.Status == hosterauth.StatusActive && row.Tier == "premium" && row.TrafficMax == 1<<40
	})
	if jd.HostActive("rapidgator.net") {
		t.Error("routing counts the login as JD's")
	}

	if err := a.RemoveHosterLogin("rg.to"); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); slices.Contains(got, rapidgator.ResolverID) {
		t.Errorf("chain after the login was removed = %v, want no Rapidgator client", got)
	}
}

func priorityIDs(a *App) []string {
	var ids []string
	for _, r := range a.ResolverPriority("") {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestARapidgatorFileIsNamedInTheCollectorAndDownloaded(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	srv := rgServer(t)
	a := newCrawlApp(t, false)
	useRapidgator(a, srv)
	if err := a.SetHosterLogin("rapidgator.net", "john@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("isle of dogs "), 8192)
	link := srv.Add(rgFile, rgtest.File{Name: "Isle of Dogs.mkv", Body: body})

	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 || created[0].Resolver != rapidgator.ResolverID {
		t.Fatalf("staged %+v, want one row for the Rapidgator client", created)
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

func TestARapidgatorLinkWithoutALoginIsNamedFromItsPage(t *testing.T) {
	srv := rgServer(t)
	a := newCrawlApp(t, false)
	a.Registry.Register(jd.Resolver{})
	jd.SetKnownHosts([]string{"rapidgator.net"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	useRapidgator(a, srv)
	live := srv.Add(rgFile, rgtest.File{Name: "Isle of Dogs.mkv", Body: make([]byte, 700<<10)})

	created := a.AddLinks([]string{live, "https://rapidgator.net/file/ffffffffffffffffffffffffffffffff"}, "")
	if len(created) != 2 || created[0].Resolver != "jd" {
		t.Fatalf("staged %+v, want both rows with JD's free mode", created)
	}
	waitFor(t, "the pages naming the file and finding the other gone", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		named, gone := a.tasks[created[0].ID], a.tasks[created[1].ID]
		return named.Name == "Isle of Dogs.mkv" && named.Size == 700<<10 && named.Online == core.AvailOnline &&
			gone.Online == core.AvailOffline
	})
	if slices.ContainsFunc(srv.Requests(), func(r string) bool { return r == "POST /api/v2/user/login" }) {
		t.Error("the API was asked without a login")
	}
}
