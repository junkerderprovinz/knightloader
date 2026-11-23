package settings

import (
	"strings"
	"testing"
)

func TestAStationKeepsTheJobItNames(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{
		{ID: "serien", AfterDownload: &AfterDownload{Member: " m-tower ", Device: " Tower ", Job: " photos "}},
	}})
	got := n.CategoryFor("serien").AfterDownload
	if got == nil || *got != (AfterDownload{Member: "m-tower", Device: "Tower", Job: "photos"}) {
		t.Errorf("the station reads %+v", got)
	}
}

// A station that names no instance or no job can start nothing, so it is the
// same as none.
func TestAStationWithoutAnInstanceOrAJobIsDropped(t *testing.T) {
	n := sanitizeCategories(Settings{Categories: []Category{
		{ID: "serien", AfterDownload: &AfterDownload{Member: "m-tower", Job: "  "}},
		{ID: "filme", AfterDownload: &AfterDownload{Job: "films"}},
	}})
	for _, c := range n.Categories {
		if c.AfterDownload != nil {
			t.Errorf("%s keeps the station %+v", c.ID, c.AfterDownload)
		}
	}
}

func TestAStationsNamesAreCutToLength(t *testing.T) {
	long := strings.Repeat("x", 500)
	n := sanitizeCategories(Settings{Categories: []Category{
		{ID: "serien", AfterDownload: &AfterDownload{Member: long, Device: long, Job: long}},
	}})
	got := n.CategoryFor("serien").AfterDownload
	if got == nil || len(got.Member) > maxAfterDownloadName || len(got.Device) > maxAfterDownloadName || len(got.Job) > maxAfterDownloadName {
		t.Errorf("the station reads %+v", got)
	}
}
