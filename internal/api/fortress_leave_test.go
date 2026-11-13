package api

import (
	"net/http"
	"testing"
)

func TestAnInstanceThatLeavesIsGoneFromTheOtherMembers(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	_, loop := arrowLoopOpen(t, relayURL, secret)
	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	listed := func() bool {
		for _, m := range loop.Members() {
			if m.App.ID == "knightloader" {
				return true
			}
		}
		return false
	}
	waitFor(t, "ArrowLoop to know KnightLoader", listed)

	if code, raw := postJSON(t, http.MethodDelete, srv.URL+"/api/fortress", nil); code != http.StatusOK {
		t.Fatalf("leave answered %d: %s", code, raw)
	}
	if listed() {
		t.Error("ArrowLoop still lists KnightLoader after it left")
	}
}
