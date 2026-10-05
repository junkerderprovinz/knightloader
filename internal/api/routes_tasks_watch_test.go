package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func watchMux(tasks *[]*core.Task) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tasks/watch", serveTaskWatch(func() []*core.Task { return *tasks }))
	return mux
}

func lookAtWatch(t *testing.T, mux *http.ServeMux, path, encoding string) (watchList, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if encoding != "" {
		req.Header.Set("Accept-Encoding", encoding)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s answered %d: %s", path, rec.Code, rec.Body)
	}
	var body io.Reader = bytes.NewReader(rec.Body.Bytes())
	if rec.Header().Get("Content-Encoding") == "gzip" {
		z, err := gzip.NewReader(body)
		if err != nil {
			t.Fatal(err)
		}
		body = z
	}
	var list watchList
	if err := json.NewDecoder(body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	return list, rec
}

func TestTheWatchListKeepsItsTagWhileDownloadsOnlyMoveBytes(t *testing.T) {
	running := &core.Task{ID: "a", Name: "a.bin", Status: core.StatusRunning, Enabled: true, Loaded: 10, Speed: 5}
	tasks := []*core.Task{running}
	mux := watchMux(&tasks)

	first, _ := lookAtWatch(t, mux, "/api/tasks/watch", "")
	if first.Tag == "" || first.Same || len(first.Tasks) != 1 {
		t.Fatalf("first look = %+v, want a tag and the one task", first)
	}

	running.Loaded, running.Speed = 5000, 900
	again, _ := lookAtWatch(t, mux, "/api/tasks/watch?tag="+first.Tag, "")
	if !again.Same || again.Tasks != nil || again.Tag != first.Tag {
		t.Fatalf("look after progress only = %+v, want the same tag and no tasks", again)
	}

	running.Status = core.StatusDone
	done, _ := lookAtWatch(t, mux, "/api/tasks/watch?tag="+first.Tag, "")
	if done.Same || done.Tag == first.Tag || len(done.Tasks) != 1 || done.Tasks[0].Status != core.StatusDone {
		t.Fatalf("look after the download finished = %+v, want a new tag and the finished task", done)
	}
}

func TestAnEmptyWatchListIsNotReadAsUnchanged(t *testing.T) {
	var tasks []*core.Task
	_, rec := lookAtWatch(t, watchMux(&tasks), "/api/tasks/watch?tag=nothing", "")
	if !strings.Contains(rec.Body.String(), `"tasks":[]`) || strings.Contains(rec.Body.String(), `"same"`) {
		t.Fatalf("empty list answered %s, want an empty tasks array", rec.Body)
	}
}

func TestTheWatchListSaysWhatARunningOrFailedTaskIsWaitingFor(t *testing.T) {
	soon := time.Now().Add(time.Minute)
	tasks := []*core.Task{
		{ID: "retry", URL: "https://example.net/r", Status: core.StatusError, Enabled: true, NextTry: soon, Error: "busy", ErrorCode: "limit", Dir: "/d", Resolver: "http"},
		{ID: "over", Name: "over.bin", URL: "https://example.net/o", Status: core.StatusError, Enabled: true},
		{ID: "stalled", Name: "s", Status: core.StatusRunning, Enabled: true, StalledSince: soon},
		{ID: "remote", Name: "m", Status: core.StatusRunning, Enabled: true, Remote: &core.RemoteFetch{Progress: 0.4}},
		{ID: "off", Name: "x", Status: core.StatusQueued, Error: "old", NextTry: soon},
	}
	list, _ := lookAtWatch(t, watchMux(&tasks), "/api/tasks/watch", "")
	got := map[string]watchTask{}
	for _, w := range list.Tasks {
		got[w.ID] = w
	}
	if w := got["retry"]; !w.Retrying || w.Name != "https://example.net/r" || w.ErrorCode != "limit" || w.Dir != "/d" || w.Resolver != "http" {
		t.Errorf("failure with a retry to come = %+v", w)
	}
	if w := got["over"]; w.Retrying || w.Name != "over.bin" {
		t.Errorf("failure with no retry left = %+v, want not retrying and its own name", w)
	}
	if !got["stalled"].Stalled || got["stalled"].Remote || !got["remote"].Remote || got["remote"].Stalled {
		t.Errorf("stalled = %+v, remote = %+v", got["stalled"], got["remote"])
	}
	if w := got["off"]; w.Enabled || w.Retrying || w.Error != "" {
		t.Errorf("queued task = %+v, want it disabled with no failure fields", w)
	}
}

func TestTheWatchListIsCompressedOnlyForAClientThatTakesGzip(t *testing.T) {
	tasks := []*core.Task{{ID: "a", Name: "a.bin", Status: core.StatusDone, Enabled: true}}
	mux := watchMux(&tasks)
	for _, c := range []struct {
		accept string
		gzip   bool
	}{
		{"", false},
		{"gzip", true},
		{"deflate, GZIP", true},
		{"gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"br", false},
	} {
		list, rec := lookAtWatch(t, mux, "/api/tasks/watch", c.accept)
		if got := rec.Header().Get("Content-Encoding") == "gzip"; got != c.gzip {
			t.Errorf("Accept-Encoding %q: compressed %v, want %v", c.accept, got, c.gzip)
		}
		if len(list.Tasks) != 1 {
			t.Errorf("Accept-Encoding %q: tasks = %+v", c.accept, list.Tasks)
		}
	}
}

func TestThePhoneReachesTheWatchListWithAReadToken(t *testing.T) {
	served := false
	for _, r := range buildRegistry(t).routes {
		served = served || r.pattern() == "GET /api/tasks/watch"
	}
	if !served {
		t.Error("the server does not serve the watch list")
	}
	if !relayForwardable(http.MethodGet, "/api/tasks/watch?tag=abc") {
		t.Error("the relay does not carry the watch list")
	}
	if s := scopeFor("GET /api/tasks/watch"); s != apitoken.ScopeRead {
		t.Errorf("scope = %q, want read", s)
	}
}

func TestTheWatchListMarksAFailureAMirrorTookOn(t *testing.T) {
	tasks := []*core.Task{
		{ID: "orig", Name: "film.rar", Package: "Film", Status: core.StatusError, Enabled: true, Error: "gone"},
		{ID: "copy", Name: "film.rar", Package: "Film", Status: core.StatusRunning, Enabled: true, MirrorOf: "orig"},
		{ID: "lone", Name: "other.rar", Package: "Film", Status: core.StatusError, Enabled: true, Error: "gone"},
	}
	list, _ := lookAtWatch(t, watchMux(&tasks), "/api/tasks/watch", "")
	got := map[string]watchTask{}
	for _, w := range list.Tasks {
		got[w.ID] = w
	}
	if !got["orig"].HandedOver {
		t.Errorf("the failed source a mirror runs for = %+v, want it handed over", got["orig"])
	}
	if got["lone"].HandedOver || got["copy"].HandedOver {
		t.Errorf("lone = %+v, copy = %+v, want neither handed over", got["lone"], got["copy"])
	}
}
