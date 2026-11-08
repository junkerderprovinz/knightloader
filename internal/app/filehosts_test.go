package app

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/captcha"
	"github.com/junkerderprovinz/knightloader/internal/confirm"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/gofile"
	"github.com/junkerderprovinz/knightloader/internal/resolver/gofile/gftest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/krakenfiles"
	"github.com/junkerderprovinz/knightloader/internal/resolver/krakenfiles/kftest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mediafire"
	"github.com/junkerderprovinz/knightloader/internal/resolver/mediafire/mftest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/pixeldrain"
	"github.com/junkerderprovinz/knightloader/internal/resolver/pixeldrain/pdtest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/workupload"
	"github.com/junkerderprovinz/knightloader/internal/resolver/workupload/wutest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// fakeHosters are the stand-ins the app's clients for Pixeldrain, Gofile,
// Mediafire, Krakenfiles and Workupload are pointed at.
type fakeHosters struct {
	pd *pdtest.Server
	gf *gftest.Server
	mf *mftest.Server
	kf *kftest.Server
	wu *wutest.Server
}

func useFakeHosters(t *testing.T, a *App) fakeHosters {
	f := fakeHosters{pd: pdtest.New(t), gf: gftest.New(t), mf: mftest.New(t), kf: kftest.New(t), wu: wutest.New(t)}
	eng := engineHandoff{a.Engine, a}
	pd := pixeldrain.NewClient(f.pd.URL, f.pd.Client())
	pd.Key = func() string { return a.hosterSecret("pixeldrain.com") }
	gf := gofile.NewClient(f.gf.API, f.gf.Site, f.gf.Client())
	gf.Token = func() string { return a.hosterSecret("gofile.io") }
	mf := mediafire.NewClient(f.mf.URL, f.mf.Client())
	kf := krakenfiles.NewClient(f.kf.URL, f.kf.Client())
	wu := workupload.NewClient(f.wu.URL, f.wu.Client())
	a.bmu.Lock()
	a.hosters = map[string]hosterClient{
		pixeldrain.ResolverID:  {pixeldrain.Resolver{Client: pd}, a.hosterBackend("Pixeldrain", "pixeldrain.com", pd, eng)},
		gofile.ResolverID:      {gofile.Resolver{Client: gf}, a.hosterBackend("Gofile", "gofile.io", gf, eng)},
		mediafire.ResolverID:   {mediafire.Resolver{Client: mf}, a.hosterBackend("Mediafire", "mediafire.com", mf, eng)},
		krakenfiles.ResolverID: {krakenfiles.Resolver{Client: kf}, a.hosterBackend("Krakenfiles", "krakenfiles.com", kf, eng)},
		workupload.ResolverID:  {workupload.Resolver{Client: wu}, a.hosterBackend("Workupload", "workupload.com", wu, eng)},
	}
	hosters := a.hosters
	a.bmu.Unlock()
	for _, h := range hosters {
		a.Registry.Register(h.res)
	}
	return f
}

func TestTheOwnHosterClientsGoBelowDebridAndAJDLoginAndAboveJDsFreeMode(t *testing.T) {
	for host, c := range map[string]struct{ id, link string }{
		"pixeldrain.com":  {pixeldrain.ResolverID, "https://pixeldrain.com/u/iniGVSnw"},
		"gofile.io":       {gofile.ResolverID, "https://gofile.io/d/AMZyDw"},
		"mediafire.com":   {mediafire.ResolverID, "https://www.mediafire.com/file/wt7dc5e9jgnym04/Poses.tar.gz/file"},
		"krakenfiles.com": {krakenfiles.ResolverID, "https://krakenfiles.com/view/DP3nGKJNsX/file.html"},
		"workupload.com":  {workupload.ResolverID, "https://workupload.com/file/QpDxHzUJcL8"},
	} {
		t.Run(host, func(t *testing.T) {
			a := newCrawlApp(t, false)
			useFakeHosters(t, a)
			a.Registry.Register(jd.Resolver{})
			// The smaller multihosters share the lowest debrid priority.
			a.Registry.Register(debrid.Resolver{ServiceID: "zevera", Prio: 43, Hosts: map[string]bool{host: true}})
			jd.SetKnownHosts([]string{host})
			t.Cleanup(func() { jd.SetKnownHosts(nil) })

			if got := chainIDs(a, c.link); !slices.Equal(got[:3], []string{"zevera", c.id, "jd"}) {
				t.Errorf("chain = %v, want the debrid service, then our own client, then JD", got)
			}
			if _, ok := a.backendFor(c.id).(*hoster.Backend); !ok {
				t.Errorf("%s tasks go to %T, which would save the hoster's page", c.id, a.backendFor(c.id))
			}
			if !a.HonoursCollisionPolicy(c.id) {
				t.Error("the collision policy is said not to reach the file the engine writes")
			}
			a.mu.Lock()
			mode := a.modeForLocked(&core.Task{URL: c.link}, c.id)
			a.mu.Unlock()
			if mode != core.ModeFree {
				t.Errorf("mode = %q, want free", mode)
			}
			if rows := a.ResolverPriority(""); !slices.ContainsFunc(rows, func(r resolver.Info) bool { return r.ID == c.id }) {
				t.Errorf("the priority card has no row for %s: %v", c.id, rows)
			}

			jd.SetHostActive(host, true)
			t.Cleanup(func() { jd.SetHostActive(host, false) })
			if got := chainIDs(a, c.link); got[0] != "jd" {
				t.Errorf("chain = %v, want JD with the login first", got)
			}
		})
	}
}

// staged waits for the collector to hold n rows and returns them.
func staged(t *testing.T, a *App, created []*core.Task, n int) []*core.Task {
	t.Helper()
	if len(created) != n {
		names := make([]string, 0, len(created))
		for _, c := range created {
			names = append(names, c.URL)
		}
		t.Fatalf("staged %v, want %d rows", names, n)
	}
	return created
}

// downloaded confirms the task and returns the file it ends up in.
func downloaded(t *testing.T, a *App, id string) []byte {
	t.Helper()
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
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAPixeldrainListBecomesOneRowPerFileAndAFileDownloads(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeHosters(t, a)
	body := bytes.Repeat([]byte("rating list "), 4096)
	f.pd.AddFile("iniGVSnw", pdtest.File{Name: "2001-2019.zip", Body: body})
	f.pd.AddFile("BvnwvXuV", pdtest.File{Name: "2020-2023.zip", Body: []byte("PK")})
	f.pd.AddList("CcFZTyZ4", pdtest.List{Title: "FIDE rating-list archive", Files: []string{"iniGVSnw", "BvnwvXuV"}})

	rows := staged(t, a, a.AddLinks([]string{"https://pixeldrain.com/l/CcFZTyZ4"}, ""), 2)
	if rows[0].Resolver != pixeldrain.ResolverID || rows[0].Name != "2001-2019.zip" || rows[0].Size != int64(len(body)) {
		t.Errorf("first row %+v, want the list's first file on our own client", rows[0])
	}
	if got := downloaded(t, a, rows[0].ID); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what Pixeldrain served")
	}
}

func TestAProtectedGofileFolderOpensWithTheBatchsDownloadPassword(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeHosters(t, a)
	body := bytes.Repeat([]byte("isle of dogs "), 4096)
	f.gf.Add("f1", "", gftest.Item{Name: "Isle of Dogs.mkv", Body: body})
	f.gf.Add("f2", "", gftest.Item{Name: "Isle of Dogs.srt", Body: []byte("1")})
	f.gf.Add("sub", "", gftest.Item{Name: "Subs", Children: []string{"f2"}})
	link := f.gf.Add("root", "AMZyDw", gftest.Item{Name: "Release", Children: []string{"f1", "sub"}, Password: "hunter2"})

	created, err := a.AddLinksWithOptions([]string{link}, "", OriginPaste, LinkBatchOptions{DownloadPassword: "hunter2", KeepCollected: true})
	if err != nil {
		t.Fatal(err)
	}
	rows := staged(t, a, created, 2)
	byName := map[string]*core.Task{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	mkv := byName["Isle of Dogs.mkv"]
	if mkv == nil || byName["Isle of Dogs.srt"] == nil || mkv.Resolver != gofile.ResolverID {
		t.Fatalf("staged %+v, want the film and its subtitles on our own client", rows)
	}
	if got := downloaded(t, a, mkv.ID); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what Gofile served")
	}
}

func TestAMediafireLimitKeepsTheTaskRunningWithTheWaitAndIsNoStall(t *testing.T) {
	a := newCrawlApp(t, false)
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: t.TempDir(), StallTimeout: settings.MinStallTimeout}); err != nil {
		t.Fatal(err)
	}
	f := useFakeHosters(t, a)
	link := f.mf.AddFile("wt7dc5e9jgnym04", mftest.File{Name: "Poses.tar.gz", Body: make([]byte, 4096), Page: mftest.PageLimit})

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	waitFor(t, "the collector naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.tasks[id].Name == "Poses.tar.gz" && a.tasks[id].Online == core.AvailOnline
	})
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	t.Cleanup(func() { a.RemoveTasks([]string{id}, true) })
	waitFor(t, "the wait showing", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		task := a.tasks[id]
		return task.Status == core.StatusRunning && strings.Contains(task.Note, "Mediafire limits free downloads") && strings.Contains(task.Note, "13 min")
	})

	a.stallPass(time.Now())
	a.stallPass(time.Now().Add(time.Hour))

	a.mu.Lock()
	defer a.mu.Unlock()
	if task := a.tasks[id]; !task.StalledSince.IsZero() || task.Status != core.StatusRunning {
		t.Errorf("status %q, stalled since %v; want it running and not stalled", task.Status, task.StalledSince)
	}
}

func TestAStoredLoginLendsItsKeyUnderAnyOfTheHostersDomainsWhileSwitchedOn(t *testing.T) {
	a := newCrawlApp(t, false)
	if err := hosterauth.NewStore(a.Accounts).Set("pixeldrain.net", accounts.Credential{Username: "reader", Password: pdtest.Key}); err != nil {
		t.Fatal(err)
	}
	if got := a.hosterSecret("pixeldrain.com"); got != pdtest.Key {
		t.Errorf("hosterSecret = %q, want the stored key", got)
	}
	a.SetAccountEnabled(hosterauth.Service, "pixeldrain.net", false)
	if got := a.hosterSecret("pixeldrain.com"); got != "" {
		t.Errorf("a switched-off login lent %q", got)
	}
	if got := a.hosterSecret("gofile.io"); got != "" {
		t.Errorf("Gofile got Pixeldrain's key %q", got)
	}
}

func TestAPixeldrainKeyOrGofileTokenStaysWithKnightLoaderAndRanksAtItsLoginRow(t *testing.T) {
	for _, c := range []struct{ host, id, link, secret string }{
		{"pixeldrain.net", pixeldrain.ResolverID, "https://pixeldrain.com/u/iniGVSnw", pdtest.Key},
		{"gofile.io", gofile.ResolverID, "https://gofile.io/d/AMZyDw", gftest.PremiumToken},
	} {
		t.Run(c.id, func(t *testing.T) {
			a := newCrawlApp(t, false)
			useFakeHosters(t, a)
			a.Registry.Register(jd.Resolver{})
			a.Registry.Register(debrid.Resolver{ServiceID: "alldebrid", Prio: 49, Hosts: map[string]bool{hostalias.Canonical(c.host): true}})
			jd.SetKnownHosts([]string{hostalias.Canonical(c.host)})
			t.Cleanup(func() { jd.SetKnownHosts(nil) })

			if err := a.SetHosterLogin(c.host, "reader", c.secret); err != nil {
				t.Fatal(err)
			}
			if !ownLogin(c.host) || jd.HostActive(hostalias.Canonical(c.host)) {
				t.Error("the login would go to JDownloader")
			}
			if got := chainIDs(a, c.link); !slices.Equal(got[:3], []string{c.id, "alldebrid", "jd"}) {
				t.Errorf("chain = %v, want our own client on the login first, then the debrid service, then JD", got)
			}
			if row, ok := loginRow(a, c.host); !ok || row.Status != hosterauth.StatusActive {
				t.Errorf("the login's row = %+v, want it active", row)
			}
			rows := a.ResolverPriority("")
			if i := slices.IndexFunc(rows, func(r resolver.Info) bool { return r.ID == loginRowID(c.host) }); i < 0 || !rows[i].OwnLogin {
				t.Errorf("the priority card = %+v, want the login's row marked as KnightLoader's own", rows)
			}

			if _, err := a.SaveResolverOrder([]string{"alldebrid", loginRowID(c.host), "jd"}); err != nil {
				t.Fatal(err)
			}
			if got := chainIDs(a, c.link); !slices.Equal(got[:2], []string{"alldebrid", c.id}) {
				t.Errorf("chain = %v, want the order the card was saved in", got)
			}
		})
	}
}

func TestAKrakenfilesDownloadAsksItsTurnstileCheckThroughTheCaptchaPrompts(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeHosters(t, a)
	body := bytes.Repeat([]byte("moonrise kingdom "), 4096)
	link := f.kf.Add("DP3nGKJNsX", kftest.File{Name: "Moonrise Kingdom (2012).mkv", Body: body})

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	waitFor(t, "the collector naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.tasks[id].Name == "Moonrise Kingdom (2012).mkv" && a.tasks[id].Online == core.AvailOnline
	})
	a.ConfirmTasks([]string{id}, confirm.Config{}, confirm.TriggerManual)
	var c captcha.Challenge
	waitFor(t, "the captcha reaching the prompts", func() bool {
		for _, ch := range a.CaptchaChallenges() {
			if ch.TaskID == id {
				c = ch
				return true
			}
		}
		return false
	})
	w, ok := c.Payload.(*captcha.WidgetPayload)
	if !ok || c.Host != "krakenfiles.com" || w.Vendor != captcha.VendorTurnstile || w.SiteKey != kftest.SiteKey || w.SiteURL != "https://krakenfiles.com/view/DP3nGKJNsX/file.html" {
		t.Fatalf("the prompt got %+v with %+v, want the file page's Turnstile check", c, c.Payload)
	}
	if _, err := a.AnswerCaptcha(context.Background(), c.ID, kftest.Token); err != nil {
		t.Fatal(err)
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
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, body) {
		t.Errorf("the downloaded file does not hold what Krakenfiles served: %v", err)
	}
}

func TestAWorkuploadFileIsNamedPastTheRobotCheckAndDownloads(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeHosters(t, a)
	body := bytes.Repeat([]byte("syscon "), 4096)
	link := f.wu.Add("QpDxHzUJcL8", wutest.File{Name: "PS3-SYSCON-1.1.ova", Body: body})

	created := a.AddLinks([]string{link}, "")
	id := created[0].ID
	waitFor(t, "the collector naming the file", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.tasks[id].Name == "PS3-SYSCON-1.1.ova" && a.tasks[id].Size == int64(len(body))
	})
	if got := downloaded(t, a, id); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what Workupload served")
	}
}
