package crossseed

import (
	"os"
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

func TestATorrentForgottenIsAskedAgainOnEveryTracker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asked.json")
	h := OpenHistory(path)
	h.Note("RED", "aa", Answer{Result: ResultNone})
	h.Note("OPS", "AA", Answer{Result: ResultInjected, InfoHash: "bb"})
	h.Note(DaemonKey, "aa", Answer{Result: ResultHanded})
	h.Note("OPS", "cc", Answer{Result: ResultNone})
	h.Forget("AA")
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}

	h = OpenHistory(path)
	for _, tracker := range []string{"RED", "OPS", DaemonKey} {
		if h.Asked(tracker, "aa") {
			t.Errorf("%s still counts as asked about the forgotten torrent", tracker)
		}
	}
	if len(h.Injected("aa")) != 0 {
		t.Error("the forgotten torrent still has its twin")
	}
	if !h.Asked("OPS", "cc") {
		t.Error("another torrent was forgotten with it")
	}
}

// A hard link to the old file is the witness: a save that rewrote the file in
// place, and so could be cut off half way, would change what it reads.
func TestASaveReplacesTheHistoryInsteadOfRewritingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cross-seed-asked.json")
	h := OpenHistory(path)
	h.Note("RED", "aa", Answer{Result: ResultNone})
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	witness := filepath.Join(dir, "witness")
	if err := os.Link(path, witness); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	h.Note("OPS", "aa", Answer{Result: ResultInjected, InfoHash: "bb"})
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(witness); string(got) != string(before) {
		t.Error("the save rewrote the history in place")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}
