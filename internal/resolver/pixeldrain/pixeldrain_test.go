package pixeldrain

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/pixeldrain/pdtest"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://pixeldrain.com/u/iniGVSnw":                 "https://pixeldrain.com/u/iniGVSnw",
		"http://www.pixeldrain.com/u/iniGVSnw?embed":        "https://pixeldrain.com/u/iniGVSnw",
		"https://pixeldrain.net/u/iniGVSnw":                 "https://pixeldrain.com/u/iniGVSnw",
		"https://pixeldra.in/u/iniGVSnw#item=2":             "https://pixeldrain.com/u/iniGVSnw",
		"https://pixeldrain.com/api/file/iniGVSnw":          "https://pixeldrain.com/u/iniGVSnw",
		"https://pixeldrain.com/api/file/iniGVSnw?download": "https://pixeldrain.com/u/iniGVSnw",
		"https://pixeldrain.com/l/CcFZTyZ4":                 "https://pixeldrain.com/l/CcFZTyZ4",
		"https://pixeldrain.dev/l/CcFZTyZ4#item=1":          "https://pixeldrain.com/l/CcFZTyZ4#item=1",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
		if !(Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"https://pixeldrain.com/",
		"https://pixeldrain.com/d/abcdefgh/file.zip",
		"https://pixeldrain.com/api/file/iniGVSnw/thumbnail",
		"https://example.com/u/iniGVSnw",
		"ftp://pixeldrain.com/u/iniGVSnw",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	srv := pdtest.New(t)
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}
	res, err := r.Resolve(context.Background(), resolver.Request{URL: "https://pixeldrain.net/u/iniGVSnw"})
	if err != nil || res.DirectURL != "https://pixeldrain.com/u/iniGVSnw" {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
	if len(srv.Paths()) != 0 {
		t.Errorf("Resolve asked Pixeldrain %v", srv.Paths())
	}
	if _, err := r.Resolve(context.Background(), resolver.Request{URL: "https://pixeldrain.com/l/CcFZTyZ4"}); err == nil {
		t.Error("a list link resolved to one download")
	}
}

func TestOneInfoRequestNamesSizesAndChecksEveryFile(t *testing.T) {
	srv := pdtest.New(t)
	srv.AddFile("iniGVSnw", pdtest.File{Name: "fide-archive-fide-2001-2019.zip", Body: make([]byte, 1757)})
	srv.AddFile("BvnwvXuV", pdtest.File{Name: "taken.zip", Body: make([]byte, 3), AbuseType: "copyright"})
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}

	got := r.Inspect(context.Background(), []string{
		"https://pixeldrain.com/u/iniGVSnw",
		"https://pixeldrain.com/l/CcFZTyZ4",
		"https://pixeldrain.com/u/zzzzzzzz",
		"https://pixeldrain.com/u/BvnwvXuV",
	})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "fide-archive-fide-2001-2019.zip", Size: 1757},
		{},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOffline},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
	if paths := srv.Paths(); !slices.Equal(paths, []string{"/api/file/iniGVSnw,zzzzzzzz,BvnwvXuV/info"}) {
		t.Errorf("asked %v, want one request for the three files", paths)
	}
}

func TestASingleMissingFileIsOffline(t *testing.T) {
	srv := pdtest.New(t)
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}
	got := r.Inspect(context.Background(), []string{"https://pixeldrain.com/u/zzzzzzzz"})
	if got[0].Availability != core.AvailOffline {
		t.Errorf("Inspect = %+v, want offline", got)
	}
}

func TestAListOpensIntoItsFilesUnderItsTitle(t *testing.T) {
	srv := pdtest.New(t)
	srv.AddFile("iniGVSnw", pdtest.File{Name: "2001-2019.zip", Body: make([]byte, 10)})
	srv.AddFile("BvnwvXuV", pdtest.File{Name: "2020-2023.zip", Body: make([]byte, 20)})
	srv.AddList("CcFZTyZ4", pdtest.List{Title: "FIDE rating-list archive", Files: []string{"iniGVSnw", "BvnwvXuV"}})
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}

	got, err := r.List(context.Background(), "https://pixeldrain.com/l/CcFZTyZ4", "")
	want := []resolver.Listing{
		{URL: "https://pixeldrain.com/u/iniGVSnw", Name: "2001-2019.zip", Size: 10, Folder: "FIDE rating-list archive"},
		{URL: "https://pixeldrain.com/u/BvnwvXuV", Name: "2020-2023.zip", Size: 20, Folder: "FIDE rating-list archive"},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("List = %+v, %v; want %+v", got, err, want)
	}

	one, err := r.List(context.Background(), "https://pixeldrain.com/l/CcFZTyZ4#item=1", "")
	if err != nil || len(one) != 1 || one[0].Name != "2020-2023.zip" {
		t.Errorf("a link to the second item listed %+v, %v", one, err)
	}
	if files, err := r.List(context.Background(), "https://pixeldrain.com/u/iniGVSnw", ""); err != nil || files != nil {
		t.Errorf("a file link listed %+v, %v; want nothing", files, err)
	}
	if _, err := r.List(context.Background(), "https://pixeldrain.com/l/zzzzzzzz", ""); !hoster.IsGone(err) {
		t.Errorf("a missing list gave %v, want gone", err)
	}
}

func TestAFreeDownloadGetsOneConnectionAndNoKey(t *testing.T) {
	srv := pdtest.New(t)
	srv.AddFile("iniGVSnw", pdtest.File{Name: "a.zip", Body: []byte("PK\x03\x04")})
	c := NewClient(srv.URL, srv.Client())

	l, err := c.Unlock(context.Background(), "https://pixeldrain.com/u/iniGVSnw", "")
	if err != nil {
		t.Fatal(err)
	}
	if l.URL != srv.URL+"/file/iniGVSnw?download" || l.Conns != 1 || l.Name != "a.zip" || l.Size != 4 {
		t.Errorf("Unlock = %+v, want the download address on one connection", l)
	}
	if len(l.Headers) != 0 || slices.ContainsFunc(srv.Auths(), func(a string) bool { return a != "" }) {
		t.Errorf("an anonymous download sent a key: %v %v", l.Headers, srv.Auths())
	}
}

func TestAStoredKeyGoesAlongAndAPaidPlanLiftsTheConnectionCap(t *testing.T) {
	srv := pdtest.New(t)
	srv.AddFile("iniGVSnw", pdtest.File{Name: "a.zip", Body: []byte("PK")})
	c := NewClient(srv.URL, srv.Client())
	c.Key = func() string { return pdtest.Key }
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+pdtest.Key))

	l, err := c.Unlock(context.Background(), "https://pixeldrain.com/u/iniGVSnw", "")
	if err != nil || l.Headers["Authorization"] != auth || l.Conns != 1 {
		t.Errorf("free account: Unlock = %+v, %v; want the key on one connection", l, err)
	}

	srv.Plan("prepaid")
	c = NewClient(srv.URL, srv.Client())
	c.Key = func() string { return pdtest.Key }
	l, err = c.Unlock(context.Background(), "https://pixeldrain.com/u/iniGVSnw", "")
	if err != nil || l.Conns != 0 {
		t.Errorf("paid account: Unlock = %+v, %v; want no cap of Pixeldrain's own", l, err)
	}
}

func TestARejectedKeyFailsAsAuth(t *testing.T) {
	srv := pdtest.New(t)
	srv.AddFile("iniGVSnw", pdtest.File{Name: "a.zip", Body: []byte("PK")})
	c := NewClient(srv.URL, srv.Client())
	c.Key = func() string { return "not-the-key" }
	_, err := c.Unlock(context.Background(), "https://pixeldrain.com/u/iniGVSnw", "")
	var r *hoster.Refusal
	if !errors.As(err, &r) || r.Reason != core.ReasonAuth {
		t.Errorf("Unlock = %v, want an auth refusal", err)
	}
}

func TestEveryWayPixeldrainHoldsADownloadBackIsTold(t *testing.T) {
	cases := map[string]struct {
		file   pdtest.File
		refuse string
		reason core.Reason
		wait   bool
	}{
		"gone":             {file: pdtest.File{}, reason: core.ReasonGone},
		"taken down":       {file: pdtest.File{AbuseType: "copyright"}, reason: core.ReasonGone},
		"rate limited":     {file: pdtest.File{Availability: "file_rate_limited_captcha_required"}, reason: core.ReasonUnavailable},
		"hotlink":          {refuse: "hotlink_detected", reason: core.ReasonUnavailable},
		"daily limit":      {refuse: "transfer_limit_exceeded", reason: core.ReasonLimit, wait: true},
		"too many at once": {refuse: "max_concurrent_downloads", reason: core.ReasonLimit, wait: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := pdtest.New(t)
			if name != "gone" {
				tc.file.Name, tc.file.Body = "a.zip", []byte("PK")
				srv.AddFile("iniGVSnw", tc.file)
			}
			srv.Refuse(tc.refuse)
			_, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), "https://pixeldrain.com/u/iniGVSnw", "")
			var w *hoster.WaitError
			var r *hoster.Refusal
			switch {
			case tc.wait && errors.As(err, &w):
				if w.Reason != tc.reason || w.Wait <= 0 || w.Why == "" {
					t.Errorf("wait %+v, want a reasoned wait failing as %q", w, tc.reason)
				}
			case !tc.wait && errors.As(err, &r):
				if r.Reason != tc.reason {
					t.Errorf("refusal %+v, want %q", r, tc.reason)
				}
			default:
				t.Errorf("Unlock = %v", err)
			}
		})
	}
}
