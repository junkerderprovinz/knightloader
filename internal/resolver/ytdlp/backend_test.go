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

// probeHelperEnv selects TestMain's fake-yt-dlp behaviour below when this
// package's own test binary is re-executed as ProbeTitle's "yt-dlp binary" -
// empty means "run the real tests".
//
// This is the same self-re-exec trick internal/provision's own
// TestSleeperHelperProcess uses (see provision_test.go) - os.Args[0] under
// `go test` is the path to the compiled test binary, so pointing Backend.bin
// at it makes exec.CommandContext launch this very package's tests as the
// child process, with no external program anywhere near these tests and
// identical behaviour on every platform the module builds for.
//
// It has to be a TestMain rather than a second `go test -test.run=...` test
// function (provision_test.go's own shape): ProbeTitle's argv is fixed
// production code (--skip-download --no-warnings --print %(title)s <url>),
// none of it a recognised go-test flag, so a re-executed binary would fail
// flag.Parse with "flag provided but not defined" before ever reaching a
// test function. TestMain runs before testing.Main touches the command
// line, so it can act on probeHelperEnv and exit before flag.Parse is ever
// called at all.
const probeHelperEnv = "KL_YTDLP_PROBE_HELPER"

func TestMain(m *testing.M) {
	// run_test.go's own helper gets first refusal, because a run() test needs
	// this binary to stand in for TWO programs at once (yt-dlp and ffprobe)
	// and therefore cannot share probeHelperEnv's one-mode-per-process
	// switch - see runHelperMain for how it tells the two callers apart.
	if mode := os.Getenv(runHelperEnv); mode != "" {
		runHelperMain(mode)
	}
	switch os.Getenv(probeHelperEnv) {
	case "":
		os.Exit(m.Run())
	case "title":
		// A title yt-dlp could plausibly hand back verbatim: mixed case,
		// punctuation, nothing that needs escaping. -j prints one JSON
		// object per line - this stands in for that, with no formats at
		// all (a source ProbeTitle's own format-list logic never sees).
		fmt.Println(`{"title":"Rick Astley - Never Gonna Give You Up (Official Video)","formats":[]}`)
		os.Exit(0)
	case "formats":
		// A source with a real, mixed format list - two video-only tracks
		// (144p/1080p), one audio-only track, one combined progressive
		// track (has BOTH a real vcodec and a real acodec) that ProbeTitle
		// must not misfile as video-only, exercising the exact
		// vcodec/acodec-both-real branch neither of isVideo/isAudio in
		// applyProbeFormats treats as mutually exclusive.
		fmt.Println(`{"title":"Formats Video","formats":[` +
			`{"format_id":"160","ext":"mp4","vcodec":"avc1.4d400b","acodec":"none","height":144,"filesize":195278},` +
			`{"format_id":"137","ext":"mp4","vcodec":"avc1.640028","acodec":"none","height":1080,"filesize_approx":52428800},` +
			`{"format_id":"140","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","filesize":3145728},` +
			`{"format_id":"18","ext":"mp4","vcodec":"avc1.42001E","acodec":"mp4a.40.2","height":360,"filesize":8388608}` +
			`]}`)
		os.Exit(0)
	case "languages":
		// A source with both kinds of subtitle track and with YouTube's own
		// auto-dubbing: one hand-written German track, automatic captions in
		// three languages (English among them, which the manual list does NOT
		// have - the exact case a default of "en" downloads nothing for), and
		// three audio formats whose language_preference says which one was
		// actually spoken.
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
		// A stream in progress. live_status carries it and the older boolean
		// does not, which is the combination ProbeTitle has to read as live -
		// and duration is absent, because a stream that has not ended has no
		// length to announce.
		fmt.Println(`{"title":"Weekend Stream","live_status":"is_live","formats":[]}`)
		os.Exit(0)
	case "waslive":
		// The other half of that pair: a finished stream. It must NOT read as
		// live - it is an ordinary recording with an ordinary length, and the
		// recording caps have no business on it.
		fmt.Println(`{"title":"Yesterday's Stream","was_live":true,"live_status":"was_live","duration":7200,"formats":[]}`)
		os.Exit(0)
	case "playlist":
		// Stands in for the --flat-playlist gap ProbeTitle's own doc comment
		// names: multiple lines out, one per entry.
		fmt.Println(`{"title":"Entry One","formats":[]}`)
		fmt.Println(`{"title":"Entry Two","formats":[]}`)
		os.Exit(0)
	case "flatlisting":
		// What --flat-playlist -J answers for a real playlist: ONE object for
		// the whole listing, with the playlist's own title (what the package
		// gets named after) and one flat entry per video. The last two entries
		// are the two shapes parsePlaylist has to leave out rather than stage -
		// a nested playlist (a channel tab) and an entry naming no URL at all
		// (a removed video the listing still carries).
		fmt.Println(`{"_type":"playlist","title":"Greatest Hits","entries":[` +
			`{"_type":"url","url":"https://youtube.com/watch?v=aaa","title":"First Song"},` +
			`{"_type":"url","url":"https://youtube.com/watch?v=bbb","title":"Second Song"},` +
			`{"_type":"playlist","url":"https://youtube.com/playlist?list=inner","title":"A Playlist Inside"},` +
			`{"_type":"url","url":"","title":"Deleted video"}` +
			`]}`)
		os.Exit(0)
	case "singlevideo":
		// The same call against an ordinary video URL: yt-dlp answers with the
		// video's own info dict, which names no _type of "playlist" and carries
		// no entries. ProbePlaylist must read that as "this link lists
		// nothing", never as a failure - it is the answer every link on an
		// install with the setting on gets, and the caller stages such a link
		// exactly as it always did.
		fmt.Println(`{"title":"Rick Astley - Never Gonna Give You Up (Official Video)","formats":[]}`)
		os.Exit(0)
	case "echoargs":
		// The listing's title is this process's own argv, so a test can assert
		// what ProbePlaylist actually asked yt-dlp for - above all that it
		// asked for the LISTING (--flat-playlist) rather than for the videos.
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

// fakeYtdlpBackend is a Backend whose "yt-dlp binary" is this test binary
// itself, re-executed with probeHelperEnv set to mode - see TestMain above.
// t.Setenv puts mode in this process's own environment, which
// Backend.ProbeTitle's cmd.Env = append(os.Environ(), ...) then hands
// straight to the child, so no production code needs to know a test is
// driving it.
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

// TestProbeTitleReturnsTheParsedFormats is the format-list half ("man soll
// nur die varianten auswählen können die wirklich verfügbar sind" /
// "dateiendungen... größe" - jdp, 2026-08-25): every field
// applyProbeFormats actually reads must survive the JSON round trip
// correctly, including telling a video-only track apart from an
// audio-only one and from a combined progressive track that carries both a
// real vcodec AND a real acodec at once.
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

// TestProbeTitleReturnsErrorOnUnparseableJSON covers a yt-dlp whose own -j
// output could not be decoded at all - a corrupt/truncated line, or (as
// stood in for here) a caller mistakenly pointed at a binary that isn't
// yt-dlp. The caller must see an error, never a zero-value ProbeResult
// mistaken for "a source with a real but empty format list".
func TestProbeTitleReturnsErrorOnUnparseableJSON(t *testing.T) {
	b := fakeYtdlpBackend(t, "badjson")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=x")
	if err == nil {
		t.Fatalf("ProbeTitle returned no error for unparseable output (title = %q)", got.Title)
	}
}

// TestProbeTitleTakesTheFirstLineOfAMultiLineAnswer pins the documented
// --flat-playlist gap: without that flag, a playlist/channel URL can print
// one title per entry, and ProbeTitle's own doc comment says the first line
// is the best single answer available rather than an error.
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

// TestProbeTitleReturnsErrorOnAFailingInvocation covers a yt-dlp that exits
// non-zero (an unsupported/unavailable link) - the caller (app.
// probeYtdlpTitle) must see an error and not a made-up title, and nothing
// here may panic on the way.
func TestProbeTitleReturnsErrorOnAFailingInvocation(t *testing.T) {
	b := fakeYtdlpBackend(t, "fail")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=gone")
	if err == nil {
		t.Fatalf("ProbeTitle returned no error for a failing invocation (title = %q)", got.Title)
	}
}

// TestProbeTitleReturnsErrorOnEmptyOutput covers a yt-dlp that exits 0 but
// prints nothing - --print with a template field yt-dlp could not fill
// prints an empty line rather than failing, and an empty string is not a
// name any caller should ever write onto a task.
func TestProbeTitleReturnsErrorOnEmptyOutput(t *testing.T) {
	b := fakeYtdlpBackend(t, "empty")
	got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=x")
	if err == nil {
		t.Fatalf("ProbeTitle returned no error for empty output (title = %q)", got.Title)
	}
}

// TestProbeTitleTimesOutWithoutPanicking is the other failure mode a
// background probe has to survive cleanly: a yt-dlp invocation that never
// returns on its own. The caller (probeYtdlpTitle) relies on ctx alone to
// bound this - see ProbeTitle's own doc comment on why the timeout is not
// baked into this package.
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
	// Generous margin above the 300ms deadline: proves the context actually
	// bounded the wait rather than ProbeTitle silently ignoring ctx and
	// blocking for the helper's full one-minute sleep.
	if elapsed > 5*time.Second {
		t.Fatalf("ProbeTitle took %v to return after its context expired", elapsed)
	}
}

// TestFirstLineSkipsBlankLines guards the helper ProbeTitle reads its answer
// through directly, independent of spawning any process at all.
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
