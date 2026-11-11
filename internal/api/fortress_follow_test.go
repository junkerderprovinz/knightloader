package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/phrase"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// openPlugin opens the member that holds only keys, as a plugin keeps it in
// dir.
func openPlugin(t *testing.T, dir string) *kitfortress.Fortress {
	t.Helper()
	cfg, err := kitfortress.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := kitfortress.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// pluginMeets reports whether the plugin reaches KnightLoader a.
func pluginMeets(p *kitfortress.Fortress, a *app.App) bool {
	id := memberKey(a).ID()
	for _, m := range p.Members() {
		if m.ID == id && m.Online() {
			return true
		}
	}
	return false
}

// pluginBeforeAChange joins a plugin to the fortress a is in, lets the two
// meet and changes the phrase while the plugin is away, as a plugin that runs
// now and then is. It returns the plugin's directory and the new secret.
func pluginBeforeAChange(t *testing.T, base string, a *app.App, relayURL string, secret []byte) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := kitfortress.Join(dir, secret, kitfortress.Membership{App: "watcher", Device: "Cellar", Relay: relayURL}); err != nil {
		t.Fatal(err)
	}
	p := openPlugin(t, dir)
	waitFor(t, "the plugin to meet KnightLoader", func() bool { return pluginMeets(p, a) })
	waitFor(t, "KnightLoader to list the plugin", func() bool {
		for _, m := range fortressNow(t, base).Members {
			if m.App == "watcher" {
				return true
			}
		}
		return false
	})
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	code, raw := postJSON(t, http.MethodPost, base+"/api/fortress/phrase", map[string]string{})
	var changed struct {
		Phrase string `json:"phrase"`
	}
	_ = json.Unmarshal(raw, &changed)
	if code != http.StatusOK {
		t.Fatalf("the change answered %d %s", code, raw)
	}
	next, err := phrase.Decode(changed.Phrase)
	if err != nil {
		t.Fatal(err)
	}
	return dir, next
}

func TestAMemberWithOnlyKeysFollowsAPhraseChange(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	dir, next := pluginBeforeAChange(t, srv.URL, a, relayURL, secret)

	p := openPlugin(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	followed := false
	for !followed && ctx.Err() == nil {
		var err error
		if followed, err = kitfortress.Follow(ctx, dir, p); err != nil && ctx.Err() == nil {
			t.Logf("follow: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !followed {
		t.Fatal("the plugin never got the keys under the new phrase")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "fortress.json"))
	if err != nil {
		t.Fatal(err)
	}
	var kept struct {
		Group string `json:"group"`
	}
	if err := json.Unmarshal(raw, &kept); err != nil || kept.Group != kitfortress.Domain.Keys(next).Group {
		t.Fatalf("the plugin keeps %s, want the fortress keys under the new phrase", raw)
	}

	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	moved := openPlugin(t, dir)
	waitFor(t, "the plugin to meet KnightLoader under the new phrase", func() bool { return pluginMeets(moved, a) })
}

func TestTheOldPhraseIsForgottenThirtyDaysAfterTheChange(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	dir, _ := pluginBeforeAChange(t, srv.URL, a, relayURL, secret)
	if storedSecret(a.Accounts, federation.FortressPreviousAccount) == nil {
		t.Fatal("the phrase before the change is not kept")
	}

	month := strconv.FormatInt(time.Now().Add(-previousKept-time.Hour).Unix(), 10)
	if err := a.Accounts.Set(federation.FortressPreviousSinceAccount, month); err != nil {
		t.Fatal(err)
	}
	if err := regroup(a, nil); err != nil {
		t.Fatal(err)
	}
	if storedSecret(a.Accounts, federation.FortressPreviousAccount) != nil {
		t.Error("the phrase before the change is still kept 30 days after it")
	}
	p := openPlugin(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		if ok, _ := kitfortress.Follow(ctx, dir, p); ok {
			t.Fatal("the old phrase still hands out the new keys 30 days after the change")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pluginMeets(p, a) {
		t.Error("KnightLoader still answers on the old phrase 30 days after the change")
	}
}
