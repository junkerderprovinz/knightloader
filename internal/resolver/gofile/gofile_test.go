package gofile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/gofile/gftest"
)

func init() { inspectPause = 0 }

func newClient(srv *gftest.Server) *Client {
	c := NewClient(srv.API, srv.Site, srv.Client())
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://gofile.io/d/AMZyDw":                               "https://gofile.io/d/AMZyDw",
		"http://www.gofile.io/d/AMZyDw/":                           "https://gofile.io/d/AMZyDw",
		"https://gofile.net/d/AMZyDw":                              "https://gofile.io/d/AMZyDw",
		"https://gofile.io/?c=AMZyDw":                              "https://gofile.io/d/AMZyDw",
		"https://gofile.io/d/de571ac1-5edc-42e2-8ec2-bdac83ad4a31": "https://gofile.io/d/de571ac1-5edc-42e2-8ec2-bdac83ad4a31",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"https://gofile.io/", "https://gofile.io/myprofile", "https://example.com/d/AMZyDw", "https://gofile.io/d/"} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	srv := gftest.New(t)
	res, err := Resolver{Client: newClient(srv)}.Resolve(context.Background(), resolver.Request{URL: "https://gofile.net/d/AMZyDw"})
	if err != nil || res.DirectURL != "https://gofile.io/d/AMZyDw" || res.Connections != maxConns {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
	if len(srv.Guests()) != 0 || len(srv.Contents()) != 0 {
		t.Error("Resolve asked Gofile")
	}
}

func TestAFolderOpensIntoEveryFileOfItsSubfoldersSignedAsTheWebsiteSigns(t *testing.T) {
	srv := gftest.New(t)
	srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("aaaa")})
	srv.Add("f2", "", gftest.Item{Name: "b.srt", Body: []byte("bb")})
	srv.Add("f3", "", gftest.Item{Name: "c.nfo", Body: []byte("c")})
	srv.Add("sub", "", gftest.Item{Name: "Extras", Children: []string{"f3"}})
	link := srv.Add("root", "AMZyDw", gftest.Item{Name: "Release", Children: []string{"f2", "sub", "f1"}})
	c := newClient(srv)
	c.pageSize = 2

	got, err := Resolver{Client: c}.List(context.Background(), link, "")
	want := []resolver.Listing{
		{URL: "https://gofile.io/d/f1", Name: "a.mkv", Size: 4, Folder: "Release"},
		{URL: "https://gofile.io/d/f2", Name: "b.srt", Size: 2, Folder: "Release"},
		{URL: "https://gofile.io/d/f3", Name: "c.nfo", Size: 1, Folder: "Release/Extras"},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("List = %+v, %v; want %+v", got, err, want)
	}
	if guests := srv.Guests(); len(guests) != 1 {
		t.Errorf("made %d guest accounts, want one for the whole listing", len(guests))
	}
	if srv.Scripts() != 1 {
		t.Errorf("fetched the website script %d times, want once", srv.Scripts())
	}
}

func TestAFileLinkListsNothingAndIsNamedByInspect(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "TMjXd9", gftest.Item{Name: "nuuh.mp4", Body: []byte("12345")})
	folder := srv.Add("root", "AMZyDw", gftest.Item{Name: "Release", Children: []string{"f1"}})
	r := Resolver{Client: newClient(srv)}

	if files, err := r.List(context.Background(), link, ""); err != nil || files != nil {
		t.Errorf("a file link listed %+v, %v; want nothing", files, err)
	}
	got := r.Inspect(context.Background(), []string{link, folder, "https://gofile.io/d/zzzzzz", "https://example.com/x"})
	want := []resolver.LinkInfo{{Availability: core.AvailOnline, Name: "nuuh.mp4", Size: 5}, {}, {Availability: core.AvailOffline}, {}}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
}

func TestTheDownloadCarriesTheAccountTokenCookieAndIsFetchable(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("payload")})
	c := newClient(srv)

	l, err := c.Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Conns != maxConns || l.Name != "a.mkv" || l.Size != 7 || !strings.HasPrefix(l.Headers["Cookie"], "accountToken=GUEST") {
		t.Fatalf("Unlock = %+v", l)
	}
	req, _ := http.NewRequest(http.MethodGet, l.URL, nil)
	for k, v := range l.Headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "payload" {
		t.Errorf("the link with its headers fetched %q", body)
	}
}

func TestAStoredPremiumTokenIsUsedInsteadOfAGuest(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x"), PremiumOnly: true})
	c := newClient(srv)
	c.Token = func() string { return gftest.PremiumToken }

	l, err := c.Unlock(context.Background(), link, "")
	if err != nil || l.Headers["Cookie"] != "accountToken="+gftest.PremiumToken {
		t.Errorf("Unlock = %+v, %v; want the premium token", l, err)
	}
	if len(srv.Guests()) != 0 {
		t.Error("a guest account was made beside the stored token")
	}
}

func TestPremiumOnlyContentWithoutAPremiumTokenFailsAsAuth(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x"), PremiumOnly: true})
	_, err := newClient(srv).Unlock(context.Background(), link, "")
	var r *hoster.Refusal
	if !errors.As(err, &r) || r.Reason != core.ReasonAuth || !strings.Contains(r.Msg, "premium") {
		t.Errorf("Unlock = %v, want an auth refusal naming premium", err)
	}
}

func TestAProtectedFolderOpensWithTheDownloadPasswordOnly(t *testing.T) {
	srv := gftest.New(t)
	srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x")})
	link := srv.Add("root", "AMZyDw", gftest.Item{Name: "Locked", Children: []string{"f1"}, Password: "hunter2"})
	r := Resolver{Client: newClient(srv)}

	files, err := r.List(context.Background(), link, "hunter2")
	if err != nil || len(files) != 1 {
		t.Fatalf("List with the password = %+v, %v", files, err)
	}
	if _, err := r.Client.Unlock(context.Background(), files[0].URL, "hunter2"); err != nil {
		t.Errorf("a file of the folder did not unlock with the password: %v", err)
	}
	for pass, want := range map[string]string{"": "has no download password", "wrong": "not accepted"} {
		_, err := r.Client.Unlock(context.Background(), files[0].URL, pass)
		var ref *hoster.Refusal
		if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth || !strings.Contains(ref.Msg, want) {
			t.Errorf("password %q: Unlock = %v, want an auth refusal saying it %s", pass, err, want)
		}
	}
}

func TestAPrivateFolderAndAMissingOneAreToldApart(t *testing.T) {
	srv := gftest.New(t)
	private := srv.Add("root", "AMZyDw", gftest.Item{Name: "Mine", Private: true})
	r := Resolver{Client: newClient(srv)}
	_, err := r.List(context.Background(), private, "")
	var ref *hoster.Refusal
	if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("private folder: %v, want an auth refusal", err)
	}
	if _, err := r.List(context.Background(), "https://gofile.io/d/zzzzzz", ""); !hoster.IsGone(err) {
		t.Errorf("missing folder: %v, want gone", err)
	}
}

func TestARateLimitIsWaitedOutBriefly(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x")})
	srv.RateLimit(2)
	c := newClient(srv)
	var waited []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }

	if _, err := c.Unlock(context.Background(), link, ""); err != nil {
		t.Fatal(err)
	}
	if len(waited) != 2 {
		t.Errorf("waited %v, want twice", waited)
	}

	srv.RateLimit(100)
	_, err := c.Unlock(context.Background(), link, "")
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Reason != core.ReasonLimit {
		t.Errorf("a lasting rate limit gave %v, want a wait failing as limit", err)
	}
}

func TestTheWebsiteScriptIsFetchedAgainOnceItIsOld(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x")})
	c := newClient(srv)
	now := time.Now()
	c.now = func() time.Time { return now }

	if _, err := c.Unlock(context.Background(), link, ""); err != nil {
		t.Fatal(err)
	}
	srv.Rotate("second")
	now = now.Add(scriptMaxAge + time.Minute)
	if _, err := c.Unlock(context.Background(), link, ""); err != nil {
		t.Errorf("after Gofile rotated its secret: %v", err)
	}
	if srv.Scripts() != 2 {
		t.Errorf("fetched the script %d times, want twice", srv.Scripts())
	}
}

func TestARejectedGuestTokenIsReplacedOnce(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x")})
	c := newClient(srv)
	c.guest = "GUESTxEXPIREDxxxxxxxxxxxxxxxxxxx"

	if _, err := c.Unlock(context.Background(), link, ""); err != nil {
		t.Fatal(err)
	}
	if len(srv.Guests()) != 1 {
		t.Errorf("made %d guest accounts, want one to replace the stale token", len(srv.Guests()))
	}
}

func TestARejectedStoredTokenFailsAsAuth(t *testing.T) {
	srv := gftest.New(t)
	link := srv.Add("f1", "", gftest.Item{Name: "a.mkv", Body: []byte("x")})
	c := newClient(srv)
	c.Token = func() string { return "WRONGxTOKENxxxxxxxxxxxxxxxxxxxxx" }
	_, err := c.Unlock(context.Background(), link, "")
	var ref *hoster.Refusal
	if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("Unlock = %v, want an auth refusal", err)
	}
}
