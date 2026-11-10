package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/fortress"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// oneEngine is a fortress with one ArrowLoop instance holding one job.
type oneEngine struct{}

func (oneEngine) Members() []fortress.Member {
	return []fortress.Member{{ID: "m-tower", App: fortress.App{ID: afterdownload.EngineApp}, Device: "Tower", Actions: afterdownload.Uses}}
}

func (oneEngine) Act(_ context.Context, _, action string, _ []byte) ([]byte, error) {
	if action != afterdownload.ListAction {
		return nil, fortress.ErrNoSuchAction
	}
	return []byte(`[{"name":"series","direction":"leftToRight","mode":"move","paused":false,"running":false}]`), nil
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
