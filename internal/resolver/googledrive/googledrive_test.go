package googledrive

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
	"github.com/junkerderprovinz/knightloader/internal/resolver/googledrive/gdtest"
)

func fake(t *testing.T) (*gdtest.Server, Resolver) {
	srv := gdtest.New(t)
	sites := Sites{Drive: srv.URL, Content: srv.URL, API: srv.URL, Docs: srv.URL}
	return srv, Resolver{Client: NewClient(sites, srv.Client())}
}

func TestEveryLinkFormIsClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://drive.google.com/file/d/1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ/view?usp=sharing":             "https://drive.google.com/file/d/1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ",
		"https://drive.google.com/uc?id=1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ&export=download":               "https://drive.google.com/file/d/1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ",
		"https://drive.google.com/open?id=0B9P1L--7Wd2vU3VUVlFnbTgtS2c":                                  "https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c",
		"https://drive.usercontent.google.com/download?id=0B9P1L--7Wd2vU3VUVlFnbTgtS2c&export=download":  "https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c",
		"https://docs.google.com/uc?export=download&id=0B9P1L--7Wd2vU3VUVlFnbTgtS2c":                     "https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c",
		"https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c/view?resourcekey=0-abcDEF_1":       "https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c?resourcekey=0-abcDEF_1",
		"https://drive.google.com/drive/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl?usp=sharing":           "https://drive.google.com/drive/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl",
		"https://drive.google.com/drive/u/1/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl":                   "https://drive.google.com/drive/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl",
		"https://drive.google.com/embeddedfolderview?id=15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl#list":          "https://drive.google.com/drive/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl",
		"https://docs.google.com/document/d/1TFYNzuZJTgNGzGmjraZ58ZVOh9_YoKeBnU-opWgXQL4/edit":           "https://docs.google.com/document/d/1TFYNzuZJTgNGzGmjraZ58ZVOh9_YoKeBnU-opWgXQL4",
		"https://docs.google.com/spreadsheets/d/1h6wQX7ATSJDOSWFEjHPmv_nukJzZD_zZ30Jvy6XNiTE/edit#gid=0": "https://docs.google.com/spreadsheets/d/1h6wQX7ATSJDOSWFEjHPmv_nukJzZD_zZ30Jvy6XNiTE",
		"https://docs.google.com/presentation/d/1h6wQX7ATSJDOSWFEjHPmv_nukJzZD_zZ30Jvy6XNiTE/pub":        "https://docs.google.com/presentation/d/1h6wQX7ATSJDOSWFEjHPmv_nukJzZD_zZ30Jvy6XNiTE",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://drive.google.com/",
		"https://drive.google.com/drive/my-drive",
		"https://docs.google.com/forms/d/e/1FAIpQLSf/viewform",
		"https://www.google.com/file/d/1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	srv, r := fake(t)
	res, err := r.Resolve(context.Background(), resolver.Request{URL: "https://drive.google.com/open?id=0B9P1L--7Wd2vU3VUVlFnbTgtS2c"})
	if err != nil || res.DirectURL != "https://drive.google.com/file/d/0B9P1L--7Wd2vU3VUVlFnbTgtS2c" || res.Connections != maxConns {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
	if len(srv.Asked()) != 0 {
		t.Errorf("Resolve asked Drive %v", srv.Asked())
	}
	if _, err := r.Resolve(context.Background(), resolver.Request{URL: "https://drive.google.com/drive/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl"}); err == nil {
		t.Error("a folder link resolved to one download")
	}
}

func fetch(t *testing.T, hc *http.Client, l hoster.Link) []byte {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, l.URL, nil)
	for k, v := range l.Headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}

func TestASmallFileIsDownloadedFromDrivesDownloadServer(t *testing.T) {
	srv, r := fake(t)
	link := srv.Add("0B9P1L--7Wd2vU3VUVlFnbTgtS2c", gdtest.Item{Name: "spam.txt", Body: []byte("spam\n")})

	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "spam.txt" || l.Size != 5 || l.Conns != maxConns || !strings.Contains(l.URL, "/download?") {
		t.Errorf("Unlock = %+v", l)
	}
	if got := fetch(t, srv.Client(), l); string(got) != "spam\n" {
		t.Errorf("the link fetched %q", got)
	}
}

func TestALargeFileIsDownloadedPastTheVirusScanWarning(t *testing.T) {
	srv, r := fake(t)
	body := bytes.Repeat([]byte("npz"), 1000)
	link := srv.Add("1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ", gdtest.Item{Name: "fcn8s_from_caffe.npz", Body: body, Big: true})

	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "fcn8s_from_caffe.npz" || l.Size != int64(len(body)) || !strings.Contains(l.URL, "uuid=0b5c2d10-0000-4000-8000-00000000c0f1") {
		t.Errorf("Unlock = %+v, want the warning form's link", l)
	}
	if got := fetch(t, srv.Client(), l); !bytes.Equal(got, body) {
		t.Error("the link fetched something else than the file")
	}
}

func TestEveryWayDriveHoldsAFileBackIsTold(t *testing.T) {
	srv, r := fake(t)
	quota := srv.Add("1quotaquotaquotaquota0", gdtest.Item{Name: "popular.mkv", Body: []byte("x"), Quota: true})
	private := srv.Add("1privateprivateprivat0", gdtest.Item{Name: "mine.zip", Body: []byte("x"), Private: true})
	keyed := srv.Add("1keyedkeyedkeyedkeyed0", gdtest.Item{Name: "old.zip", Body: []byte("x"), ResourceKey: "0-abc"})

	_, err := r.Client.Unlock(context.Background(), quota, "")
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Reason != core.ReasonLimit || w.Wait != quotaWait {
		t.Errorf("a used up quota gave %v, want a wait", err)
	}
	_, err = r.Client.Unlock(context.Background(), private, "")
	var ref *hoster.Refusal
	if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("a private file gave %v, want a refusal for a login", err)
	}
	if _, err := r.Client.Unlock(context.Background(), "https://drive.google.com/file/d/1nonenonenonenonenone0/view", ""); !hoster.IsGone(err) {
		t.Errorf("an unknown file gave %v, want gone", err)
	}
	if _, err := r.Client.Unlock(context.Background(), keyed, ""); err != nil {
		t.Errorf("a file shared with a resource key gave %v", err)
	}
}

func TestInspectNamesAndSizesFilesAndKnowsGoneOnes(t *testing.T) {
	srv, r := fake(t)
	small := srv.Add("0B9P1L--7Wd2vU3VUVlFnbTgtS2c", gdtest.Item{Name: "spam.txt", Body: []byte("spam\n")})
	big := srv.Add("1l_5RK28JRL19wpT22B-DY9We3TVXnnQQ", gdtest.Item{Name: "fcn8s_from_caffe.npz", Body: make([]byte, 700), Big: true})
	quota := srv.Add("1quotaquotaquotaquota0", gdtest.Item{Name: "popular.mkv", Body: []byte("x"), Quota: true})

	got := r.Inspect(context.Background(), []string{
		small, "https://drive.google.com/drive/folders/15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl", big,
		"https://drive.google.com/file/d/1nonenonenonenonenone0/view", quota,
	})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "spam.txt", Size: 5},
		{},
		{Availability: core.AvailOnline, Name: "fcn8s_from_caffe.npz", Size: 700},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOnline},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v, want %+v", got, want)
	}
}

func TestAFolderOpensIntoEveryFileOfItsSubfoldersPageByPage(t *testing.T) {
	srv, r := fake(t)
	srv.Add("1fileAfileAfileAfileA0", gdtest.Item{Name: "spam.txt", Body: []byte("spam\n")})
	srv.Add("1fileBfileBfileBfileB0", gdtest.Item{Name: "gdown.pptx", Body: make([]byte, 34)})
	srv.Add("1fileCfileCfileCfileC0", gdtest.Item{Name: "deep.txt", Body: make([]byte, 3), ResourceKey: "0-deep"})
	srv.Add("1docDdocDdocDdocDdocD0", gdtest.Item{Name: "gdown", Mime: gdtest.Document, Body: []byte("PK")})
	srv.Add("1formEformEformEform0", gdtest.Item{Name: "survey", Mime: gdtest.Form})
	srv.Add("1shortcutshortcutsho0", gdtest.Item{Name: "spam link", Target: "1fileAfileAfileAfileA0"})
	srv.Add("1deepdeepdeepdeepdee0", gdtest.Item{Name: "deep_folder", Mime: gdtest.Folder, Children: []string{"1fileCfileCfileCfileC0"}, ResourceKey: "0-sub"})
	link := srv.Add("15uNXeRBIhVvZJIhL4yTw4IsStMhUaaxl", gdtest.Item{Name: "gdown", Mime: gdtest.Folder, Children: []string{
		"1deepdeepdeepdeepdee0", "1docDdocDdocDdocDdocD0", "1fileBfileBfileBfileB0", "1fileAfileAfileAfileA0", "1formEformEformEform0", "1shortcutshortcutsho0",
	}})
	srv.Page(2)

	got, err := r.List(context.Background(), link, "")
	want := []resolver.Listing{
		{URL: "https://docs.google.com/document/d/1docDdocDdocDdocDdocD0", Name: "gdown.docx", Folder: "gdown"},
		{URL: "https://drive.google.com/file/d/1fileBfileBfileBfileB0", Name: "gdown.pptx", Size: 34, Folder: "gdown"},
		{URL: "https://drive.google.com/file/d/1fileAfileAfileAfileA0", Name: "spam.txt", Size: 5, Folder: "gdown"},
		{URL: "https://drive.google.com/file/d/1fileCfileCfileCfileC0?resourcekey=0-deep", Name: "deep.txt", Size: 3, Folder: "gdown/deep_folder"},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("List = %+v, %v; want %+v", got, err, want)
	}
	if files, err := r.List(context.Background(), "https://drive.google.com/file/d/1fileAfileAfileAfileA0", ""); err != nil || files != nil {
		t.Errorf("a file link listed %+v, %v; want nothing", files, err)
	}
	if _, err := r.List(context.Background(), "https://drive.google.com/drive/folders/1nonenonenonenonenone0", ""); !hoster.IsGone(err) {
		t.Errorf("an unknown folder gave %v, want gone", err)
	}
}

func TestAPrivateFolderAsksForALogin(t *testing.T) {
	srv, r := fake(t)
	link := srv.Add("1privateprivateprivat0", gdtest.Item{Name: "mine", Mime: gdtest.Folder, Private: true})
	_, err := r.List(context.Background(), link, "")
	var ref *hoster.Refusal
	if !errors.As(err, &ref) || ref.Reason != core.ReasonAuth {
		t.Errorf("List = %v, want a refusal for a login", err)
	}
}

func TestAGoogleDocumentDownloadsAsItsOfficeExport(t *testing.T) {
	srv, r := fake(t)
	link := srv.Add("1TFYNzuZJTgNGzGmjraZ58ZVOh9_YoKeBnU-opWgXQL4", gdtest.Item{Name: "gdown", Mime: gdtest.Document, Body: []byte("PK\x03\x04docx")})

	l, err := r.Client.Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "gdown.docx" || l.Conns != 1 || !strings.HasSuffix(l.URL, "/document/d/1TFYNzuZJTgNGzGmjraZ58ZVOh9_YoKeBnU-opWgXQL4/export?format=docx") {
		t.Errorf("Unlock = %+v", l)
	}
	if got := fetch(t, srv.Client(), l); string(got) != "PK\x03\x04docx" {
		t.Errorf("the export fetched %q", got)
	}
}
