package ytdlp

import (
	"context"
	"fmt"
	"os"
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
	switch os.Getenv(probeHelperEnv) {
	case "":
		os.Exit(m.Run())
	case "title":
		// A title yt-dlp could plausibly hand back verbatim: mixed case,
		// punctuation, nothing that needs escaping.
		fmt.Println("Rick Astley - Never Gonna Give You Up (Official Video)")
		os.Exit(0)
	case "playlist":
		// Stands in for the --flat-playlist gap ProbeTitle's own doc comment
		// names: multiple lines out, one per entry.
		fmt.Println("Entry One")
		fmt.Println("Entry Two")
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
	if got != want {
		t.Errorf("ProbeTitle = %q, want %q", got, want)
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
	if got != "Entry One" {
		t.Errorf("ProbeTitle = %q, want the first entry's title", got)
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
		t.Fatalf("ProbeTitle returned no error for a failing invocation (title = %q)", got)
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
		t.Fatalf("ProbeTitle returned no error for empty output (title = %q)", got)
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
		t.Fatalf("ProbeTitle returned no error for a context that expired (title = %q)", got)
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
