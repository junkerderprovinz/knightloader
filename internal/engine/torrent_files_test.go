package engine

import (
	"slices"
	"testing"

	"github.com/GopeedLab/gopeed/pkg/base"
)

func TestFileProgressLandsOnTheTorrentsOwnFileIndices(t *testing.T) {
	// The library counts over the selection in selection order: files 3 and 1.
	got, ok := spreadProgress([]int64{30, 10}, torrentPick{count: 4, sel: []int{3, 1}})
	if !ok || !slices.Equal(got, []int64{-1, 10, -1, 30}) {
		t.Fatalf("spreadProgress = %v, %v; want the counts on files 1 and 3 and the rest left out", got, ok)
	}
}

func TestFileProgressOfATorrentFetchingEverythingCoversEveryFile(t *testing.T) {
	got, ok := spreadProgress([]int64{5, 6, 7}, torrentPick{count: 3})
	if !ok || !slices.Equal(got, []int64{5, 6, 7}) {
		t.Fatalf("spreadProgress = %v, %v; want every file counted", got, ok)
	}
}

func TestAReadingThatDoesNotFitTheSelectionIsNotShown(t *testing.T) {
	// A save from before a change, with one count too few.
	if got, ok := spreadProgress([]int64{5}, torrentPick{count: 3, sel: []int{0, 2}}); ok {
		t.Fatalf("spreadProgress = %v, want no reading", got)
	}
}

func TestTheLibrarysSavedTorrentStateYieldsItsFileCounts(t *testing.T) {
	// The shape of the library's internal fetcherData.
	type saved struct {
		Progress  []int64
		SeedBytes int64
		SeedTime  int64
	}
	got, ok := savedFileProgress(&saved{Progress: []int64{1, 2}, SeedBytes: 9})
	if !ok || !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("savedFileProgress = %v, %v", got, ok)
	}
	if _, ok := savedFileProgress(struct{ Connections []int }{}); ok {
		t.Fatal("an HTTP task's saved state read as a torrent's")
	}
}

func TestTheReportedFileListMarksTheSelection(t *testing.T) {
	res := &base.Resource{Name: "Pack", Files: []*base.FileInfo{
		{Name: "a.mkv", Size: 10},
		{Path: "extras", Name: "b.mkv", Size: 20},
	}}
	files := pickedFiles(res, []int{1})
	if len(files) != 2 || files[0].Selected || !files[1].Selected || files[1].Path != "extras/b.mkv" || files[1].Size != 20 {
		t.Fatalf("pickedFiles = %+v", files)
	}
	for _, f := range pickedFiles(res, nil) {
		if !f.Selected {
			t.Fatalf("no selection left %q out; it means every file", f.Path)
		}
	}
}
