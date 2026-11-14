package crossseed

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheLogReadsBackItsLastEntriesNewestFirst(t *testing.T) {
	l := OpenLog(filepath.Join(t.TempDir(), "cross-seed.jsonl"))
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		l.Add(Entry{Time: at.Add(time.Duration(i) * time.Minute), Tracker: "OPS", Name: fmt.Sprint("album ", i), Result: ResultInjected})
	}
	got, err := l.Last(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "album 4" || got[2].Name != "album 2" {
		t.Fatalf("Last(3) = %+v", got)
	}
}

func TestAFullLogStartsAFreshFileAndKeepsTheLastOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cross-seed.jsonl")
	l := OpenLog(path)
	l.max = 400
	for i := range 20 {
		l.Add(Entry{Tracker: "RED", Name: fmt.Sprint("album ", i), Result: ResultRefused, Reason: "the two folders are on different filesystems"})
	}
	for _, p := range []string{path, path + ".1"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() > 400 {
			t.Errorf("%s grew to %d bytes", p, fi.Size())
		}
	}
	got, err := l.Last(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Name != "album 19" {
		t.Fatalf("Last = %+v, want the newest first", got)
	}
	if len(got) >= 20 {
		t.Errorf("all %d entries are kept, want the oldest dropped", len(got))
	}
}

func TestAMissingLogHasNoEntries(t *testing.T) {
	got, err := OpenLog(filepath.Join(t.TempDir(), "cross-seed.jsonl")).Last(10)
	if err != nil || len(got) != 0 {
		t.Fatalf("Last = %+v, %v", got, err)
	}
}
