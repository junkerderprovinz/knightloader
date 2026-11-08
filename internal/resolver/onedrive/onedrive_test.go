package onedrive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/onedrive/odtest"
)

func fake(t *testing.T) (*odtest.Server, Resolver) {
	srv := odtest.New(t)
	return srv, Resolver{Client: NewClient(srv.URL, srv.Client())}
}

func TestEveryShareLinkFormIsClaimed(t *testing.T) {
	for _, raw := range []string{
		"https://1drv.ms/u/s!AjTsLJdE37DwtrsnIehxKx7N7XgoBg?e=r1sszn",
		"https://1drv.ms/f/s!AgaP37NGYuEXhRfQxHRseR7eSxXo",
		"https://1drv.ms/u/c/f029f6f5a52c17c4/ETR7ogx7YCtBgtDu66a4R14B7RKLZJoz20D4Z5I1KD6HTg?e=404kKg",
		"https://onedrive.live.com/:u:/g/personal/F029F6F5A52C17C4/ETR7ogx7YCtBgtDu66a4R14B7RKLZJoz20D4Z5I1KD6HTg?e=404kKg",
		"https://onedrive.live.com/redir?resid=F0B0DF44972CEC34!892327&authkey=!AIehxKx7N7XgoBg",
		"https://ucsdcloud-my.sharepoint.com/:u:/g/personal/h3dong_ucsd_edu/Ea2kDl2YokNIiZ_OXxzgclYBX0Tk-awE2kDlm2HO9ofqPg",
		"https://gohkust-my.sharepoint.com/:f:/g/personal/imyiyang_ust_hk/EksJcamJpclJlbMweFfB5DQB1XrsxURYN5GSqZw3jmSeSw?e=KAyhsX",
		"https://contoso.sharepoint.com/:b:/s/Marketing/EZtJk0v3bQ5Fq?e=4abc",
	} {
		if !(Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"https://onedrive.live.com/",
		"https://onedrive.live.com/about/en-us/",
		"https://contoso.sharepoint.com/sites/Marketing/SitePages/Home.aspx",
		"https://1drv.ms/",
		"https://example.com/:u:/g/personal/x/token",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func fetch(t *testing.T, srv *odtest.Server, l hoster.Link) []byte {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, l.URL, nil)
	for k, v := range l.Headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}

func TestAPersonalFileShareDownloadsWithItsGuestSession(t *testing.T) {
	srv, r := fake(t)
	body := bytes.Repeat([]byte("zip"), 500)
	srv.File("/personal/f029f6f5a52c17c4/Documents/DVD_share/LVBench_4.1_nomatrix.zip", body)
	link := srv.Add("ETR7ogx7YCtBgtDu66a4R14B7RKLZJoz20D4Z5I1KD6HTg", odtest.Share{Path: "/personal/f029f6f5a52c17c4/Documents/DVD_share/LVBench_4.1_nomatrix.zip"})

	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "LVBench_4.1_nomatrix.zip" || l.Size != int64(len(body)) || !strings.Contains(l.Headers["Cookie"], "FedAuth=") {
		t.Errorf("Unlock = %+v, want the file with the guest cookie", l)
	}
	if got := fetch(t, srv, l); !bytes.Equal(got, body) {
		t.Error("the link fetched something else than the file")
	}
	if asked := srv.Asked(); !strings.Contains(asked[0], "download=1") {
		t.Errorf("asked %v, want the share link with download=1", asked)
	}
}

func TestABusinessFileShareIsNamedAfterItsPath(t *testing.T) {
	srv, r := fake(t)
	srv.File("/personal/h3dong_ucsd_edu/Documents/data/musegan/pretrained_models.tar.gz", []byte("\x1f\x8b"))
	link := srv.Add("Ea2kDl2YokNIiZ_OXxzgclYBX0Tk-awE2kDlm2HO9ofqPg", odtest.Share{Business: true, Path: "/personal/h3dong_ucsd_edu/Documents/data/musegan/pretrained_models.tar.gz"})

	got := r.Inspect(context.Background(), []string{link})
	if want := (resolver.LinkInfo{Availability: core.AvailOnline, Name: "pretrained_models.tar.gz", Size: 2}); got[0] != want {
		t.Errorf("Inspect = %+v, want %+v", got[0], want)
	}
}

func TestEveryWayAShareHoldsBackIsTold(t *testing.T) {
	srv, r := fake(t)
	gone := srv.Add("gonegonegone", odtest.Share{Path: "/personal/aa/Documents/x.zip", Gone: true})
	login := srv.Add("loginlogin", odtest.Share{Business: true, Path: "/personal/bb/Documents/x.zip", Login: true})
	locked := srv.Add("lockedlocked", odtest.Share{Business: true, Path: "/personal/cc/Documents/x.zip", Password: true})

	if _, err := r.Client.Unlock(context.Background(), gone, ""); !hoster.IsGone(err) {
		t.Errorf("a removed share gave %v, want gone", err)
	}
	for _, link := range []string{login, locked} {
		_, err := r.Client.Unlock(context.Background(), link, "")
		var ref *hoster.Refusal
		if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
			t.Errorf("%s gave %v, want a refusal", link, err)
		}
	}
	for _, asked := range srv.Asked() {
		if strings.Contains(asked, "login.") {
			t.Errorf("the client went on to a sign-in page: %s", asked)
		}
	}
}

func TestAFolderShareOpensIntoEveryFileOfItsSubfolders(t *testing.T) {
	for _, business := range []bool{false, true} {
		srv, r := fake(t)
		root := "/personal/17e16246b3df8f06/Documents/ShuffleNetSeries"
		srv.File(root+"/README.md", []byte("readme"))
		srv.File(root+"/ShuffleNetV2+/models/ShuffleNetV2+.Small.pth.tar", make([]byte, 7))
		srv.File(root+"/O'Brien/notes.txt", make([]byte, 3))
		link := srv.Add("AgaP37NGYuEXhRfQxHRseR7eSxXo", odtest.Share{Folder: true, Business: business, Path: root})

		got, err := r.List(context.Background(), link, "")
		want := []resolver.Listing{
			{URL: link + "#path=README.md", Name: "README.md", Size: 6, Folder: "ShuffleNetSeries"},
			{URL: link + "#path=O%27Brien/notes.txt", Name: "notes.txt", Size: 3, Folder: "ShuffleNetSeries/O'Brien"},
			{URL: link + "#path=ShuffleNetV2+/models/ShuffleNetV2+.Small.pth.tar", Name: "ShuffleNetV2+.Small.pth.tar", Size: 7, Folder: "ShuffleNetSeries/ShuffleNetV2+/models"},
		}
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("business %v: List = %+v, %v; want %+v", business, got, err, want)
		}

		l, err := r.Client.Unlock(context.Background(), got[2].URL, "")
		if err != nil || l.Name != "ShuffleNetV2+.Small.pth.tar" || l.Size != 7 {
			t.Fatalf("business %v: the folder's file unlocked as %+v, %v", business, l, err)
		}
		if body := fetch(t, srv, l); len(body) != 7 {
			t.Errorf("business %v: the file fetched %d bytes", business, len(body))
		}
		if info := r.Inspect(context.Background(), []string{got[0].URL, got[1].URL}); info[1].Name != "notes.txt" || info[0].Availability != core.AvailOnline {
			t.Errorf("business %v: Inspect = %+v", business, info)
		}
		redeemed := 0
		for _, asked := range srv.Asked() {
			if strings.HasPrefix(asked, "/:f:/") {
				redeemed++
			}
		}
		if redeemed != 1 {
			t.Errorf("business %v: the share was redeemed %d times, want once for the listing and its files", business, redeemed)
		}
	}
}

func TestAnOlderLinkHandingOutTheFileStraightAwayIsTakenAsIs(t *testing.T) {
	srv, r := fake(t)
	link := srv.Direct("F0B0DF44972CEC34!892327", []byte("7z"))
	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil || l.Name != "old share.7z" || l.Size != 2 {
		t.Fatalf("Unlock = %+v, %v", l, err)
	}
	if got := fetch(t, srv, l); string(got) != "7z" {
		t.Errorf("the link fetched %q", got)
	}
}

func TestAFolderShareIsNotOneDownload(t *testing.T) {
	_, r := fake(t)
	if _, err := r.Resolve(context.Background(), resolver.Request{URL: "https://1drv.ms/f/s!AgaP37NGYuEXhRfQxHRseR7eSxXo"}); err == nil {
		t.Error("a folder share resolved to one download")
	}
	if files, err := r.List(context.Background(), "https://1drv.ms/u/s!AjTsLJdE37DwtrsnIehxKx7N7XgoBg", ""); err != nil || files != nil {
		t.Errorf("a file share listed %+v, %v; want nothing", files, err)
	}
}
