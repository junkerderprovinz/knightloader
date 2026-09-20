package ytdlp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// probeHelperEnv makes this test binary act as a fake yt-dlp when a test sets
// it and points Backend.bin at os.Args[0].
//
// It needs TestMain rather than a helper test function: yt-dlp's arguments are
// no go-test flags, so the re-executed binary would fail flag parsing before
// reaching any test.
const probeHelperEnv = "KL_YTDLP_PROBE_HELPER"

func TestMain(m *testing.M) {
	// run_test.go's helper comes first: it stands in for yt-dlp and ffprobe
	// in one run and tells them apart itself (see runHelperMain).
	if mode := os.Getenv(runHelperEnv); mode != "" {
		runHelperMain(mode)
	}
	switch os.Getenv(probeHelperEnv) {
	case "":
		os.Exit(m.Run())
	case "title":
		fmt.Println(`{"title":"Rick Astley - Never Gonna Give You Up (Official Video)","formats":[]}`)
		os.Exit(0)
	case "formats":
		// Two video-only tracks, one audio-only and one progressive track
		// carrying both codecs.
		fmt.Println(`{"title":"Formats Video","formats":[` +
			`{"format_id":"160","ext":"mp4","vcodec":"avc1.4d400b","acodec":"none","height":144,"filesize":195278},` +
			`{"format_id":"137","ext":"mp4","vcodec":"avc1.640028","acodec":"none","height":1080,"filesize_approx":52428800},` +
			`{"format_id":"140","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","filesize":3145728},` +
			`{"format_id":"18","ext":"mp4","vcodec":"avc1.42001E","acodec":"mp4a.40.2","height":360,"filesize":8388608}` +
			`]}`)
		os.Exit(0)
	case "languages":
		// Manual subtitles without English, automatic captions with it, and
		// auto-dubbed audio whose language_preference marks the original.
		fmt.Println(`{"title":"Mehrsprachig","duration":2718.041,` +
			`"subtitles":{"de":[{"ext":"vtt"}],"fr":[{"ext":"vtt"}]},` +
			`"automatic_captions":{"en":[{"ext":"vtt"}],"de":[{"ext":"vtt"}],"es":[{"ext":"vtt"}]},` +
			`"formats":[` +
			`{"format_id":"140-0","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","language":"de","language_preference":10},` +
			`{"format_id":"140-1","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","language":"en","language_preference":-1},` +
			`{"format_id":"140-2","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","language":"en","language_preference":-1},` +
			`{"format_id":"137","ext":"mp4","vcodec":"avc1.640028","acodec":"none","height":1080}` +
			`]}`)
		os.Exit(0)
	case "live":
		// A running stream flagged only by live_status, without a duration.
		fmt.Println(`{"title":"Weekend Stream","live_status":"is_live","formats":[]}`)
		os.Exit(0)
	case "waslive":
		// A finished stream, which is an ordinary recording.
		fmt.Println(`{"title":"Yesterday's Stream","was_live":true,"live_status":"was_live","duration":7200,"formats":[]}`)
		os.Exit(0)
	case "playlist":
		// A playlist probed without --flat-playlist: one line per entry.
		fmt.Println(`{"title":"Entry One","formats":[]}`)
		fmt.Println(`{"title":"Entry Two","formats":[]}`)
		os.Exit(0)
	case "flatlisting":
		// --flat-playlist -J: one object with the playlist title. The last two
		// entries must be dropped: a nested playlist and a removed video
		// without a URL.
		fmt.Println(`{"_type":"playlist","title":"Greatest Hits","entries":[` +
			`{"_type":"url","url":"https://youtube.com/watch?v=aaa","title":"First Song"},` +
			`{"_type":"url","url":"https://youtube.com/watch?v=bbb","title":"Second Song"},` +
			`{"_type":"playlist","url":"https://youtube.com/playlist?list=inner","title":"A Playlist Inside"},` +
			`{"_type":"url","url":"","title":"Deleted video"}` +
			`]}`)
		os.Exit(0)
	case "singlevideo":
		// An ordinary video's info dict: no playlist type, no entries.
		fmt.Println(`{"title":"Rick Astley - Never Gonna Give You Up (Official Video)","formats":[]}`)
		os.Exit(0)
	case "echoargs":
		// Echoes the arguments as the playlist title.
		fmt.Printf("{\"_type\":\"playlist\",\"title\":%q,\"entries\":[]}\n", strings.Join(os.Args[1:], " "))
		os.Exit(0)
	case "badjson":
		fmt.Println("not json at all")
		os.Exit(0)
	case "fail":
		fmt.Fprintln(os.Stderr, "ERROR: [youtube] abc123: Video unavailable")
		os.Exit(1)
	case "empty":
		os.Exit(0)
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

// fakeYtdlpBackend is a Backend whose yt-dlp is this test binary in the given
// mode; the child inherits the environment set here.
func fakeYtdlpBackend(t *testing.T, mode string) *Backend {
	t.Helper()
	t.Setenv(probeHelperEnv, mode)
	return NewBackend(os.Args[0], t.TempDir(), func(string, core.Update) {})
}

func TestProbeTitleReturnsTheParsedTitleOnSuccess(t *testing.T) {
	b := fakeYtdlpBackend(t, "title")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	want := "Rick Astley - Never Gonna Give You Up (Official Video)"
	if got.Title != want {
		t.Errorf("ProbeTitle().Title = %q, want %q", got.Title, want)
	}
	if len(got.Formats) != 0 {
		t.Errorf("ProbeTitle().Formats = %+v, want none for a source with an empty formats array", got.Formats)
	}
}

// Video-only, audio-only and progressive tracks must stay distinguishable.
func TestProbeTitleReturnsTheParsedFormats(t *testing.T) {
	b := fakeYtdlpBackend(t, "formats")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=formats")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if len(got.Formats) != 4 {
		t.Fatalf("ProbeTitle().Formats has %d entries, want 4: %+v", len(got.Formats), got.Formats)
	}
	videoOnly := got.Formats[1] // format_id "137", 1080p
	if videoOnly.FormatID != "137" || videoOnly.Height != 1080 || videoOnly.Vcodec == "none" || videoOnly.Acodec != "none" {
		t.Errorf("video-only entry = %+v, want format_id 137, height 1080, a real vcodec, acodec \"none\"", videoOnly)
	}
	if videoOnly.Filesize != 0 || videoOnly.FilesizeApprox != 52428800 {
		t.Errorf("video-only entry sizes = filesize=%d filesize_approx=%d, want filesize 0 (never reported) and filesize_approx 52428800", videoOnly.Filesize, videoOnly.FilesizeApprox)
	}
	audioOnly := got.Formats[2] // format_id "140"
	if audioOnly.FormatID != "140" || audioOnly.Vcodec != "none" || audioOnly.Acodec == "none" || audioOnly.Filesize != 3145728 {
		t.Errorf("audio-only entry = %+v, want format_id 140, vcodec \"none\", a real acodec, filesize 3145728", audioOnly)
	}
	progressive := got.Formats[3] // format_id "18", has BOTH a real vcodec and a real acodec
	if progressive.FormatID != "18" || progressive.Vcodec == "none" || progressive.Acodec == "none" || progressive.Height != 360 {
		t.Errorf("progressive entry = %+v, want format_id 18, a real vcodec AND a real acodec, height 360", progressive)
	}
}

// Unparseable output must not pass as a source with no formats.
func TestProbeTitleReturnsErrorOnUnparseableJSON(t *testing.T) {
	b := fakeYtdlpBackend(t, "badjson")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=x")
	if err == nil {
		t.Fatalf("ProbeTitle returned no error for unparseable output (title = %q)", got.Title)
	}
}

func TestProbeTitleTakesTheFirstLineOfAMultiLineAnswer(t *testing.T) {
	b := fakeYtdlpBackend(t, "playlist")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/playlist?list=x")
	if err != nil {
		t.Fatalf("ProbeTitle: %v", err)
	}
	if got.Title != "Entry One" {
		t.Errorf("ProbeTitle().Title = %q, want the first entry's title", got.Title)
	}
}

func TestProbeTitleReturnsErrorOnAFailingInvocation(t *testing.T) {
	b := fakeYtdlpBackend(t, "fail")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=gone")
	if err == nil {
		t.Fatalf("ProbeTitle returned no error for a failing invocation (title = %q)", got.Title)
	}
}

func TestProbeTitleReturnsErrorOnEmptyOutput(t *testing.T) {
	b := fakeYtdlpBackend(t, "empty")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=x")
	if err == nil {
		t.Fatalf("ProbeTitle returned no error for empty output (title = %q)", got.Title)
	}
}

// The caller bounds a hanging yt-dlp through ctx alone.
func TestProbeTitleTimesOutWithoutPanicking(t *testing.T) {
	b := fakeYtdlpBackend(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	got, err := b.ProbeTitle(ctx, "https://youtube.com/watch?v=slow")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("ProbeTitle returned no error for a context that expired (title = %q)", got.Title)
	}
	// Far below the helper's one-minute sleep.
	if elapsed > 5*time.Second {
		t.Fatalf("ProbeTitle took %v to return after its context expired", elapsed)
	}
}

func TestFirstLineSkipsBlankLines(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"\n\n":                "",
		"Title\n":             "Title",
		"  Title  \n":         "Title",
		"\n\nTitle\nSecond\n": "Title",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
