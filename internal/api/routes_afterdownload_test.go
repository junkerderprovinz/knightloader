package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/fortress"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// oneEngine is a fortress with one ArrowLoop instance holding one job.
type oneEngine struct{}

func (oneEngine) Members() []fortress.Member {
	return []fortress.Member{{ID: "m-tower", App: fortress.App{ID: afterdownload.EngineApp}, Device: "Tower", Actions: afterdownload.Uses}}
}

// created is what oneEngine was asked to create, for the tests to read.
var created = struct {
	sync.Mutex
	list []afterdownload.CreateRequest
}{}

func (oneEngine) Act(_ context.Context, _, action string, body []byte) ([]byte, error) {
	switch action {
	case afterdownload.ListAction:
		return []byte(`[{"name":"series","direction":"leftToRight","mode":"move","paused":false,"running":false}]`), nil
	case afterdownload.TargetsAction:
		return []byte(`[{"id":"nas","name":"NAS","kind":"share","where":"share /archive"}]`), nil
	case afterdownload.CreateAction:
		var req afterdownload.CreateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		if req.Name == "series" {
			return nil, &fortress.Refusal{Status: 409, Code: "name_taken"}
		}
		created.Lock()
		created.list = append(created.list, req)
		created.Unlock()
		return json.Marshal(map[string]string{"job": req.Name})
	}
	return nil, fortress.ErrNoSuchAction
}

type afterDownloadAnswer struct {
	Fortress bool                   `json:"fortress"`
	Engines  []afterdownload.Engine `json:"engines"`
}

func afterDownloadServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerAfterDownload(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

func TestTheStationListsTheJobsOfTheFortress(t *testing.T) {
	a, srv := afterDownloadServer(t)
	a.SetFortress(oneEngine{})

	var got afterDownloadAnswer
	if code := getJSON(t, srv.URL+"/api/afterdownload", &got); code != http.StatusOK {
		t.Fatalf("GET /api/afterdownload answered %d", code)
	}
	if !got.Fortress || len(got.Engines) != 1 {
		t.Fatalf("answered %+v", got)
	}
	e := got.Engines[0]
	if e.ID != "m-tower" || e.Device != "Tower" || len(e.Jobs) != 1 || e.Jobs[0].Name != "series" || e.Jobs[0].Mode != "move" {
		t.Errorf("listed %+v", e)
	}
}

func TestWithoutAFortressTheStationSaysSo(t *testing.T) {
	_, srv := afterDownloadServer(t)

	var got afterDownloadAnswer
	if code := getJSON(t, srv.URL+"/api/afterdownload", &got); code != http.StatusOK {
		t.Fatalf("GET /api/afterdownload answered %d", code)
	}
	if got.Fortress || got.Engines == nil || len(got.Engines) != 0 {
		t.Errorf("answered %+v", got)
	}
}

func TestTheStationListsWhereArrowLoopCanCopyTo(t *testing.T) {
	a, srv := afterDownloadServer(t)
	a.SetFortress(oneEngine{})

	var got []afterdownload.Destination
	if code := getJSON(t, srv.URL+"/api/afterdownload/engines/m-tower/targets", &got); code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	if len(got) != 1 || got[0].ID != "nas" || got[0].Kind != "share" {
		t.Errorf("listed %+v", got)
	}
}

// withSeries gives a a category whose folder is a template under /data, which
// the host knows as /mnt/user.
func withSeries(t *testing.T, a *app.App) {
	t.Helper()
	cfg := a.Settings.Get()
	cfg.Categories = []settings.Category{
		{ID: "serien", Dir: absPath("data", "media", "serien") + "/<jd:packagename>"},
		{ID: "musik"},
	}
	if _, err := a.Settings.Set(cfg); err != nil {
		t.Fatal(err)
	}
	inside := absPath("data")
	a.ToHost = func(dir string) (string, error) {
		rel, err := filepath.Rel(inside, dir)
		if err != nil {
			return "", err
		}
		return "/mnt/user/" + filepath.ToSlash(rel), nil
	}
}

func TestANewJobStartsFromTheCategoryFolderAsTheHostSeesIt(t *testing.T) {
	a, srv := afterDownloadServer(t)
	a.SetFortress(oneEngine{})
	withSeries(t, a)

	code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/afterdownload/jobs", map[string]string{
		"category": "serien", "member": "m-tower", "name": "Series to the archive", "target": "nas", "mode": "mirror",
	})
	if code != http.StatusOK {
		t.Fatalf("answered %d: %s", code, raw)
	}
	var answer struct{ Job string }
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Job != "Series to the archive" {
		t.Fatalf("answered %s", raw)
	}
	created.Lock()
	defer created.Unlock()
	want := afterdownload.CreateRequest{Name: "Series to the archive", Source: "/mnt/user/media/serien", Target: "nas", Mode: "mirror"}
	if len(created.list) == 0 || created.list[len(created.list)-1] != want {
		t.Errorf("ArrowLoop was asked %+v, want %+v", created.list, want)
	}
}

func TestANewJobIsRefusedWithAReasonTheStationCanWord(t *testing.T) {
	a, srv := afterDownloadServer(t)
	a.SetFortress(oneEngine{})
	withSeries(t, a)

	for _, c := range []struct {
		name     string
		body     map[string]string
		status   int
		wantCode string
	}{
		{"name taken", map[string]string{"category": "serien", "member": "m-tower", "name": "series", "target": "nas", "mode": "sync"}, http.StatusConflict, "afterDownload.nameTaken"},
		{"category without a folder", map[string]string{"category": "musik", "member": "m-tower", "name": "music", "target": "nas", "mode": "sync"}, http.StatusBadRequest, "afterDownload.noFolder"},
		{"unknown category", map[string]string{"category": "filme", "member": "m-tower", "name": "films", "target": "nas", "mode": "sync"}, http.StatusBadRequest, "afterDownload.noFolder"},
		{"member gone", map[string]string{"category": "serien", "member": "m-gone", "name": "new", "target": "nas", "mode": "sync"}, http.StatusNotFound, "afterDownload.gone"},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/afterdownload/jobs", c.body)
			var refusal struct{ Code string }
			_ = json.Unmarshal(raw, &refusal)
			if code != c.status || refusal.Code != c.wantCode {
				t.Errorf("answered %d %s, want %d %s", code, raw, c.status, c.wantCode)
			}
		})
	}
}

// absPath is an absolute folder on whichever platform the tests run on.
func absPath(segs ...string) string {
	abs, err := filepath.Abs(filepath.Join(append([]string{string(filepath.Separator)}, segs...)...))
	if err != nil {
		panic(err)
	}
	return abs
}
