package ytdlp

import (
	"context"
	"testing"
)

func TestProbeTitleNamesTheVideoByExtractorAndID(t *testing.T) {
	got, err := fakeYtdlpBackend(t, "identified").ProbeTitle(context.Background(), "https://youtu.be/dQw4w9WgXcQ", nil)
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if got.Extractor != "Youtube" || got.ID != "dQw4w9WgXcQ" {
		t.Errorf("the probe names the video %q %q, want Youtube dQw4w9WgXcQ", got.Extractor, got.ID)
	}
}

// A link that probes as several videos downloads them all, so the first
// one's id would name only part of it.
func TestProbeTitleNamesNoVideoForALinkThatListsSeveral(t *testing.T) {
	got, err := fakeYtdlpBackend(t, "identifiedplaylist").ProbeTitle(context.Background(), "https://example.com/list", nil)
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if got.Extractor != "" || got.ID != "" {
		t.Errorf("the probe names the video %q %q, want none for a list", got.Extractor, got.ID)
	}
	if got.Title != "Entry One" {
		t.Errorf("the title is %q, want the first entry's as before", got.Title)
	}
}
