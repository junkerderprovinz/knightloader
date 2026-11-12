package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

type removedAnswer struct {
	Joined      bool                 `json:"joined"`
	Members     []fortressMemberView `json:"members"`
	Removed     []fortressRemoved    `json:"removed"`
	RemovedHere bool                 `json:"removedHere"`
}

func removedView(t *testing.T, base string) removedAnswer {
	t.Helper()
	var v removedAnswer
	if code := getJSON(t, base+"/api/fortress", &v); code != http.StatusOK {
		t.Fatalf("GET /api/fortress answered %d", code)
	}
	return v
}

// serverAt opens KnightLoader on dir, as a start after a restart does.
func serverAt(t *testing.T, dir string) (*httptest.Server, *app.App) {
	t.Helper()
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	return srv, a
}

func hasMember(v removedAnswer, id string) bool {
	return slices.ContainsFunc(v.Members, func(m fortressMemberView) bool { return m.ID == id })
}

func TestARemovedAppStaysNamedAndRefusedUntilThePhraseChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv, a := serverAt(t, dir)
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	engine, loop := arrowLoopOpen(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	waitFor(t, "ArrowLoop among the members", func() bool { return hasMember(removedView(t, srv.URL), engine) })
	own := memberKey(a).ID()

	code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/fortress/members/"+engine, nil)
	var after removedAnswer
	_ = json.Unmarshal(raw, &after)
	want := []fortressRemoved{{App: afterdownload.EngineApp, Name: "ArrowLoop", Device: "Tower"}}
	if code != http.StatusOK || !slices.Equal(after.Removed, want) || hasMember(after, engine) {
		t.Fatalf("removing ArrowLoop answered %d %s", code, raw)
	}

	// The next start still names it and still turns it away.
	srv.Close()
	a.Close()
	srv, _ = serverAt(t, dir)
	if v := removedView(t, srv.URL); !v.Joined || !slices.Equal(v.Removed, want) {
		t.Fatalf("after a restart the fortress reads %+v", v)
	}
	time.Sleep(3 * time.Second)
	if hasMember(removedView(t, srv.URL), engine) {
		t.Fatal("the removed app is a member again after a restart")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := loop.Status(ctx, own, "en"); err == nil {
		t.Fatal("KnightLoader answers the removed app after a restart")
	}

	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/phrase", map[string]string{}); code != http.StatusOK {
		t.Fatalf("the change answered %d %s", code, raw)
	}
	if v := removedView(t, srv.URL); len(v.Removed) != 0 {
		t.Fatalf("still named after the phrase changed: %+v", v.Removed)
	}
}

func TestRemovingNeedsAnotherMemberAndAFortress(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if code, _ := postJSON(t, http.MethodDelete, srv.URL+"/api/fortress/members/abc", nil); code != http.StatusNotFound {
		t.Errorf("removing outside a fortress answered %d", code)
	}
	ownRelay(t, a, srv.URL)
	_, words := newWords(t)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	for _, id := range []string{"abc", memberKey(a).ID()} {
		code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/fortress/members/"+id, nil)
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &refusal)
		if code != http.StatusNotFound || refusal.Code != "fortress.noMember" {
			t.Errorf("removing %q answered %d %s", id, code, raw)
		}
	}
	if v := removedView(t, srv.URL); v.RemovedHere || v.Removed == nil || len(v.Removed) != 0 {
		t.Errorf("the view reads %+v", v)
	}
}

func TestAnAPITokenCannotRemoveAMember(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.CreateScoped("script", apitoken.AllScopes())
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/fortress/members/abc", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a token with every right removed with %d", res.StatusCode)
	}
}

func TestAnInstanceTakenOutSaysSoUntilItJoinsAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv, a := serverAt(t, dir)
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	_, loop := arrowLoopOpen(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	own := memberKey(a).ID()
	waitFor(t, "ArrowLoop to know KnightLoader", func() bool {
		return slices.ContainsFunc(loop.Members(), func(m fortress.Member) bool { return m.ID == own && m.App.ID == appID })
	})

	if err := loop.Remove(own); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the view to say another app took KnightLoader out", func() bool {
		v := removedView(t, srv.URL)
		return !v.Joined && v.RemovedHere
	})

	srv.Close()
	a.Close()
	srv, a = serverAt(t, dir)
	if v := removedView(t, srv.URL); !v.RemovedHere {
		t.Fatalf("the next start forgets that another app took KnightLoader out: %+v", v)
	}
	ownRelay(t, a, srv.URL)
	_, again := newWords(t)
	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": again})
	var joined removedAnswer
	_ = json.Unmarshal(raw, &joined)
	if code != http.StatusOK || !joined.Joined || joined.RemovedHere {
		t.Fatalf("joining again answered %d %s", code, raw)
	}
}
