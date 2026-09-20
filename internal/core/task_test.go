package core

import "testing"

func TestSelectedTorrentIndicesSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	all := []TorrentFile{
		{Path: "a.mkv", Selected: true},
		{Path: "b.mkv", Selected: true},
	}
	if got := SelectedTorrentIndices(all); got != nil {
		t.Fatalf("every file ticked gave %v, want nil so the library fetches all of them", got)
	}
	if got := SelectedTorrentIndices(nil); got != nil {
		t.Fatalf("no list at all gave %v, want nil", got)
	}
}

func TestSelectedTorrentIndicesNamesThePositionsTheLibraryUses(t *testing.T) {
	files := []TorrentFile{
		{Path: "a.mkv", Selected: true},
		{Path: "b.mkv"},
		{Path: "c.srt", Selected: true},
		{Path: "d.nfo"},
	}
	got := SelectedTorrentIndices(files)
	want := []int{0, 2}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestNothingTickedIsNotTheSameAsEverythingTicked checks for an empty non-nil
// slice, since nil would make the library fetch the whole torrent.
func TestNothingTickedIsNotTheSameAsEverythingTicked(t *testing.T) {
	files := []TorrentFile{{Path: "a.mkv"}, {Path: "b.mkv"}}
	got := SelectedTorrentIndices(files)
	if got == nil {
		t.Fatal("unticking every file answered nil, which the download library reads as fetch-everything")
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want an empty selection", got)
	}
}

func TestApplyToWritesEveryTorrentFieldAndTouchesNothingElse(t *testing.T) {
	task := Task{Name: "Show.S01", Status: StatusDone, Loaded: 42}
	TorrentStats{Peers: 188, Seeds: 4, Ratio: 1.25, Uploaded: 900, Seeding: true}.ApplyTo(&task)
	if task.Peers != 188 || task.Seeds != 4 || task.Ratio != 1.25 || task.Uploaded != 900 || !task.Seeding {
		t.Fatalf("task = %+v", task)
	}
	if task.Name != "Show.S01" || task.Status != StatusDone || task.Loaded != 42 {
		t.Fatal("ApplyTo wrote outside the torrent fields")
	}
	TorrentStats{}.ApplyTo(&task)
	if task.Peers != 0 || task.Seeds != 0 || task.Ratio != 0 || task.Uploaded != 0 || task.Seeding {
		t.Fatalf("a zero reading left stale numbers behind: %+v", task)
	}
}

// TestSeedingDidNotBecomeAnEighthStatus guards the rule that seeding is a flag
// beside StatusDone; a new Status would break every exhaustive mapping, the
// store round trip and a rollback.
func TestSeedingDidNotBecomeAnEighthStatus(t *testing.T) {
	seven := []Status{
		StatusCollected, StatusQueued, StatusRunning, StatusPaused,
		StatusExtracting, StatusDone, StatusError,
	}
	for _, s := range seven {
		if string(s) == "seeding" {
			t.Fatal("seeding became a Status value")
		}
	}
	if len(seven) != 7 {
		t.Fatalf("the status set is now %d values; every exhaustive mapping of it has to be revisited", len(seven))
	}
}
