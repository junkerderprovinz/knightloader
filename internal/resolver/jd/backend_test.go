package jd

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestAggregateSumsThePackage pins the fix for a link JDownloader crawled into
// several files. Reporting only the first showed a fraction of the real size
// and called the task finished while the rest was still downloading.
func TestAggregateSumsThePackage(t *testing.T) {
	links := []DownloadLink{
		{Name: "part1.rar", BytesTotal: 100, BytesLoaded: 100, Speed: 0, Finished: true},
		{Name: "part2.rar", BytesTotal: 200, BytesLoaded: 50, Speed: 700},
		{Name: "part3.rar", BytesTotal: 300, BytesLoaded: 0, Speed: 300},
	}
	u := aggregate(links)
	if u.Size != 600 {
		t.Errorf("Size = %d, want the package total 600", u.Size)
	}
	if u.Loaded != 150 {
		t.Errorf("Loaded = %d, want 150", u.Loaded)
	}
	if u.Speed != 1000 {
		t.Errorf("Speed = %d, want the combined 1000", u.Speed)
	}
	if u.Status != core.StatusRunning {
		t.Errorf("Status = %q; one finished file does not finish the package", u.Status)
	}
	if !strings.Contains(u.Name, "+2") {
		t.Errorf("Name = %q, want it to say how many more files there are", u.Name)
	}
}

// TestAggregateFinishesOnlyWhenEveryFileIs is the other half: the task settles
// exactly once, when nothing is left.
func TestAggregateFinishesOnlyWhenEveryFileIs(t *testing.T) {
	all := []DownloadLink{
		{Name: "a.bin", BytesTotal: 10, BytesLoaded: 10, Finished: true},
		{Name: "b.bin", BytesTotal: 20, BytesLoaded: 20, Status: "Finished"},
	}
	u := aggregate(all)
	if u.Status != core.StatusDone {
		t.Errorf("Status = %q, want done", u.Status)
	}
	if u.Speed != 0 {
		t.Errorf("Speed = %d on a finished package, want 0", u.Speed)
	}

	one := []DownloadLink{{Name: "solo.bin", BytesTotal: 5, BytesLoaded: 5, Finished: true}}
	if u := aggregate(one); u.Name != "solo.bin" {
		t.Errorf("Name = %q; a single file gets no counter", u.Name)
	}
}

// TestPatienceLimitsAreSeparate guards the reasoning rather than the numbers: a
// download that takes hours must not be killed for taking hours, so the limit
// on "did it ever start" has to be much shorter than the one on "has it
// stopped moving".
func TestPatienceLimitsAreSeparate(t *testing.T) {
	if appearLimit >= stallLimit {
		t.Errorf("appearLimit %v is not shorter than stallLimit %v; the two answer different questions",
			appearLimit, stallLimit)
	}
	if stallLimit < appearLimit*2 {
		t.Errorf("stallLimit %v leaves too little room for a hoster cool-down", stallLimit)
	}
}
