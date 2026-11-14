package app

import (
	"context"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/feed"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// trustApp is an app whose browsers hold a Patreon session and which crawls
// no page, so staging a link touches no network.
func trustApp(t *testing.T) *App {
	t.Helper()
	stubSiteCheck(t, sitelogin.Status{State: sitelogin.SignedIn})
	a := siteApp(t)
	s := a.Settings.Get()
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	withBrowsers(t, a).set("patreon.com", patreonIn(firefoxProfile, "FROM-FIREFOX", anHourAgo))
	return a
}

const patreonPost = "https://www.patreon.com/posts/a-post-1"

func addedID(t *testing.T, a *App, opts LinkBatchOptions) string {
	t.Helper()
	added, err := a.AddLinksWithOptions([]string{patreonPost}, "", OriginPaste, opts)
	if err != nil || len(added) != 1 {
		t.Fatalf("adding the link = %v, %v", added, err)
	}
	return added[0].ID
}

// A token that may only add and read must not get content behind the owner's
// sign-ins fetched for it.
func TestALinkAddedBelowAdminGoesOutWithoutTheOwnersSessions(t *testing.T) {
	a := trustApp(t)
	id := addedID(t, a, LinkBatchOptions{})
	if a.maySignIn(id) {
		t.Error("a link added below admin may go out with the owner's sessions")
	}
	if yb := a.ytdlpBackend(""); yb.MaySignIn == nil || yb.MaySignIn(id) {
		t.Error("the yt-dlp backend would hand the owner's sessions to a link added below admin")
	}
}

func TestALinkAnAdminAddedGoesOutWithTheOwnersSessions(t *testing.T) {
	a := trustApp(t)
	id := addedID(t, a, LinkBatchOptions{ByAdmin: true})
	if !a.maySignIn(id) || !a.ytdlpBackend("").MaySignIn(id) {
		t.Error("a link the owner added may not use the owner's sessions")
	}
	for _, task := range a.Tasks() {
		if task.ID == id && !task.AddedByAdmin {
			t.Errorf("the task does not record that an admin added it: %+v", task)
		}
	}
}

// A feed's entries are links the feed chose, whoever subscribed to it.
func TestAFeedItemGoesOutWithoutTheOwnersSessions(t *testing.T) {
	a := trustApp(t)
	a.stageFeedJob(feed.Job{URL: patreonPost, Package: "Feed", Source: "https://feeds.example/rss"})
	found := false
	for _, task := range a.Tasks() {
		if task.URL != patreonPost {
			continue
		}
		found = true
		if a.maySignIn(task.ID) {
			t.Errorf("the feed's entry %s may go out with the owner's sessions", task.ID)
		}
	}
	if !found {
		t.Fatal("the feed's entry was not staged")
	}
}

func TestAnUnknownTaskGoesOutWithoutTheOwnersSessions(t *testing.T) {
	a := trustApp(t)
	if a.maySignIn("no-such-task") {
		t.Error("a task that is not there may go out with the owner's sessions")
	}
}

// signInProber records whether each probe was let use the owner's sessions.
type signInProber struct {
	mu   sync.Mutex
	seen []bool
}

func (p *signInProber) ProbeTitle(ctx context.Context, _ string, _ map[string]string) (ytdlp.ProbeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, ytdlp.SignInAllowed(ctx))
	return ytdlp.ProbeResult{Title: "A Post"}, nil
}

// A probe answered for an admin's link is not handed to a link added below
// admin, which would show what the owner's session sees.
func TestAProbeUsesTheSessionsOnlyForAnAdminsLink(t *testing.T) {
	a := trustApp(t)
	p := &signInProber{}
	if _, err := a.linkProbe(context.Background(), p, patreonPost, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.linkProbe(context.Background(), p, patreonPost, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.linkProbe(context.Background(), p, patreonPost, nil, false); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seen) != 2 || p.seen[0] || !p.seen[1] {
		t.Errorf("probes let use the sessions: %v, want one without and one with, each answered once", p.seen)
	}
}
