package app

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/reclaim"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The wiring tests for the remote-server resolver. The protocols themselves are
// covered in internal/resolver/remotefs against its own fake servers; what this
// package contributes is three connections:
//
//   - rewireBackends registers the resolver even with no credential stored,
//   - crawl() sends a folder link to the remote lister instead of the HTML
//     crawler, and does so with the "crawl pages" setting off,
//   - backendFor hands those tasks to the remote backend rather than the
//     engine, which cannot speak any of these protocols.
//
// The addresses below point at a port nothing is listening on, so the dial
// fails at once and no test here leaves the machine.

// deadFTP is a link to a port nothing answers on. Port 1 is reserved and never
// bound, so the connection is refused in microseconds rather than timing out.
const deadFTP = "ftp://127.0.0.1:1/pub"

func TestRemoteServerResolverIsRegisteredWithoutAnyCredential(t *testing.T) {
	// Unlike every debrid service, this resolver is not gated on a stored
	// account: a public FTP archive is fetched anonymously, so a registration
	// that waited for a credential would leave "ftp://..." claimed by nothing
	// and settled as "no resolver matches".
	a := newCrawlApp(t, true)
	if got := a.Registry.For("ftp://ftp.example.org/pub/thing.iso"); got == nil {
		t.Fatal("nothing claims an ftp:// link")
	} else if got.Info().ID != remotefs.ResolverID {
		t.Fatalf("an ftp:// link routes to %q", got.Info().ID)
	}
	// And it does not claim ordinary web links.
	if got := a.Registry.For("https://host.example/movie.mkv"); got != nil && got.Info().ID == remotefs.ResolverID {
		t.Error("an ordinary https link was claimed by the remote-server resolver")
	}
}

func TestRemoteServerLinksGoToTheirOwnBackendAndNotTheEngine(t *testing.T) {
	// The engine speaks http, bittorrent and ed2k. Handing it an sftp:// link
	// would fail on the scheme, at start time, with nothing pointing back here.
	a := newCrawlApp(t, true)
	if be := a.backendFor(remotefs.ResolverID); be == a.Engine {
		t.Fatal("remote-server tasks were routed to the embedded engine")
	}
	// HonoursCollisionPolicy reads the same table, and its answer is what the
	// interface uses to decide whether to offer a rename control at all.
	if a.HonoursCollisionPolicy(remotefs.ResolverID) {
		t.Error("the collision policy is advertised as reaching a delegated backend")
	}
}

func TestRemoteFolderExpansionRunsEvenWithPageCrawlingSwitchedOff(t *testing.T) {
	// "Seiten crawlen" asks whether this app may fetch an arbitrary web page
	// somebody pasted. A folder on a server the user configured is a different
	// question: the link is the folder, and a folder cannot be downloaded as one
	// file, so gating it on that setting would disable the feature outright.
	a := newCrawlApp(t, false)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin"}}}
	a.Crawler = fc

	created := a.AddLinks([]string{deadFTP}, "")
	// The HTML crawler never sees it: it cannot fetch ftp://, and a yield from
	// it here would stage links from a different host.
	if len(fc.seen) != 0 {
		t.Errorf("the page crawler was asked about %v", fc.seen)
	}
	// The server is unreachable, so the expansion finds nothing and the link is
	// staged as itself with the reason on it, as stage does for every link.
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the link itself", len(created))
	}
	if created[0].Resolver != remotefs.ResolverID {
		t.Errorf("resolver = %q, want the remote-server one", created[0].Resolver)
	}
	if created[0].Error == "" {
		t.Error("an unreachable server produced a task with no reason on it")
	}
}

func TestRemoteServerAccountsAreReadPerHost(t *testing.T) {
	// The account id is the hostname (accounts.GroupRemoteServer). Nothing
	// enforces that at storage time, so this keeps what the accounts route
	// writes and what Match reads on the same key.
	a := newCrawlApp(t, true)
	if err := a.SetAccountCredential(remotefs.ResolverID, "cloud.example.com", accounts.Credential{
		Username: "me", Password: "pw",
	}); err != nil {
		t.Fatal(err)
	}
	// With the credential stored, an ordinary https link on that host is WebDAV.
	got := a.Registry.For("https://cloud.example.com/remote.php/dav/files/me/film.mkv")
	if got == nil || got.Info().ID != remotefs.ResolverID {
		t.Fatalf("an https link on a configured WebDAV host routes to %v", got)
	}
	// A different host on the same install is untouched.
	other := a.Registry.For("https://rapidgator.net/file/abc/film.mkv")
	if other != nil && other.Info().ID == remotefs.ResolverID {
		t.Error("storing one server's login claimed an unrelated host too")
	}

	// Switching the account off stops it claiming, as it stops a debrid account
	// routing; see remotefsLogins.
	a.SetAccountEnabled(remotefs.ResolverID, "cloud.example.com", false)
	if off := a.Registry.For("https://cloud.example.com/remote.php/dav/files/me/film.mkv"); off != nil && off.Info().ID == remotefs.ResolverID {
		t.Error("a disabled server account still claims its host's links")
	}
}

func TestRemoteServerLinkWithAPasswordInItIsKeptNowhere(t *testing.T) {
	// A password in a URL would sit in the task list, the database and the log
	// in plain text. The link is refused before any of them sees it, and the
	// refusal is listed with the password masked, so the link does not just
	// vanish.
	const link = "ftp://alice:hunter2@127.0.0.1:1/pub/sample.iso"
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	fromContainer := func(a *App) []*core.Task {
		return a.AddResolvedLinksFrom([]resolver.Result{{DirectURL: link}}, "", OriginContainer)
	}
	for _, tc := range []struct {
		name   string
		filter bool
		add    func(a *App) []*core.Task
	}{
		{"pasted", false, func(a *App) []*core.Task { return a.AddLinks([]string{link}, "") }},
		{"from a container", false, fromContainer},
		{"turned down by the link filter", true, fromContainer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
				if tc.filter {
					s.LinkFilter = rejectRule("sample files are not wanted here")
				}
			})
			if created := tc.add(a); len(created) != 0 {
				t.Fatalf("staged %d tasks, want the link refused", len(created))
			}
			stored, err := a.Store.All()
			if err != nil {
				t.Fatal(err)
			}
			if len(stored) != 0 {
				t.Errorf("the store holds %q, want nothing", stored[0].URL)
			}
			skipped := a.SkippedLinks()
			if len(skipped) != 1 {
				t.Fatalf("%d refusals listed, want the one", len(skipped))
			}
			if !strings.Contains(skipped[0].Reason, "store it under Accounts") {
				t.Errorf("reason = %q, want it to point at the account store", skipped[0].Reason)
			}
			if strings.Contains(skipped[0].URL, "hunter2") {
				t.Errorf("the refusal shows the password: %q", skipped[0].URL)
			}
		})
	}
	if strings.Contains(logged.String(), "hunter2") {
		t.Errorf("the log holds the password:\n%s", logged.String())
	}
}

// After a restart the backend knows nothing of a paused download, so removing
// the row with its files has to find the part file from the task alone, and
// only that task's: another download of the same name keeps its own.
func TestRemovingAPausedRemoteDownloadWithFilesDeletesItsPartFileAfterARestart(t *testing.T) {
	a := newCrawlApp(t, true)
	dir := t.TempDir()
	a.mu.Lock()
	for id, link := range map[string]string{"1": "ftp://127.0.0.1:1/pub/film.mkv", "2": "ftp://127.0.0.1:1/pub/other/film.mkv"} {
		a.tasks[id] = &core.Task{
			ID: id, URL: link, Name: "film.mkv", Resolver: remotefs.ResolverID, Dir: dir,
			Status: core.StatusPaused, Enabled: true, Size: 4096, Loaded: 1024,
		}
	}
	a.mu.Unlock()
	mine, theirs := reclaim.PartPath(dir, "film.mkv", "1"), reclaim.PartPath(dir, "film.mkv", "2")
	for _, p := range []string{mine, theirs} {
		if err := os.WriteFile(p, make([]byte, 1024), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a.Remove("1", true)

	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Error("the removed download's part file is still there")
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Errorf("the other download of the same name lost its part file: %v", err)
	}
}

// pausedRemoteTask is a paused FTP download in the download folder as a
// restart finds it: the backend has never heard of it, and its part file holds
// what it had fetched.
func pausedRemoteTask(t *testing.T, a *App, id, name string) (part string) {
	t.Helper()
	link := "ftp://127.0.0.1:1/pub/film.mkv"
	a.mu.Lock()
	dir := a.defaultDir()
	a.tasks[id] = &core.Task{
		ID: id, URL: link, Name: name, Resolver: remotefs.ResolverID, Dir: dir,
		Status: core.StatusPaused, Enabled: true, Size: 4096, Loaded: 1024,
	}
	a.mu.Unlock()
	part = remotefs.PartFile(dir, link, id)
	if err := os.WriteFile(part, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	return part
}

func orphansOf(t *testing.T, a *App) []reclaim.Orphan {
	t.Helper()
	rep, err := a.Reclaim()
	if err != nil {
		t.Fatal(err)
	}
	return rep.Orphans
}

// A remove without files is the one an undo can take back, so the part file
// stays for the row to resume from, and until then nothing claims it.
func TestAPlainRemoveOfAPausedRemoteDownloadKeepsItsPartFileForAnUndo(t *testing.T) {
	a := newCrawlApp(t, true)
	part := pausedRemoteTask(t, a, "1", "film.mkv")

	removed, token := a.RemoveTasksUndoable([]string{"1"}, false)
	if len(removed) != 1 || token == "" {
		t.Fatalf("removed %v with token %q, want the row and a way back", removed, token)
	}
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("a remove without files deleted the part file: %v", err)
	}
	if got := orphansOf(t, a); len(got) != 1 || got[0].Path != part {
		t.Errorf("orphans = %+v, want the part file no row claims any more", got)
	}

	if back := a.UndoRemove(token); len(back) != 1 {
		t.Fatalf("undo brought back %v", back)
	}
	if got := liveTask(a, "1"); got.Loaded != 1024 {
		t.Errorf("loaded = %d after the undo, want the 1024 bytes in the part file", got.Loaded)
	}
	if got := orphansOf(t, a); len(got) != 0 {
		t.Errorf("orphans = %+v after the undo, want none", got)
	}
}

// The part file is named from the link, and a rename of the paused row leaves
// it where it is.
func TestTheRenamedRemoteDownloadsPartFileIsNotAnOrphan(t *testing.T) {
	a := newCrawlApp(t, true)
	pausedRemoteTask(t, a, "1", "renamed.mkv")

	if got := orphansOf(t, a); len(got) != 0 {
		t.Errorf("orphans = %+v, want the paused row's part file claimed", got)
	}
}
