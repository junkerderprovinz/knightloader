package mediafire

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
	"github.com/junkerderprovinz/knightloader/internal/resolver/mediafire/mftest"
)

func TestEveryDomainAndLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.mediafire.com/file/wt7dc5e9jgnym04/Poses.tar.gz/file": "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"http://mediafire.com/file/wt7dc5e9jgnym04":                        "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"https://app.mediafire.com/view/wt7dc5e9jgnym04/Poses.tar.gz":      "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"https://www.mediafire.com/download/wt7dc5e9jgnym04":               "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"https://www.mediafire.com/file_premium/wt7dc5e9jgnym04/x.zip":     "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"https://www.mediafire.com/?wt7dc5e9jgnym04":                       "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"https://mfi.re/file/wt7dc5e9jgnym04":                              "https://www.mediafire.com/file/wt7dc5e9jgnym04",
		"https://www.mediafire.com/folder/iuf7lue8r578c/a-culfw":           "https://www.mediafire.com/folder/iuf7lue8r578c",
		"https://www.mediafire.com/folder/h14iarbs62e7p/shared":            "https://www.mediafire.com/folder/h14iarbs62e7p",
		"https://www.mediafire.com/?iuf7lue8r578c":                         "https://www.mediafire.com/folder/iuf7lue8r578c",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://www.mediafire.com/",
		"https://www.mediafire.com/upgrade/",
		"https://download1530.mediafire.com/6wkrnlu3vceg/wt7dc5e9jgnym04/Poses.tar.gz",
		"https://example.com/file/wt7dc5e9jgnym04",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	srv := mftest.New(t)
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}
	res, err := r.Resolve(context.Background(), resolver.Request{URL: "https://mfi.re/file/wt7dc5e9jgnym04"})
	if err != nil || res.DirectURL != "https://www.mediafire.com/file/wt7dc5e9jgnym04" || res.Connections != maxConns {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
	if len(srv.Asked()) != 0 {
		t.Errorf("Resolve asked Mediafire %v", srv.Asked())
	}
	if _, err := r.Resolve(context.Background(), resolver.Request{URL: "https://www.mediafire.com/folder/iuf7lue8r578c"}); err == nil {
		t.Error("a folder link resolved to one download")
	}
}

func TestOneInfoRequestNamesSizesAndChecksEveryFile(t *testing.T) {
	srv := mftest.New(t)
	srv.AddFile("wt7dc5e9jgnym04", mftest.File{Name: "Poses.tar.gz", Body: make([]byte, 405)})
	srv.AddFile("90ckx8l648kktcv", mftest.File{Name: "old.zip", Body: make([]byte, 9), Deleted: true})
	r := Resolver{Client: NewClient(srv.URL, srv.Client())}

	got := r.Inspect(context.Background(), []string{
		"https://www.mediafire.com/file/wt7dc5e9jgnym04/Poses.tar.gz/file",
		"https://www.mediafire.com/folder/iuf7lue8r578c",
		"https://www.mediafire.com/file/zzzzzzzzzzzzzzz",
		"https://www.mediafire.com/file/90ckx8l648kktcv",
	})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "Poses.tar.gz", Size: 405},
		{},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOffline},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
	if asked := srv.Asked(); len(asked) != 1 || !strings.Contains(asked[0], "quick_key=wt7dc5e9jgnym04%2Czzzzzzzzzzzzzzz%2C90ckx8l648kktcv") {
		t.Errorf("asked %v, want one request for the three files", asked)
	}
}

func TestASingleUnknownFileIsOffline(t *testing.T) {
	srv := mftest.New(t)
	got := Resolver{Client: NewClient(srv.URL, srv.Client())}.Inspect(context.Background(), []string{"https://www.mediafire.com/file/zzzzzzzzzzzzzzz"})
	if got[0].Availability != core.AvailOffline {
		t.Errorf("Inspect = %+v, want offline", got)
	}
}

func TestAFolderOpensIntoEveryFileOfItsSubfoldersPageByPage(t *testing.T) {
	srv := mftest.New(t)
	srv.AddFile("90ckx8l648kktcv", mftest.File{Name: "build_171.zip", Body: make([]byte, 3)})
	srv.AddFile("a1b2c3d4e5f6g7h", mftest.File{Name: "build_170.zip", Body: make([]byte, 2)})
	srv.AddFile("q9w8e7r6t5y4u3i", mftest.File{Name: "photo.jpg", Body: make([]byte, 1)})
	srv.AddFolder("dcoqqd5idwrrs", mftest.Folder{Name: "Photographs", Files: []string{"q9w8e7r6t5y4u3i"}})
	link := srv.AddFolder("iuf7lue8r578c", mftest.Folder{Name: "a-culfw", Files: []string{"90ckx8l648kktcv", "a1b2c3d4e5f6g7h"}, Folders: []string{"dcoqqd5idwrrs"}})
	srv.Chunk(1)

	got, err := Resolver{Client: NewClient(srv.URL, srv.Client())}.List(context.Background(), link, "")
	want := []resolver.Listing{
		{URL: "https://www.mediafire.com/file/a1b2c3d4e5f6g7h/build_170.zip/file", Name: "build_170.zip", Size: 2, Folder: "a-culfw"},
		{URL: "https://www.mediafire.com/file/90ckx8l648kktcv/build_171.zip/file", Name: "build_171.zip", Size: 3, Folder: "a-culfw"},
		{URL: "https://www.mediafire.com/file/q9w8e7r6t5y4u3i/photo.jpg/file", Name: "photo.jpg", Size: 1, Folder: "a-culfw/Photographs"},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("List = %+v, %v; want %+v", got, err, want)
	}
	if files, err := (Resolver{Client: NewClient(srv.URL, srv.Client())}).List(context.Background(), "https://www.mediafire.com/file/90ckx8l648kktcv", ""); err != nil || files != nil {
		t.Errorf("a file link listed %+v, %v; want nothing", files, err)
	}
	if _, err := (Resolver{Client: NewClient(srv.URL, srv.Client())}).List(context.Background(), "https://www.mediafire.com/folder/zzzzzzzzzzzzz", ""); !hoster.IsGone(err) {
		t.Errorf("an unknown folder gave %v, want gone", err)
	}
}

func TestTheDownloadButtonOfTheFilePageIsTheLink(t *testing.T) {
	srv := mftest.New(t)
	link := srv.AddFile("wt7dc5e9jgnym04", mftest.File{Name: "Poses.tar.gz", Body: []byte("\x1f\x8b\x08\x00payload")})

	l, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(l.URL, "/download/1530/6wkrnlu3vceg/wt7dc5e9jgnym04/Poses.tar.gz") || l.Conns != maxConns {
		t.Fatalf("Unlock = %+v", l)
	}
	resp, err := srv.Client().Get(l.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "\x1f\x8b\x08\x00payload" {
		t.Errorf("the link fetched %d %q", resp.StatusCode, body)
	}
}

func TestEveryWayTheFilePageHoldsADownloadBackIsTold(t *testing.T) {
	cases := map[mftest.Page]struct {
		reason core.Reason
		wait   time.Duration
	}{
		mftest.PageLimit:       {reason: core.ReasonLimit, wait: 754 * time.Second},
		mftest.PageUnavailable: {reason: core.ReasonUnavailable, wait: 45 * time.Second},
		mftest.PageRemoved:     {reason: core.ReasonGone},
		mftest.PagePassword:    {reason: core.ReasonAuth},
		mftest.PageCaptcha:     {reason: core.ReasonLimit},
		mftest.PageMalware:     {reason: core.ReasonUnavailable},
	}
	for page, tc := range cases {
		srv := mftest.New(t)
		link := srv.AddFile("wt7dc5e9jgnym04", mftest.File{Name: "Poses.tar.gz", Body: []byte("x"), Page: page})
		_, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), link, "")
		var w *hoster.WaitError
		var r *hoster.Refusal
		switch {
		case tc.wait > 0 && errors.As(err, &w):
			if w.Wait != tc.wait || w.Reason != tc.reason || w.Why == "" {
				t.Errorf("page %d: wait %+v, want %s failing as %q", page, w, tc.wait, tc.reason)
			}
		case tc.wait == 0 && errors.As(err, &r):
			if r.Reason != tc.reason {
				t.Errorf("page %d: refusal %+v, want %q", page, r, tc.reason)
			}
		default:
			t.Errorf("page %d: Unlock = %v", page, err)
		}
	}
}
