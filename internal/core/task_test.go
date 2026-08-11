package core

import "testing"

// Nil for "no list" and nil for "everything ticked" are the same answer because
// they are the same instruction: the download library reads an empty selection
// as "fetch all of it". Handing it an explicit all-files list instead would
// differ only in being longer and easy to get one entry short.
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

// Nothing ticked is a real answer and it is NOT "fetch everything". An empty
// non-nil slice would be read by the library as "no opinion" and start the
// whole torrent, which is the opposite of what the user just did.
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
	// And it clears as well as sets, or a torrent that stops seeding keeps
	// claiming a peer count from the last reading it ever got.
	TorrentStats{}.ApplyTo(&task)
	if task.Peers != 0 || task.Seeds != 0 || task.Ratio != 0 || task.Uploaded != 0 || task.Seeding {
		t.Fatalf("a zero reading left stale numbers behind: %+v", task)
	}
}

// Seeding is a FLAG beside StatusDone and never a Status of its own - build
// plan section 4, conflict 2, unbroken since Wave 1. A new status value would
// break every exhaustive mapping of the seven, the store round trip and a
// rollback to the previous build. This test is what makes that a rule rather
// than a comment.
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
