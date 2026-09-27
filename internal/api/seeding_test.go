package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// The seeding routes act on the finished torrents of a selection whose state
// they change and answer with those: stop on one that owes its seeding, start
// on one whose seeding is over. Disabled, so neither is taken up to seed in a
// test without a network.
func TestTheSeedingRoutesStopAndStartTheTorrentsOfASelection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	finished := func(id string, over bool) *core.Task {
		return &core.Task{
			ID: id, URL: "magnet:?xt=urn:btih:" + id, Name: id, Resolver: "torrent",
			Status: core.StatusDone, CreatedAt: time.Now(), SeedingOver: over,
		}
	}
	for _, task := range []*core.Task{
		finished("0123456789abcdef0123456789abcdef01234567", false),
		finished("fedcba9876543210fedcba9876543210fedcba98", true),
		{ID: "plain", URL: "https://host.example/one.bin", Resolver: "http", Status: core.StatusDone, CreatedAt: time.Now()},
	} {
		if err := st.Save(task); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	srv := httptest.NewServer(Handler(a))
	defer srv.Close()
	sel := []string{"0123456789abcdef0123456789abcdef01234567", "fedcba9876543210fedcba9876543210fedcba98", "plain"}

	code, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/seeding/stop", map[string]any{"ids": sel})
	if code != http.StatusOK || touchedCount(t, body) != 1 {
		t.Fatalf("stopping the seeding = %d %s, want the one torrent that owed it", code, body)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.Resolver == "torrent" && !task.SeedingOver {
			t.Errorf("%s still owes its seeding after the stop", task.ID)
		}
	}

	code, body = postJSON(t, http.MethodPost, srv.URL+"/api/tasks/seeding/start", map[string]any{"ids": sel})
	if code != http.StatusOK || touchedCount(t, body) != 2 {
		t.Fatalf("starting the seeding = %d %s, want both torrents", code, body)
	}
	for _, task := range tasksNow(t, srv.URL) {
		if task.Resolver == "torrent" && (task.SeedingOver || !app.SeedPending(&task)) {
			t.Errorf("%s does not owe its seeding after the start", task.ID)
		}
	}
	if _, body := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/seeding/start", map[string]any{"ids": sel}); touchedCount(t, body) != 0 {
		t.Errorf("starting torrents that owe their seeding already reports %s, want a count of 0", body)
	}
	if code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/tasks/seeding/stop", map[string]any{"ids": []string{}}); code != http.StatusBadRequest {
		t.Errorf("a stop without ids = %d, want 400", code)
	}
}
