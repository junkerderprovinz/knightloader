package dropbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/dropbox/dbtest"
)

func fake(t *testing.T) (*dbtest.Server, Resolver) {
	srv := dbtest.New(t)
	c := NewClient(srv.URL, srv.Client())
	c.settle = 0
	return srv, Resolver{Client: c}
}

func TestEveryLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.dropbox.com/scl/fi/hzfx1l267m8gwyhcjvfk4/Quantization-Cuda-vs-Triton.pdf?rlkey=inventedfilekey&dl=0": "https://www.dropbox.com/scl/fi/hzfx1l267m8gwyhcjvfk4/Quantization-Cuda-vs-Triton.pdf?rlkey=inventedfilekey",
		"https://dropbox.com/scl/fi/hzfx1l267m8gwyhcjvfk4/x.pdf?dl=1&rlkey=inventedfilekey&st=abc123":                     "https://www.dropbox.com/scl/fi/hzfx1l267m8gwyhcjvfk4/x.pdf?rlkey=inventedfilekey&st=abc123",
		"https://www.dropbox.com/s/5h5bnwzklsev6ch/1mb.test?raw=1":                                                        "https://www.dropbox.com/s/5h5bnwzklsev6ch/1mb.test",
		"https://dl.dropbox.com/s/5h5bnwzklsev6ch/1mb.test":                                                               "https://www.dropbox.com/s/5h5bnwzklsev6ch/1mb.test",
		"https://www.dropbox.com/scl/fo/y3naz62gy6f6qfrhquu7u/h?rlkey=inventedfolderkey&dl=0":                             "https://www.dropbox.com/scl/fo/y3naz62gy6f6qfrhquu7u/h?rlkey=inventedfolderkey",
		"https://www.dropbox.com/sh/abcdefghijk/AABBccDDeeFF?dl=0":                                                        "https://www.dropbox.com/sh/abcdefghijk/AABBccDDeeFF",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://www.dropbox.com/",
		"https://www.dropbox.com/home/Photos",
		"https://www.dropbox.com/sc/gallery123/abc",
		"https://dl.dropboxusercontent.com/s/5h5bnwzklsev6ch/1mb.test",
		"https://www.dropboxforum.com/s/5h5bnwzklsev6ch/1mb.test",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	srv, r := fake(t)
	res, err := r.Resolve(context.Background(), resolver.Request{URL: "https://www.dropbox.com/s/5h5bnwzklsev6ch/1mb.test?dl=0"})
	if err != nil || res.DirectURL != "https://www.dropbox.com/s/5h5bnwzklsev6ch/1mb.test" || res.Connections != maxConns {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
	if len(srv.Asked()) != 0 {
		t.Errorf("Resolve asked Dropbox %v", srv.Asked())
	}
}

func fetch(t *testing.T, srv *dbtest.Server, l hoster.Link) []byte {
	t.Helper()
	resp, err := srv.Client().Get(l.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}

func TestAFileIsDownloadedFromWhereItsDl1LinkLeads(t *testing.T) {
	srv, r := fake(t)
	body := bytes.Repeat([]byte("%PDF"), 300)
	link := srv.AddFile("hzfx1l267m8gwyhcjvfk4", "inventedfilekey", dbtest.Share{File: &dbtest.File{Name: "Quantization Cuda vs Triton.pdf", Body: body}})

	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "Quantization Cuda vs Triton.pdf" || l.Size != int64(len(body)) || l.Conns != maxConns || !strings.Contains(l.URL, "/content/cd/0/get/") {
		t.Errorf("Unlock = %+v", l)
	}
	if got := fetch(t, srv, l); !bytes.Equal(got, body) {
		t.Error("the link fetched something else than the file")
	}
	if asked := srv.Asked(); !strings.Contains(asked[0], "dl=1") || !strings.Contains(asked[0], "rlkey=inventedfilekey") {
		t.Errorf("asked %v, want the link with dl=1 and its key", asked)
	}
}

func TestAPasswordFromTheLinkOpensALockedFile(t *testing.T) {
	srv, r := fake(t)
	link := srv.AddFile("lockedlockedlocked0", "k", dbtest.Share{File: &dbtest.File{Name: "secret.zip", Body: []byte("PK")}, Password: "hunter2"})

	_, err := r.Client.Unlock(context.Background(), link, "")
	var ref *hoster.Refusal
	if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("without a password Unlock = %v, want a refusal asking for one", err)
	}
	if _, err := r.Client.Unlock(context.Background(), link, "wrong"); !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("with a wrong password Unlock = %v, want a refusal", err)
	}
	l, err := r.Client.Unlock(context.Background(), link, "hunter2")
	if err != nil || l.Name != "secret.zip" {
		t.Fatalf("Unlock = %+v, %v", l, err)
	}
	if got := fetch(t, srv, l); string(got) != "PK" {
		t.Errorf("the link fetched %q", got)
	}
}

func TestEveryWayDropboxHoldsALinkBackIsTold(t *testing.T) {
	srv, r := fake(t)
	busy := srv.AddFile("busybusybusybusy00", "k", dbtest.Share{File: &dbtest.File{Name: "popular.mkv", Body: []byte("x")}, Disabled: true})

	_, err := r.Client.Unlock(context.Background(), busy, "")
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Reason != core.ReasonLimit || w.Wait != disabledWait {
		t.Errorf("a disabled link gave %v, want a wait", err)
	}
	if _, err := r.Client.Unlock(context.Background(), "https://www.dropbox.com/scl/fi/zzzzzzzzzzzzzzzzzzzzz/none.pdf?rlkey=zzz&dl=0", ""); !hoster.IsGone(err) {
		t.Errorf("an invalid link gave %v, want gone", err)
	}
}

func TestInspectNamesAndSizesFilesAndKnowsInvalidLinks(t *testing.T) {
	srv, r := fake(t)
	link := srv.AddFile("hzfx1l267m8gwyhcjvfk4", "k", dbtest.Share{File: &dbtest.File{Name: "slides.pdf", Body: make([]byte, 77)}})
	folder := srv.AddFolder("y3naz62gy6f6qfrhquu7u", "k", dbtest.Share{Folder: &dbtest.Folder{Name: "shared"}})

	got := r.Inspect(context.Background(), []string{link, folder, "https://www.dropbox.com/scl/fi/zzzzzzzzzzzzzzzzzzzzz/none.pdf?rlkey=zzz"})
	want := []resolver.LinkInfo{{Availability: core.AvailOnline, Name: "slides.pdf", Size: 77}, {}, {Availability: core.AvailOffline}}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
}

func swisseph() *dbtest.Folder {
	return &dbtest.Folder{
		Name: "shared",
		Files: []dbtest.File{
			{Name: "seas_18.se1", Body: make([]byte, 5)},
			{Name: "semo_18.se1", Body: make([]byte, 6)},
			{Name: "sepl_18.se1", Body: make([]byte, 7)},
		},
		Folders: []dbtest.Folder{
			{Name: "jpl binary files", Files: []dbtest.File{{Name: "de441.eph", Body: make([]byte, 8)}}},
		},
	}
}

func TestAFolderOpensIntoEveryFileOfItsSubfoldersPageByPage(t *testing.T) {
	srv, r := fake(t)
	link := srv.AddFolder("y3naz62gy6f6qfrhquu7u", "inventedfolderkey", dbtest.Share{Folder: swisseph()})
	srv.Page(2)

	got, err := r.List(context.Background(), link, "")
	base := "https://www.dropbox.com/scl/fo/y3naz62gy6f6qfrhquu7u/h/"
	key := "?rlkey=inventedfolderkey"
	want := []resolver.Listing{
		{URL: base + "seas_18.se1" + key, Name: "seas_18.se1", Size: 5, Folder: "shared"},
		{URL: base + "semo_18.se1" + key, Name: "semo_18.se1", Size: 6, Folder: "shared"},
		{URL: base + "sepl_18.se1" + key, Name: "sepl_18.se1", Size: 7, Folder: "shared"},
		{URL: base + "jpl%20binary%20files/de441.eph" + key, Name: "de441.eph", Size: 8, Folder: "shared/jpl binary files"},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("List = %+v, %v; want %+v", got, err, want)
	}
	l, err := r.Client.Unlock(context.Background(), got[3].URL, "")
	if err != nil || l.Name != "de441.eph" || l.Size != 8 {
		t.Errorf("the subfolder's file unlocked as %+v, %v", l, err)
	}
	if files, err := r.List(context.Background(), "https://www.dropbox.com/scl/fi/hzfx1l267m8gwyhcjvfk4/x.pdf?rlkey=k", ""); err != nil || files != nil {
		t.Errorf("a file link listed %+v, %v; want nothing", files, err)
	}
	if _, err := r.List(context.Background(), "https://www.dropbox.com/scl/fo/zzzzzzzzzzzzzzzzzzzzz/h?rlkey=zzz", ""); !hoster.IsGone(err) {
		t.Errorf("an invalid folder link gave %v, want gone", err)
	}
}

func TestALockedFolderOpensWithTheLinkPassword(t *testing.T) {
	srv, r := fake(t)
	link := srv.AddFolder("lockedlockedlocked0", "k", dbtest.Share{Folder: swisseph(), Password: "hunter2"})

	if _, err := r.List(context.Background(), link, ""); err == nil {
		t.Error("a locked folder listed without its password")
	}
	got, err := r.List(context.Background(), link, "hunter2")
	if err != nil || len(got) != 4 {
		t.Errorf("List = %+v, %v; want the folder's four files", got, err)
	}
}

func TestAFolderLinkDownloadsAsTheZipDropboxMakesOfIt(t *testing.T) {
	srv, r := fake(t)
	link := srv.AddFolder("y3naz62gy6f6qfrhquu7u", "k", dbtest.Share{Folder: swisseph()})
	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil || l.Name != "shared.zip" {
		t.Fatalf("Unlock = %+v, %v", l, err)
	}
	if got := fetch(t, srv, l); !bytes.HasPrefix(got, []byte("PK\x03\x04")) {
		t.Errorf("the folder fetched %q", got)
	}
}
