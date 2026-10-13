package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A task nothing is fetching has no map, and the route says so with null
// rather than an error, since the panel asks about every task it opens.
func TestTheRouteAnswersNullWhereNothingKnowsThePieces(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	reg := newRegistry()
	registerTasks(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	created := stage(t, a, "https://host.example/still-collected.bin")

	for _, id := range []string{created[0].ID, "nobody"} {
		resp, err := http.Get(srv.URL + "/api/tasks/" + id + "/pieces")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "null" {
			t.Errorf("task %s: %d %q, want 200 null", id, resp.StatusCode, body)
		}
	}
}
