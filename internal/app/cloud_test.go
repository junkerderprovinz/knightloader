package app

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/dropbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/dropbox/dbtest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/googledrive"
	"github.com/junkerderprovinz/knightloader/internal/resolver/googledrive/gdtest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/onedrive"
	"github.com/junkerderprovinz/knightloader/internal/resolver/onedrive/odtest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/swisstransfer"
	"github.com/junkerderprovinz/knightloader/internal/resolver/swisstransfer/sttest"
	"github.com/junkerderprovinz/knightloader/internal/resolver/wetransfer"
	"github.com/junkerderprovinz/knightloader/internal/resolver/wetransfer/wttest"
)

// fakeClouds are the stand-ins the app's clients for the cloud shares are
// pointed at.
type fakeClouds struct {
	gd *gdtest.Server
	db *dbtest.Server
	od *odtest.Server
	wt *wttest.Server
	st *sttest.Server
}

func useFakeClouds(t *testing.T, a *App) fakeClouds {
	f := fakeClouds{gd: gdtest.New(t), db: dbtest.New(t), od: odtest.New(t), wt: wttest.New(t), st: sttest.New(t)}
	eng := engineHandoff{a.Engine, a}
	gd := googledrive.NewClient(googledrive.Sites{Drive: f.gd.URL, Content: f.gd.URL, API: f.gd.URL, Docs: f.gd.URL}, f.gd.Client())
	db := dropbox.NewClient(f.db.URL, f.db.Client())
	od := onedrive.NewClient(f.od.URL, f.od.Client())
	wt := wetransfer.NewClient(f.wt.URL, f.wt.Client())
	st := swisstransfer.NewClient(f.st.URL, f.st.Client())
	clients := map[string]hosterClient{
		googledrive.ResolverID:   {googledrive.Resolver{Client: gd}, a.hosterBackend("Google Drive", "drive.google.com", gd, eng)},
		dropbox.ResolverID:       {dropbox.Resolver{Client: db}, a.hosterBackend("Dropbox", "dropbox.com", db, eng)},
		onedrive.ResolverID:      {onedrive.Resolver{Client: od}, a.hosterBackend("OneDrive", "onedrive.live.com", od, eng)},
		wetransfer.ResolverID:    {wetransfer.Resolver{Client: wt}, a.hosterBackend("WeTransfer", "wetransfer.com", wt, eng)},
		swisstransfer.ResolverID: {swisstransfer.Resolver{Client: st}, a.hosterBackend("SwissTransfer", "swisstransfer.com", st, eng)},
	}
	a.bmu.Lock()
	for id, c := range clients {
		a.hosters[id] = c
	}
	a.bmu.Unlock()
	for _, c := range clients {
		a.Registry.Register(c.res)
	}
	return f
}

// cloudLinks is one share link of each service, by the host a debrid service
// would list it under.
var cloudLinks = map[string]struct{ id, link string }{
	"drive.google.com":  {googledrive.ResolverID, "https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c/view"},
	"dropbox.com":       {dropbox.ResolverID, "https://www.dropbox.com/scl/fi/hzfx1l267m8gwyhcjvfk4/slides.pdf?rlkey=k&dl=0"},
	"onedrive.live.com": {onedrive.ResolverID, "https://1drv.ms/u/s!AjTsLJdE37DwtrsnIehxKx7N7XgoBg?e=r1sszn"},
	"wetransfer.com":    {wetransfer.ResolverID, "https://wetransfer.com/downloads/5b2a0b4c7d5c9e6f1a2b3c4d5e6f708192a3b4c5d6e7f8/a1b2c3"},
	"swisstransfer.com": {swisstransfer.ResolverID, "https://www.swisstransfer.com/dl/01a1a57e-cc7d-7249-ac30-4c3063a223f5"},
}

func TestTheCloudClientsGoBelowDebridAndAJDLoginAndAboveJDsFreeMode(t *testing.T) {
	for host, c := range cloudLinks {
		t.Run(host, func(t *testing.T) {
			a := newCrawlApp(t, false)
			useFakeClouds(t, a)
			a.Registry.Register(jd.Resolver{})
			a.Registry.Register(debrid.Resolver{ServiceID: "zevera", Prio: 43, Hosts: map[string]bool{host: true}})
			jd.SetKnownHosts([]string{host})
			t.Cleanup(func() { jd.SetKnownHosts(nil) })

			got := chainIDs(a, c.link)
			if !slices.Equal(got[:3], []string{"zevera", c.id, "jd"}) {
				t.Errorf("chain = %v, want the debrid service, then our own client, then JD", got)
			}
			if slices.Contains(got, "direct") || slices.Contains(got, "http") {
				t.Errorf("chain = %v, a plain GET would save the share's page", got)
			}
			if _, ok := a.backendFor(c.id).(*hoster.Backend); !ok {
				t.Errorf("%s tasks go to %T", c.id, a.backendFor(c.id))
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
		})
	}
}

func TestAPlainGetLeavesEveryCloudShareToItsClient(t *testing.T) {
	a := newCrawlApp(t, false)
	for _, host := range []string{"drive.google.com", "docs.google.com", "www.dropbox.com", "1drv.ms", "onedrive.live.com", "contoso-my.sharepoint.com", "wetransfer.com", "we.tl", "www.swisstransfer.com"} {
		if !a.claims.pageOnly(host) {
			t.Errorf("a plain GET would take links of %s", host)
		}
	}
	if a.claims.pageOnly("dl.dropboxusercontent.com") {
		t.Error("Dropbox's content server hands out files to a plain GET and is left to it")
	}
}

func TestAGoogleDriveFolderBecomesOneRowPerFileAndAFileDownloads(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeClouds(t, a)
	body := bytes.Repeat([]byte("model weights "), 4096)
	f.gd.Add("1fileAfileAfileAfileA0", gdtest.Item{Name: "fcn8s_from_caffe.npz", Body: body, Big: true})
	f.gd.Add("1fileBfileBfileBfileB0", gdtest.Item{Name: "spam.txt", Body: []byte("spam\n")})
	link := f.gd.Add("15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl", gdtest.Item{Name: "gdown", Mime: gdtest.Folder, Children: []string{"1fileAfileAfileAfileA0", "1fileBfileBfileBfileB0"}})

	rows := staged(t, a, a.AddLinks([]string{link}, ""), 2)
	byName := map[string]*core.Task{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	npz := byName["fcn8s_from_caffe.npz"]
	if npz == nil || npz.Resolver != googledrive.ResolverID || npz.Size != int64(len(body)) {
		t.Fatalf("staged %+v, want the folder's files on our own client", rows)
	}
	if got := downloaded(t, a, npz.ID); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what Drive served past its virus scan warning")
	}
}

func TestAOneDriveFolderFileDownloadsWithTheShareGuestCookie(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 races on a task's status when a real transfer starts")
	}
	a := newCrawlApp(t, false)
	f := useFakeClouds(t, a)
	body := bytes.Repeat([]byte("checkpoint "), 4096)
	root := "/personal/17e16246b3df8f06/Documents/ShuffleNetSeries"
	f.od.File(root+"/ShuffleNetV2+/models/ShuffleNetV2+.Small.pth.tar", body)
	link := f.od.Add("AgaP37NGYuEXhRfQxHRseR7eSxXo", odtest.Share{Folder: true, Path: root})

	rows := staged(t, a, a.AddLinks([]string{link}, ""), 1)
	if rows[0].Resolver != onedrive.ResolverID || !strings.Contains(rows[0].URL, "#path=") {
		t.Fatalf("staged %+v, want the folder's file on our own client", rows[0])
	}
	if got := downloaded(t, a, rows[0].ID); !bytes.Equal(got, body) {
		t.Error("the downloaded file does not hold what SharePoint served the guest")
	}
}
