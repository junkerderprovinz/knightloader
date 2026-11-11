package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// The browser extension follows the rows one send created, over the relay,
// without fetching the whole list every few seconds.
func TestTheTaskListAnswersOnlyTheRowsAskedFor(t *testing.T) {
	a := testApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	created := a.AddLinks([]string{"https://host.example/one.bin", "https://host.example/two.bin", "https://host.example/three.bin"}, "Batch")
	if len(created) != 3 {
		t.Fatalf("staged %d links, want 3", len(created))
	}

	reg := newRegistry()
	registerTasks(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ids := func(path string) []string {
		var got []*core.Task
		if code := getJSON(t, srv.URL+path, &got); code != http.StatusOK {
			t.Fatalf("GET %s answered %d", path, code)
		}
		out := []string{}
		for _, task := range got {
			out = append(out, task.ID)
		}
		slices.Sort(out)
		return out
	}
	want := []string{created[0].ID, created[2].ID}
	slices.Sort(want)
	if got := ids("/api/tasks?ids=" + created[0].ID + "," + created[2].ID + ",gone"); !slices.Equal(got, want) {
		t.Errorf("asked for two rows, got %v, want %v", got, want)
	}
	if got := ids("/api/tasks"); len(got) != 3 {
		t.Errorf("without ids the list has %d rows, want all 3", len(got))
	}
	if !groupReaches(t, http.MethodGet, "/api/tasks?ids=a,b") {
		t.Error("a member of the group cannot read rows by id")
	}
}
