package app

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/crawler"
	"github.com/junkerderprovinz/knightloader/internal/resolver/remotefs"
)

// The wiring tests for the remote-server resolver. Everything about the
// protocols themselves lives in internal/resolver/remotefs, against its own
// fake servers; what is worth pinning HERE is the three connections this
// package makes and nothing else does:
//
//   - rewireBackends registers the resolver even with no credential stored,
//   - crawl() sends a folder link to the remote lister instead of the HTML
//     crawler, and does so with the "crawl pages" setting off,
//   - backendFor hands those tasks to the remote backend rather than the
//     engine, which cannot speak any of these protocols.
//
// The addresses below all point at a port nothing is listening on, so the
// dial fails immediately and no test in this package ever leaves the machine.

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
	// And it must not have grown an appetite for ordinary web links on the way.
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
	// "Seiten crawlen" answers a question about the open internet: may this app
	// fetch an arbitrary web page somebody pasted. A folder on a server the
	// user configured is not that - the link IS the folder, and a folder cannot
	// be downloaded as one file - so gating it on that setting would make the
	// feature silently do nothing on any install that has it off.
	a := newCrawlApp(t, false)
	fc := &fakeCrawler{yield: []crawler.Result{{URL: "https://host.example/one.bin"}}}
	a.Crawler = fc

	created := a.AddLinks([]string{deadFTP}, "")
	// The HTML crawler must never see it: it cannot fetch ftp:// at all, and a
	// yield from it here would stage links from a completely different host.
	if len(fc.seen) != 0 {
		t.Errorf("the page crawler was asked about %v", fc.seen)
	}
	// The server is unreachable, so the expansion finds nothing and the link is
	// staged as itself with the reason on it - never dropped. Which is the
	// contract stage() has for every link in this app.
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
	// The convention the whole feature rests on: the ACCOUNT ID is the
	// hostname (accounts.GroupRemoteServer). Nothing enforces it at storage
	// time, so this is the test that keeps the two halves - what the accounts
	// route writes and what Match reads - talking about the same key.
	a := newCrawlApp(t, true)
	if err := a.SetAccountCredential(remotefs.ResolverID, "cloud.example.com", accounts.Credential{
		Username: "me", Password: "pw",
	}); err != nil {
		t.Fatal(err)
	}
	// Now, and only now, an ordinary https link on that host is WebDAV.
	got := a.Registry.For("https://cloud.example.com/remote.php/dav/files/me/film.mkv")
	if got == nil || got.Info().ID != remotefs.ResolverID {
		t.Fatalf("an https link on a configured WebDAV host routes to %v", got)
	}
	// A different host on the same install is untouched, which is what makes
	// the claim narrow enough to be safe.
	other := a.Registry.For("https://rapidgator.net/file/abc/film.mkv")
	if other != nil && other.Info().ID == remotefs.ResolverID {
		t.Error("storing one server's login claimed an unrelated host too")
	}

	// Switching the account off has to stop it claiming, the same way it stops
	// a debrid account routing - see remotefsLogins.
	a.SetAccountEnabled(remotefs.ResolverID, "cloud.example.com", false)
	if off := a.Registry.For("https://cloud.example.com/remote.php/dav/files/me/film.mkv"); off != nil && off.Info().ID == remotefs.ResolverID {
		t.Error("a disabled server account still claims its host's links")
	}
}

func TestRemoteServerLinkWithAPasswordInItIsRefusedWithASentence(t *testing.T) {
	// A password in a URL would be written to the task store in plain text and
	// shown in the collector's own URL column. The refusal has to reach the
	// row, not just the log, or the link looks like it failed for a network
	// reason.
	a := newCrawlApp(t, true)
	created := a.AddLinks([]string{"ftp://alice:hunter2@127.0.0.1:1/pub/x.iso"}, "")
	if len(created) != 1 {
		t.Fatalf("staged %d tasks, want the link itself with the reason on it", len(created))
	}
	if !strings.Contains(created[0].Error, "store it under Accounts") {
		t.Errorf("error = %q, want it to point at the account store", created[0].Error)
	}
}
