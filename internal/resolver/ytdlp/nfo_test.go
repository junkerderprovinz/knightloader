package ytdlp

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestBuildNFOFillsWhatALibraryScrapes(t *testing.T) {
	m := buildNFO(infoDict{
		ID: "dQw4w9WgXcQ", Title: "A Video", Description: "Line one.\nLine two.",
		Uploader: "Some Channel", UploadDate: "20260907", Duration: 2718.041,
		Thumbnail: "https://example.invalid/t.jpg", WebpageURL: "https://example.invalid/v",
		Extractor: "Youtube", Categories: []string{"Music"}, Tags: []string{"a", "b"},
	})
	if m.Title != "A Video" || m.Plot != "Line one.\nLine two." {
		t.Errorf("title/plot = %q / %q", m.Title, m.Plot)
	}
	// Whole minutes, truncated, as Kodi's <runtime> expects.
	if m.Runtime != 45 {
		t.Errorf("Runtime = %d, want 45 whole minutes", m.Runtime)
	}
	if m.Premiered != "2026-09-07" || m.Year != "2026" {
		t.Errorf("premiered/year = %q / %q, want the ISO date and the year", m.Premiered, m.Year)
	}
	if m.Studio != "Some Channel" || m.Director != "Some Channel" {
		t.Errorf("studio/director = %q / %q", m.Studio, m.Director)
	}
	if m.UniqueID == nil || m.UniqueID.Value != "dQw4w9WgXcQ" || m.UniqueID.Type != "youtube" {
		t.Errorf("uniqueid = %+v, want the source's own id under its own extractor name", m.UniqueID)
	}
}

// Some scrapers store an empty element as a real empty value.
func TestBuildNFOLeavesOutWhatTheExtractorDidNotSay(t *testing.T) {
	body, err := xml.Marshal(buildNFO(infoDict{Title: "Bare"}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, unwanted := range []string{"<plot>", "<premiered>", "<year>", "<uniqueid", "<genre>", "<runtime>"} {
		if strings.Contains(string(body), unwanted) {
			t.Errorf("an empty %s survived into the document:\n%s", unwanted, body)
		}
	}
	if !strings.Contains(string(body), "<title>Bare</title>") {
		t.Errorf("the one field there was is missing:\n%s", body)
	}
}

func TestParseUploadDateRefusesAPartialDate(t *testing.T) {
	for _, in := range []string{"", "2026", "202609", "2026-09-07", "not a date"} {
		if p, y := parseUploadDate(in); p != "" || y != "" {
			t.Errorf("parseUploadDate(%q) = %q / %q, want nothing", in, p, y)
		}
	}
	if p, y := parseUploadDate("20260907"); p != "2026-09-07" || y != "2026" {
		t.Errorf("parseUploadDate = %q / %q", p, y)
	}
}

func TestBuildNFOBoundsTheTagList(t *testing.T) {
	many := make([]string, 200)
	for i := range many {
		many[i] = "tag"
	}
	if got := len(buildNFO(infoDict{Tags: many}).Tags); got != maxNFOTags {
		t.Errorf("kept %d tags, want at most %d", got, maxNFOTags)
	}
}

// The readers silently skip an NFO that is not well-formed XML.
func TestNFOEscapesWhatWouldBreakTheDocument(t *testing.T) {
	body, err := xml.Marshal(buildNFO(infoDict{Title: `Fish & Chips <best> "ever"`}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(body), "<best>") {
		t.Fatalf("an angle bracket from the title reached the document raw:\n%s", body)
	}
	var back nfoMovie
	if err := xml.Unmarshal(body, &back); err != nil {
		t.Fatalf("the document does not parse back: %v\n%s", err, body)
	}
	if back.Title != `Fish & Chips <best> "ever"` {
		t.Errorf("round trip gave %q", back.Title)
	}
}

func TestReadInfoJSONTreatsAMissingFileAsNothingToSay(t *testing.T) {
	if _, ok := readInfoJSON("no-such-file.info.json"); ok {
		t.Errorf("readInfoJSON claimed to have read a file that is not there")
	}
}

func TestInfoJSONAndNFOPathsReplaceTheExtension(t *testing.T) {
	if got := infoJSONPath("/downloads/Some Title.mkv"); got != "/downloads/Some Title.info.json" {
		t.Errorf("infoJSONPath = %q", got)
	}
	if got := nfoPath("/downloads/Some Title.mkv"); got != "/downloads/Some Title.nfo" {
		t.Errorf("nfoPath = %q", got)
	}
}

// The Destination lines name half-stream files the Merger removes.
func TestFinishedFileTakesTheMergedNameNotTheHalfStreams(t *testing.T) {
	cases := []struct {
		line, want string
		ok         bool
	}{
		{`[download] Destination: /d/A Video.f137.mp4`, "/d/A Video.f137.mp4", true},
		{`[Merger] Merging formats into "/d/A Video.mkv"`, "/d/A Video.mkv", true},
		{`[ExtractAudio] Destination: /d/A Video.mp3`, "/d/A Video.mp3", true},
		{`[info] Writing video subtitles to: /d/A Video.de.srt`, "", false},
		{`[download] 100% of 5.00MiB`, "", false},
	}
	for _, c := range cases {
		got, ok := finishedFile(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("finishedFile(%q) = %q, %v; want %q, %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestWroteSubtitleRecognisesBothKinds(t *testing.T) {
	for _, line := range []string{
		"[info] Writing video subtitles to: /d/A.de.srt",
		"[info] Writing automatic captions subtitles to: /d/A.en.srt",
	} {
		if !wroteSubtitle(line) {
			t.Errorf("wroteSubtitle(%q) = false", line)
		}
	}
	if wroteSubtitle("[download] Destination: /d/A.mkv") {
		t.Errorf("wroteSubtitle counted a download line as a subtitle file")
	}
}
