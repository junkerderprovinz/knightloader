package ytdlp

// run_test.go: Backend.run end to end, with this test binary standing in for
// yt-dlp AND for ffprobe.
//
// Everything the new wave does after "yt-dlp exited 0" - the NFO, the ffprobe
// measurement, the short-file warning, the empty-subtitle-task check, the
// live limits, the cookie file's whole lifetime - happens in run() and
// finish(), which no amount of buildArgs testing reaches. The self-re-exec
// trick is backend_test.go's (see probeHelperEnv there); this file needs a
// second helper because one run() spawns two different programs, and which
// one this process is playing is decided by the argv it was handed rather
// than by a second environment variable that the first child would also
// inherit.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// runHelperEnv carries two modes at once, "<yt-dlp mode>:<ffprobe mode>",
// because the same environment reaches both children.
const runHelperEnv = "KL_YTDLP_RUN_HELPER"

// runHelperMain never returns: it plays whichever of the two programs the
// argv it was given belongs to, then exits.
func runHelperMain(mode string) {
	ytMode, probeMode, _ := strings.Cut(mode, ":")
	// -print_format is ffprobe's flag and appears in no yt-dlp invocation
	// this package builds, which makes it a reliable way to tell the two
	// children apart without a second variable the first would also inherit.
	for _, a := range os.Args[1:] {
		if a == "-print_format" {
			ffprobeHelper(probeMode, os.Args[len(os.Args)-1])
		}
	}
	ytdlpHelper(ytMode)
}

// helperOutDir digs the destination directory out of the argv buildArgs
// produced: the plain -o (the one with no "TYPE:" prefix, so the music
// thumbnail template is not mistaken for it), minus its template part.
func helperOutDir() string {
	for i, a := range os.Args {
		if a != "-o" || i+1 >= len(os.Args) {
			continue
		}
		v := os.Args[i+1]
		if strings.HasPrefix(v, "thumbnail:") {
			continue
		}
		if j := strings.LastIndexAny(v, `/\`); j >= 0 {
			return v[:j]
		}
	}
	return "."
}

// helperInfoJSON is the info dict yt-dlp would have written beside the file:
// enough for the NFO to be worth writing and for the short-file check to have
// an announced duration to compare against. 2718.041 seconds is 45 minutes,
// which makes a "short" measurement obvious in a failure message.
const helperInfoJSON = `{"id":"dQw4w9WgXcQ","title":"A Video","description":"Line one.\nLine two.",` +
	`"uploader":"Some Channel","upload_date":"20260907","duration":2718.041,` +
	`"thumbnail":"https://example.invalid/t.jpg","webpage_url":"https://example.invalid/v",` +
	`"extractor_key":"Youtube","categories":["Music"],"tags":["a","b"]}`

func ytdlpHelper(mode string) {
	dir := helperOutDir()
	final := filepath.Join(dir, "A Video.mkv")
	switch mode {
	case "video":
		_ = os.WriteFile(final, []byte("bytes"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "A Video.info.json"), []byte(helperInfoJSON), 0o644)
		fmt.Println("[download] Destination: " + final)
		fmt.Println("KLP:" + `{"downloaded_bytes":5,"total_bytes":5,"speed":1.0,"filename":"` + jsonPath(final) + `"}`)
	case "merge":
		// The real pipeline for a video row: two half-streams, then the
		// merged file that replaces them. finishedFile has to end up on the
		// LAST of the three.
		_ = os.WriteFile(final, []byte("bytes"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "A Video.info.json"), []byte(helperInfoJSON), 0o644)
		fmt.Println("[download] Destination: " + filepath.Join(dir, "A Video.f137.mp4"))
		fmt.Println("[download] Destination: " + filepath.Join(dir, "A Video.f140.m4a"))
		fmt.Printf("[Merger] Merging formats into \"%s\"\n", final)
	case "nosubs":
		// A subtitle row for a language the source does not carry: --no-warnings
		// swallows yt-dlp's own complaint, so it exits 0 having said nothing.
	case "subs":
		fmt.Println("[info] Writing video subtitles to: " + filepath.Join(dir, "A Video.de.srt"))
	case "live":
		// A stream: is_live true on every progress line, no total ever, and
		// a quarter mebibyte more each time.
		//
		// The interrupt is IGNORED and the helper finishes on its own,
		// deliberately: what is under test here is the limit being noticed
		// and the recording being settled as a finished download, not the
		// shutdown handshake - and a helper that died on the signal would
		// make this test pass or fail for reasons that differ between Linux
		// (where the interrupt lands) and Windows (where it does not).
		signal.Ignore(os.Interrupt)
		for i := 1; i <= 8; i++ {
			fmt.Printf("KLP:{\"live\":\"True\",\"p\":{\"downloaded_bytes\":%d,\"speed\":1000.0}}\n", i*(256<<10))
			time.Sleep(5 * time.Millisecond)
		}
		_ = os.WriteFile(final, []byte("bytes"), 0o644)
		// A stream announces no runtime, which is what the info json for a
		// recording actually looks like - and the case the short-file check
		// has to stay silent on.
		_ = os.WriteFile(filepath.Join(dir, "A Video.info.json"),
			[]byte(`{"id":"live1","title":"Weekend Stream","uploader":"Some Channel"}`), 0o644)
		fmt.Println("[download] Destination: " + final)
	case "botcheck":
		// The one failing mode: yt-dlp's real bot-check line (the fixture lives
		// in diagnose_test.go, beside the phrase table that reads it) followed
		// by the chatter that used to be all a task ever saw, and then a
		// non-zero exit so run() takes its error path.
		fmt.Fprintln(os.Stderr, botCheckStderr)
		fmt.Fprintln(os.Stderr, "WARNING: unable to obtain file audio codec with ffprobe")
		os.Exit(1)
	case "cookies":
		// Records what run() actually handed over, so the test can check both
		// that the jar arrived and that the file is gone afterwards.
		_ = os.WriteFile(filepath.Join(dir, "argv.txt"), []byte(strings.Join(os.Args[1:], "\n")), 0o644)
		for i, a := range os.Args {
			if a == "--cookies" && i+1 < len(os.Args) {
				if b, err := os.ReadFile(os.Args[i+1]); err == nil {
					_ = os.WriteFile(filepath.Join(dir, "jar-seen.txt"), b, 0o644)
				}
			}
		}
	}
	os.Exit(0)
}

// jsonPath escapes a Windows path's backslashes so the helper's hand-built
// progress line stays valid JSON.
func jsonPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

func ffprobeHelper(mode, path string) {
	if _, err := os.Stat(path); err != nil {
		// ffprobe answering about a file that is not there is a real failure,
		// and finish() has to survive it without touching the task's status.
		os.Exit(1)
	}
	switch mode {
	case "short":
		// Half the announced 2718 seconds: an interrupted merge, which is
		// exactly what this check exists to catch.
		fmt.Println(`{"format":{"duration":"1359.0"},"streams":[` +
			`{"codec_type":"video","codec_name":"h264","width":1920,"height":1080},` +
			`{"codec_type":"audio","codec_name":"aac"}]}`)
	case "broken":
		os.Exit(1)
	default:
		fmt.Println(`{"format":{"duration":"2718.041"},"streams":[` +
			`{"codec_type":"video","codec_name":"h264","width":1920,"height":1080},` +
			`{"codec_type":"audio","codec_name":"aac"},` +
			`{"codec_type":"audio","codec_name":"opus"}]}`)
	}
	os.Exit(0)
}

// recorder collects every update one run produced, in order.
type recorder struct {
	mu  sync.Mutex
	ups []core.Update
}

func (r *recorder) add(_ string, u core.Update) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ups = append(r.ups, u)
}

func (r *recorder) last() core.Update {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ups) == 0 {
		return core.Update{}
	}
	return r.ups[len(r.ups)-1]
}

func (r *recorder) all() []core.Update {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.Update(nil), r.ups...)
}

// runFake drives one whole Backend.run against the helper and returns the
// destination directory plus every update it produced. run() is synchronous
// once called directly (Download is the goroutine-spawning wrapper), so there
// is nothing to wait on.
func runFake(t *testing.T, mode string, o Options) (string, *recorder) {
	t.Helper()
	t.Setenv(runHelperEnv, mode)
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.FFprobe = os.Args[0]
	b.Options = func(string) Options { return o }
	b.run("task-1", "https://example.invalid/watch?v=x")
	return dir, rec
}

func TestRunWritesTheNFOBesideTheFileAndTakesTheInfoJSONAwayAgain(t *testing.T) {
	dir, rec := runFake(t, "merge:full", Options{Embed: Embed{NFO: true}})

	if got := rec.last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
	nfo := filepath.Join(dir, "A Video.nfo")
	body, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatalf("no NFO beside the merged file: %v", err)
	}
	for _, want := range []string{"<movie>", "<title>A Video</title>", "<premiered>2026-09-07</premiered>", "Some Channel"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("NFO is missing %q:\n%s", want, body)
		}
	}
	// The json was scaffolding this backend asked for, not something the
	// person downloading a video switched on - leaving one per download
	// behind is a filesystem change nobody consented to.
	if _, err := os.Stat(filepath.Join(dir, "A Video.info.json")); !os.IsNotExist(err) {
		t.Errorf("the info json survived the download (stat err = %v)", err)
	}
}

// TestRunFindsTheMergedFileNotTheHalfStreams is finishedFile's own reason for
// keeping the LAST match: a merged video prints Destination twice for streams
// that no longer exist by the time the download ends, and writing the NFO
// beside one of those would put it next to a deleted file.
func TestRunFindsTheMergedFileNotTheHalfStreams(t *testing.T) {
	dir, _ := runFake(t, "merge:full", Options{Embed: Embed{NFO: true}})
	if _, err := os.Stat(filepath.Join(dir, "A Video.nfo")); err != nil {
		t.Fatalf("NFO was not written beside the merged file: %v", err)
	}
	for _, stray := range []string{"A Video.f137.nfo", "A Video.f140.nfo"} {
		if _, err := os.Stat(filepath.Join(dir, stray)); err == nil {
			t.Errorf("an NFO was written beside a half-stream (%s)", stray)
		}
	}
}

func TestRunPutsTheMeasurementOnTheTask(t *testing.T) {
	_, rec := runFake(t, "video:full", Options{Measure: Measure{Enabled: true}})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
	for _, want := range []string{"45:18", "1920x1080", "h264/aac", "2 audio tracks"} {
		if !strings.Contains(got.Note, want) {
			t.Errorf("note = %q, want it to carry %q", got.Note, want)
		}
	}
}

// TestRunWarnsLoudlyAboutAShortFile is the whole point of the measurement: an
// interrupted merge exits 0 with a plausible file, and nothing else in this
// backend can tell.
func TestRunWarnsLoudlyAboutAShortFile(t *testing.T) {
	_, rec := runFake(t, "video:short", Options{Measure: Measure{Enabled: true}})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done (the bytes are real, FailOnShort is off)", got)
	}
	if !strings.Contains(got.Note, "short file") {
		t.Errorf("note = %q, want the short-file warning", got.Note)
	}
	// Both numbers, because this is a claim somebody is going to check
	// against the player they are about to open.
	if !strings.Contains(got.Note, "22:39") || !strings.Contains(got.Note, "45:18") {
		t.Errorf("note = %q, want both the measured and the announced runtime", got.Note)
	}
}

func TestRunFailsAShortFileWhenAsked(t *testing.T) {
	_, rec := runFake(t, "video:short", Options{Measure: Measure{Enabled: true, FailOnShort: true}})
	got := rec.last()
	if got.Status != core.StatusError {
		t.Fatalf("last update = %+v, want Error with FailOnShort on", got)
	}
	if !strings.Contains(got.Err, "short file") {
		t.Errorf("err = %q, want the short-file warning", got.Err)
	}
}

// TestRunKeepsAFullLengthFileQuiet: a threshold that fires on an ordinary
// download trains people to ignore it, which is worse than not having it.
func TestRunKeepsAFullLengthFileQuiet(t *testing.T) {
	_, rec := runFake(t, "video:full", Options{Measure: Measure{Enabled: true}})
	if note := rec.last().Note; strings.Contains(note, "short file") {
		t.Errorf("note = %q, want no warning for a file that is the announced length", note)
	}
}

// TestRunSurvivesAnFFprobeThatFails: no ffprobe in the build, or a file on a
// mount that went away. Neither is a statement about whether the download
// worked, and "could not measure" on every finished task would be a permanent
// false alarm.
func TestRunSurvivesAnFFprobeThatFails(t *testing.T) {
	_, rec := runFake(t, "video:broken", Options{Measure: Measure{Enabled: true}})
	got := rec.last()
	if got.Status != core.StatusDone || got.Err != "" {
		t.Fatalf("last update = %+v, want a plain Done", got)
	}
}

// TestRunFailsASubtitleRowThatWroteNothing is the silent-empty-task bug:
// --no-warnings mutes "There are no subtitles for the requested languages",
// so the row exits 0 over an empty folder and settles green.
func TestRunFailsASubtitleRowThatWroteNothing(t *testing.T) {
	_, rec := runFake(t, "nosubs:full", Options{Variant: VariantSubtitle, SubtitleLangs: "de", SubtitleStrict: true})
	got := rec.last()
	if got.Status != core.StatusError {
		t.Fatalf("last update = %+v, want Error for a subtitle row that wrote no file", got)
	}
	if !strings.Contains(got.Err, "de") {
		t.Errorf("err = %q, want it to name the language that was asked for", got.Err)
	}
}

func TestRunLeavesASubtitleRowThatWroteAFileAlone(t *testing.T) {
	_, rec := runFake(t, "subs:full", Options{Variant: VariantSubtitle, SubtitleLangs: "de", SubtitleStrict: true})
	if got := rec.last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
}

// TestRunKeepsTodaysBehaviourWhenStrictIsOff pins the default: turning this
// on retroactively fails tasks an existing install has been settling green
// for months, which is a choice for the settings page and not for an upgrade.
func TestRunKeepsTodaysBehaviourWhenStrictIsOff(t *testing.T) {
	_, rec := runFake(t, "nosubs:full", Options{Variant: VariantSubtitle, SubtitleLangs: "de"})
	if got := rec.last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want the unchanged Done", got)
	}
}

// TestRunStopsALiveRecordingAtItsSizeLimit is point six's own failure: a
// weekend stream fills the disk behind a progress bar computed from a total
// nobody knows.
func TestRunStopsALiveRecordingAtItsSizeLimit(t *testing.T) {
	_, rec := runFake(t, "live:full", Options{Live: Live{Enabled: true, MaxMB: 1}})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done - a recording that hit its cap is finished, not failed", got)
	}
	if !strings.Contains(got.Note, "1 MiB limit") {
		t.Errorf("note = %q, want it to name the limit that stopped the recording", got.Note)
	}
	// And the running rows said what they were, rather than showing a share
	// of a total that does not exist.
	var sawRecording bool
	for _, u := range rec.all() {
		if strings.HasPrefix(u.Note, "live recording,") {
			sawRecording = true
		}
	}
	if !sawRecording {
		t.Errorf("no update marked the task as a recording: %+v", rec.all())
	}
}

// TestRunStillMeasuresARecordingThatHitItsCap is the trap in stopping a
// download from inside run(): the guard cancels the context to stop yt-dlp,
// and handing that same, now-cancelled context to ffprobe would mean the
// measurement silently never ran for exactly the downloads somebody put a cap
// on - the ones most likely to have ended somewhere unexpected.
func TestRunStillMeasuresARecordingThatHitItsCap(t *testing.T) {
	_, rec := runFake(t, "live:full", Options{
		Live:    Live{Enabled: true, MaxMB: 1},
		Measure: Measure{Enabled: true},
	})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
	if !strings.Contains(got.Note, "1 MiB limit") {
		t.Errorf("note = %q, want the limit that stopped it", got.Note)
	}
	if !strings.Contains(got.Note, "1920x1080") {
		t.Errorf("note = %q, want the measurement alongside it", got.Note)
	}
	// A stream announces no runtime, so there is nothing to be short OF -
	// and a warning here would fire on every single recording.
	if strings.Contains(got.Note, "short file") {
		t.Errorf("note = %q, want no short-file warning for a recording", got.Note)
	}
}

// TestRunWithoutTheLiveSwitchReadsTheOldProgressShape is the other half of
// that: an install with the guard off runs the progress template it always
// ran, and every byte still has to arrive on the task.
func TestRunWithoutTheLiveSwitchReadsTheOldProgressShape(t *testing.T) {
	_, rec := runFake(t, "video:full", Options{})
	var loaded int64
	for _, u := range rec.all() {
		if u.Loaded > loaded {
			loaded = u.Loaded
		}
		if u.Note != "" {
			t.Errorf("an ordinary download carried a note: %q", u.Note)
		}
	}
	if loaded != 5 {
		t.Errorf("progress never reached the task (max Loaded = %d, want 5)", loaded)
	}
}

// TestRunHandsTheCookieJarOverAndTakesItAwayAgain is the whole lifecycle of a
// live session in one test: written 0600, passed as --cookies, readable by
// the child, gone the moment the child is.
func TestRunHandsTheCookieJarOverAndTakesItAwayAgain(t *testing.T) {
	const jar = "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\ttop-secret-session\n"
	t.Setenv(runHelperEnv, "cookies:full")
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.Options = func(string) Options { return Options{Cookies: true} }
	b.Cookies = func(string) string { return jar }
	b.run("task-1", "https://youtube.com/watch?v=x")

	argv, err := os.ReadFile(filepath.Join(dir, "argv.txt"))
	if err != nil {
		t.Fatalf("the helper recorded no argv: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(argv)), "\n")
	var cookiePath string
	for i, a := range lines {
		if a == "--cookies" && i+1 < len(lines) {
			cookiePath = lines[i+1]
		}
	}
	if cookiePath == "" {
		t.Fatalf("--cookies was never passed: %v", lines)
	}
	seen, err := os.ReadFile(filepath.Join(dir, "jar-seen.txt"))
	if err != nil {
		t.Fatalf("the child could not read the jar: %v", err)
	}
	if string(seen) != jar {
		t.Errorf("the child read %q, want the stored jar verbatim", seen)
	}
	if _, err := os.Stat(cookiePath); !os.IsNotExist(err) {
		t.Errorf("the cookie file outlived the download at %s (stat err = %v)", cookiePath, err)
	}
}

// TestRunNeverPutsTheCookieTextInAnUpdate is the rule cookies.go's file
// comment states, asserted rather than asserted-in-prose: an Err or a Note
// travels into the task, into the log ring and from there into the
// diagnostics bundle somebody attaches to a public bug report.
func TestRunNeverPutsTheCookieTextInAnUpdate(t *testing.T) {
	const secret = "top-secret-session"
	jar := "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\t" + secret + "\n"
	t.Setenv(runHelperEnv, "cookies:full")
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.Options = func(string) Options { return Options{Cookies: true} }
	b.Cookies = func(string) string { return jar }
	b.run("task-1", "https://youtube.com/watch?v=x")

	for _, u := range rec.all() {
		blob, _ := json.Marshal(u)
		if strings.Contains(string(blob), secret) {
			t.Fatalf("an update carried the cookie text: %s", blob)
		}
	}
}

// TestRunNamesTheCauseOnTheFailedUpdate is the wiring for the whole feature:
// Diagnose can be as right as it likes in its own tests, and until run() puts
// its answer on the Update nothing downstream has anything to act on.
//
// It asserts the Reason and not the Err on purpose. The sentence is what a
// person reads; the reason is what the retry policy, the reconnect veto and
// the interface's one useful button all key on.
func TestRunNamesTheCauseOnTheFailedUpdate(t *testing.T) {
	_, rec := runFake(t, "botcheck:full", Options{})

	got := rec.last()
	if got.Status != core.StatusError {
		t.Fatalf("last update = %+v, want Error", got)
	}
	if got.Reason != core.ReasonBotCheck {
		t.Errorf("Reason = %q, want %q - the diagnosis never reached the update", got.Reason, core.ReasonBotCheck)
	}
	// And the sentence beside it is the ERROR line rather than the warning
	// that followed it, with the identifying words still in it.
	if !strings.Contains(got.Err, "not a bot") {
		t.Errorf("Err = %q, want the phrase that names the failure to reach the task as well", got.Err)
	}
	if got.Unsupported {
		t.Error("a bot check was handed to the next backend as though yt-dlp did not claim the link")
	}
}

// TestRunAsksForNoCookiesUntilTheSwitchIsOn: a jar pasted in once must not
// start being sent on its own - a site's rate limits and bans land on the
// account, not on the address.
func TestRunAsksForNoCookiesUntilTheSwitchIsOn(t *testing.T) {
	t.Setenv(runHelperEnv, "cookies:full")
	dir := t.TempDir()
	asked := false
	b := NewBackend(os.Args[0], dir, func(string, core.Update) {})
	b.Options = func(string) Options { return Options{} }
	b.Cookies = func(string) string { asked = true; return "jar" }
	b.run("task-1", "https://youtube.com/watch?v=x")

	if asked {
		t.Errorf("the cookie store was read with Options.Cookies off")
	}
	argv, err := os.ReadFile(filepath.Join(dir, "argv.txt"))
	if err != nil {
		t.Fatalf("the helper recorded no argv: %v", err)
	}
	if strings.Contains(string(argv), "--cookies") {
		t.Errorf("--cookies was passed with the switch off: %s", argv)
	}
}
