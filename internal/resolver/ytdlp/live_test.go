package ytdlp

import (
	"strings"
	"testing"
	"time"
)

// TestParseProgressReadsTheShapeAnInstallWithoutTheLiveSwitchStillSends is
// the compatibility half live.go's file comment promises: the guard is off on
// every install until somebody turns it on, and the plain progress line is
// what those spawn with.
func TestParseProgressReadsTheShapeAnInstallWithoutTheLiveSwitchStillSends(t *testing.T) {
	p, ok := parseProgress(`KLP:{"downloaded_bytes":42,"total_bytes":100,"speed":7.5,"filename":"a.mkv"}`)
	if !ok {
		t.Fatalf("the plain progress line no longer parses")
	}
	if p.Downloaded != 42 || p.Total != 100 || p.Speed != 7 || p.Filename != "a.mkv" {
		t.Errorf("parsed %+v, want 42/100 at 7 B/s from a.mkv", p)
	}
	if p.LiveKnown {
		t.Errorf("the plain shape claimed to know about is_live: %+v", p)
	}
}

func TestParseProgressReadsTheLiveWrapper(t *testing.T) {
	p, ok := parseProgress(`KLP:{"live":"True","p":{"downloaded_bytes":42,"speed":7.5}}`)
	if !ok {
		t.Fatalf("the live progress line did not parse")
	}
	if !p.Live || !p.LiveKnown {
		t.Errorf("parsed %+v, want a known live reading", p)
	}
	if p.Downloaded != 42 {
		t.Errorf("parsed %+v, want the inner progress to survive the wrapper", p)
	}
}

// TestParseProgressFallsBackToTheEstimatedTotal pins behaviour the old inline
// decode had and that the extracted one must not lose: a DASH or m3u8 source
// reports no exact total, only yt-dlp's estimate, and losing it makes every
// fragmented download show no size at all.
func TestParseProgressFallsBackToTheEstimatedTotal(t *testing.T) {
	p, ok := parseProgress(`KLP:{"downloaded_bytes":1,"total_bytes_estimate":52428800.0}`)
	if !ok {
		t.Fatalf("the line did not parse")
	}
	if p.Total != 52428800 {
		t.Errorf("Total = %d, want the estimate", p.Total)
	}
}

func TestParseProgressRejectsWhatIsNotAProgressLine(t *testing.T) {
	for _, line := range []string{
		"[download] Destination: a.mkv",
		"",
		"KLP:not json",
		`KLP:{"live":"True","p":not json}`,
	} {
		if _, ok := parseProgress(line); ok {
			t.Errorf("parseProgress(%q) claimed to parse it", line)
		}
	}
}

// TestParseLiveFlagOnlyBelievesTheTwoWordsItKnows: yt-dlp writes NA for a
// field the extractor never set, and treating an unrecognised value as "live"
// would put the recording caps on an ordinary download.
func TestParseLiveFlagOnlyBelievesTheTwoWordsItKnows(t *testing.T) {
	cases := []struct {
		in          string
		live, known bool
	}{
		{"True", true, true},
		{"true", true, true},
		{"False", false, true},
		{"NA", false, false},
		{"", false, false},
		{"None", false, false},
	}
	for _, c := range cases {
		live, known := parseLiveFlag(c.in)
		if live != c.live || known != c.known {
			t.Errorf("parseLiveFlag(%q) = (%v, %v), want (%v, %v)", c.in, live, known, c.live, c.known)
		}
	}
}

// TestLiveGuardStartsTheClockAtTheFirstLiveByte, not at the spawn: yt-dlp
// does real work before a byte arrives - extraction, whatever anti-bot
// gauntlet the site puts up, waiting for a scheduled premiere to actually
// begin - and charging that against "record for 60 minutes" hands back a
// recording noticeably shorter than the number somebody set.
func TestLiveGuardStartsTheClockAtTheFirstLiveByte(t *testing.T) {
	g := newLiveGuard(Live{Enabled: true, MaxMinutes: 60})
	if g.active() {
		t.Fatalf("the guard was running before a single live line arrived")
	}
	if reason := g.exceeded(1 << 40); reason != "" {
		t.Errorf("an inactive guard stopped something: %q", reason)
	}
	start := time.Now()
	g.begin(start)
	g.begin(start.Add(time.Hour)) // a second line must not restart the clock
	if !g.started.Equal(start) {
		t.Errorf("the clock restarted on a later line: %v", g.started)
	}
}

func TestLiveGuardStopsAtTheMinuteLimit(t *testing.T) {
	start := time.Now()
	g := newLiveGuard(Live{Enabled: true, MaxMinutes: 60})
	g.now = func() time.Time { return start.Add(59 * time.Minute) }
	g.begin(start)
	if reason := g.exceeded(0); reason != "" {
		t.Errorf("stopped at 59 of 60 minutes: %q", reason)
	}
	g.now = func() time.Time { return start.Add(60 * time.Minute) }
	reason := g.exceeded(0)
	if reason == "" {
		t.Fatalf("did not stop at the 60 minute limit")
	}
	if !strings.Contains(reason, "60 minute") {
		t.Errorf("reason = %q, want it to name the limit and the number", reason)
	}
}

func TestLiveGuardStopsAtTheSizeLimit(t *testing.T) {
	g := newLiveGuard(Live{Enabled: true, MaxMB: 100})
	g.begin(time.Now())
	if reason := g.exceeded(99 << 20); reason != "" {
		t.Errorf("stopped at 99 of 100 MiB: %q", reason)
	}
	reason := g.exceeded(100 << 20)
	if !strings.Contains(reason, "100 MiB") {
		t.Errorf("reason = %q, want it to name the limit and the number", reason)
	}
}

// TestLiveGuardWithNoLimitsNeverStops: the switch is also what turns the note
// and the detection on, so somebody who wants to SEE that a link is a stream
// without capping it must not have it capped anyway.
func TestLiveGuardWithNoLimitsNeverStops(t *testing.T) {
	g := newLiveGuard(Live{Enabled: true})
	g.begin(time.Now().Add(-72 * time.Hour))
	if reason := g.exceeded(1 << 40); reason != "" {
		t.Errorf("a guard with no limits stopped a recording: %q", reason)
	}
}

// TestSanitizeFloorsNegativeLiveLimits: a negative number reaching the guard
// compares true on the very first progress line and stops the recording the
// instant it starts.
func TestSanitizeFloorsNegativeLiveLimits(t *testing.T) {
	o := Options{Live: Live{Enabled: true, MaxMinutes: -5, MaxMB: -1}}
	got := o.Sanitize().Live
	if got.MaxMinutes != 0 || got.MaxMB != 0 {
		t.Fatalf("Sanitize left %+v, want both caps floored to no limit", got)
	}
	g := newLiveGuard(got)
	g.begin(time.Now())
	if reason := g.exceeded(0); reason != "" {
		t.Errorf("a sanitized guard still stopped at zero bytes: %q", reason)
	}
}

func TestLiveNoteSaysWhatIsHappeningInsteadOfAShareOfNothing(t *testing.T) {
	start := time.Now()
	g := newLiveGuard(Live{Enabled: true})
	g.now = func() time.Time { return start.Add(83*time.Minute + 20*time.Second) }
	g.begin(start)
	note := g.note(3 << 30)
	if !strings.HasPrefix(note, "live recording,") {
		t.Errorf("note = %q, want it to say what the row is", note)
	}
	if !strings.Contains(note, "1:23:20") {
		t.Errorf("note = %q, want the running time", note)
	}
	if !strings.Contains(note, "3.0 GiB") {
		t.Errorf("note = %q, want what has arrived so far", note)
	}
}

func TestFormatDurationReadsLikeAClock(t *testing.T) {
	cases := map[time.Duration]string{
		0:                "0:00",
		45 * time.Second: "0:45",
		90 * time.Second: "1:30",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03",
		-time.Second: "0:00",
	}
	for in, want := range cases {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		1536:       "1.5 KiB",
		1 << 20:    "1.0 MiB",
		3 << 30:    "3.0 GiB",
		2048 << 30: "2.0 TiB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
