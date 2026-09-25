package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A rename refreshes the announce of the handler that saved it. A second
// handler built in the same process keeps its own, so neither can end up
// announcing the other's name.
func TestARenameRefreshesOnlyItsOwnHandlersAnnounce(t *testing.T) {
	t.Parallel()
	first := newRegistry()
	registerAll(first, testApp(t))
	var firstCalls, secondCalls int
	first.refreshDiscovery = func() { firstCalls++ }

	second := newRegistry()
	registerAll(second, testApp(t))
	second.refreshDiscovery = func() { secondCalls++ }

	mux := http.NewServeMux()
	first.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if code, _, msg := patchSettings(t, srv.URL, `{"instanceName":"Keller"}`); code != http.StatusOK {
		t.Fatalf("renaming answered %d: %s", code, msg)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Errorf("the rename refreshed the saving handler %d times and the other one %d times, want 1 and 0",
			firstCalls, secondCalls)
	}
}
