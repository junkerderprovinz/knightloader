package crossseed

import (
	"path/filepath"
	"testing"
)

func TestAnAnswerIsRememberedAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cross-seed-asked.json")
	h := OpenHistory(path)
	h.Note("RED", "aa", Answer{Result: ResultNone})
	h.Note("OPS", "aa", Answer{Result: ResultInjected, InfoHash: "bb"})
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	h = OpenHistory(path)
	if !h.Asked("RED", "aa") || !h.Asked("OPS", "aa") {
		t.Fatal("an answer was forgotten across the restart")
	}
	if h.Asked("RED", "cc") || h.Asked("torznab:Prowlarr", "aa") {
		t.Fatal("a torrent or a tracker never asked counts as asked")
	}
	if got := h.Injected("aa"); len(got) != 1 || got[0] != "bb" {
		t.Errorf("Injected = %v, want the OPS torrent", got)
	}
}

func TestAnErrorIsAskedAgainOnceTheTrackerChanges(t *testing.T) {
	h := OpenHistory(filepath.Join(t.TempDir(), "asked.json"))
	h.Note("RED", "aa", Answer{Result: ResultError, Reason: "bad credentials"})
	h.Note("RED", "bb", Answer{Result: ResultNone})
	h.Note("OPS", "cc", Answer{Result: ResultError})
	if !h.Asked("RED", "aa") {
		t.Fatal("an error was asked again before anything changed")
	}
	h.ForgetErrors("RED")
	if h.Asked("RED", "aa") {
		t.Error("the error stayed after the key changed")
	}
	if !h.Asked("RED", "bb") || !h.Asked("OPS", "cc") {
		t.Error("forgetting RED's errors forgot more than those")
	}
}

func TestAnUnreadableHistoryStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asked.json")
	writeFile(t, path, "{not json")
	if OpenHistory(path).Asked("RED", "aa") {
		t.Fatal("an unreadable file answered")
	}
}
