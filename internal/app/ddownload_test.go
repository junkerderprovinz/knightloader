package app

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ddownload"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ddownload/ddltest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// useDdownload points the app's ddownload client at srv.
func useDdownload(a *App, srv *ddltest.Server) *ddownload.Backend {
	b := a.newDdownloadBackend(ddownload.NewClient(srv.URL, srv.Client()), engineHandoff{a.Engine, a})
	a.bmu.Lock()
	a.ddl = b
	a.bmu.Unlock()
	a.Registry.Register(ddownload.Resolver{Client: b.Client(), Premium: b.Premium})
	return b
}

// answerTurnstile waits for the Turnstile check ddownload shows on pageURL to
// reach the captcha prompts, answers it there and returns it.
func answerTurnstile(t *testing.T, a *App, pageURL string) captcha.Challenge {
	t.Helper()
	var c captcha.Challenge
	waitFor(t, "the captcha reaching the prompts", func() bool {
		for _, ch := range a.CaptchaChallenges() {
			if w, ok := ch.Payload.(*captcha.WidgetPayload); ok && w.SiteURL == pageURL {
				c = ch
				return true
			}
		}
		return false
	})
	w := c.Payload.(*captcha.WidgetPayload)
	if c.Host != ddownload.Site || c.Kind != captcha.KindWidget || w.Vendor != captcha.VendorTurnstile || w.SiteKey != ddltest.SiteKey {
		t.Errorf("the prompt got %+v with %+v, want ddownload's Turnstile check", c, w)
	}
	got, err := a.AnswerCaptcha(context.Background(), c.ID, ddltest.Token)
	if err != nil || !got.StillValid {
		t.Fatalf("answering the captcha = %+v, %v", got, err)
	}
	return c
}

func TestADdownloadLinkGoesFreeBelowDebridAndOnAPremiumLoginAboveIt(t *testing.T) {
	srv := ddltest.New(t)
	a := newCrawlApp(t, false)
	b := useDdownload(a, srv)
	a.Registry.Register(jd.Resolver{})
	a.Registry.Register(debrid.Resolver{ServiceID: "zevera", Prio: 43, Hosts: map[string]bool{"ddownload.com": true}})
	jd.SetKnownHosts([]string{"ddownload.com"})
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	const link = "https://ddl.to/k3m9x2p7q1ab/a.bin"
	mode := func() core.DownloadMode {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.modeForLocked(&core.Task{URL: link}, ddownload.ResolverID)
	}

	if got := chainIDs(a, link); !slices.Equal(got[:3], []string{"zevera", ddownload.ResolverID, "jd"}) {
		t.Errorf("chain = %v, want the debrid service, then the ddownload client, then JD", got)
	}
	if mode() != core.ModeFree {
		t.Errorf("mode = %q without a login, want free", mode())
	}
	if !a.HonoursCollisionPolicy(ddownload.ResolverID) {
		t.Error("the collision policy does not reach a ddownload download")
	}

	srv.AddAccount("fox", ddltest.Account{Password: "badger", Premium: true, Until: time.Now().AddDate(1, 0, 0), TrafficMB: 150000})
	if err := a.SetHosterLogin("ddl.to", "fox", "badger"); err != nil {
		t.Fatal(err)
	}
	if c := answerTurnstile(t, a, srv.URL+"/login"); c.TaskID != "" {
		t.Errorf("the sign-in's captcha names task %q, want none", c.TaskID)
	}
	var row hosterauth.LoginState
	waitFor(t, "the login checked", func() bool {
		for _, l := range a.HosterLogins() {
			row = l
		}
		return row.Status == hosterauth.StatusActive
	})
	if row.Host != "ddl.to" || row.Tier != "premium" || row.Expiry == "" || row.TrafficMax != 200000e6 {
		t.Errorf("the login's row = %+v, want it premium with its expiry and traffic", row)
	}
	if !b.Premium() || mode() != core.ModePremium {
		t.Errorf("premium %v, mode %q; want the login used", b.Premium(), mode())
	}
	if got := chainIDs(a, link); got[0] != ddownload.ResolverID {
		t.Errorf("chain = %v, want the ddownload client on the premium login first", got)
	}
	if rows := a.ResolverPriority(""); !slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == "login:ddl.to" }) ||
		!slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == ddownload.ResolverID }) {
		t.Errorf("the priority card = %v, want rows for the login and the free client", rows)
	}

	// A hand-arranged order that puts the debrid service above the login
	// sends the link there.
	if _, err := a.SaveResolverOrder([]string{"zevera", "login:ddl.to", ddownload.ResolverID, "jd"}); err != nil {
		t.Fatal(err)
	}
	if got := chainIDs(a, link); !slices.Equal(got[:2], []string{"zevera", ddownload.ResolverID}) {
		t.Errorf("chain = %v, want the order's debrid service first", got)
	}

	if err := a.SetHosterLoginEnabled("ddl.to", false); err != nil {
		t.Fatal(err)
	}
	if b.Premium() || mode() != core.ModeFree {
		t.Error("a switched-off login is still used")
	}
}

func TestADdownloadFileIsNamedInTheCollectorAndDownloadedForFree(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	srv := ddltest.New(t)
	a := newCrawlApp(t, false)
	useDdownload(a, srv)
	body := bytes.Repeat([]byte("fantastic mr fox "), 8192)
	body = body[:len(body)/1024*1024]
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "Fantastic Mr Fox.mkv", Body: body})

	created := a.AddLinks([]string{link}, "")
	if len(created) != 1 || created[0].Resolver != ddownload.ResolverID {
		t.Fatalf("staged %+v, want one row for the ddownload client", created)
	}
	id := created[0].ID
	waitFor(t, "the page naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Name == "Fantastic Mr Fox.mkv" && task.Size == int64(len(body)) && task.Online == core.AvailOnline
	})

	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	if c := answerTurnstile(t, a, srv.URL+"/k3m9x2p7q1ab"); c.TaskID != id {
		t.Errorf("the captcha names task %q, want %q", c.TaskID, id)
	}
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

func TestADdownloadWaitKeepsTheTaskRunningAndIsNoStall(t *testing.T) {
	srv := ddltest.New(t)
	a := newCrawlApp(t, false)
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), StallTimeout: settings.MinStallTimeout}); err != nil {
		t.Fatal(err)
	}
	useDdownload(a, srv)
	link := srv.Add("k3m9x2p7q1ab", ddltest.File{Name: "a.bin", Body: make([]byte, 4096)})
	srv.Wait(5700)

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	t.Cleanup(func() { a.RemoveTasks([]string{id}, true) })
	waitFor(t, "the wait showing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Status == core.StatusRunning && strings.Contains(task.Note, "1 h 35 min")
	})

	a.stallPass(time.Now())
	a.stallPass(time.Now().Add(time.Hour))

	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.tasks[id]
	if !task.StalledSince.IsZero() {
		t.Error("a task sitting out ddownload's wait was marked as stalled")
	}
	if task.Status != core.StatusRunning || task.Error != "" {
		t.Errorf("status %q, error %q; want it running without a failure", task.Status, task.Error)
	}
}
