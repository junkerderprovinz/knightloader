package ytdlp

// These tests run Backend.run end to end with this test binary standing in for
// both yt-dlp and ffprobe. One run spawns both, so the helper tells them apart
// by argv rather than by an environment variable both children inherit.

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
	// -print_format appears only in the ffprobe invocation.
	for _, a := range os.Args[1:] {
		if a == "-print_format" {
			ffprobeHelper(probeMode, os.Args[len(os.Args)-1])
		}
	}
	ytdlpHelper(ytMode)
}

// helperOutDir returns the directory of the plain -o template, skipping the
// "thumbnail:" one music mode adds.
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

// helperInfoJSON is the info dict yt-dlp would have written beside the file,
// with an announced duration of about 45 minutes.
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
		// Two half-streams, then the merged file that replaces them.
		_ = os.WriteFile(final, []byte("bytes"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "A Video.info.json"), []byte(helperInfoJSON), 0o644)
		fmt.Println("[download] Destination: " + filepath.Join(dir, "A Video.f137.mp4"))
		fmt.Println("[download] Destination: " + filepath.Join(dir, "A Video.f140.m4a"))
		fmt.Printf("[Merger] Merging formats into \"%s\"\n", final)
	case "nosubs":
		// A language the source lacks: exit 0 without a word.
	case "subs":
		fmt.Println("[info] Writing video subtitles to: " + filepath.Join(dir, "A Video.de.srt"))
	case "live":
		// A stream with is_live on every line and no total. The interrupt is
		// ignored so the result does not depend on whether the platform
		// delivers it.
		signal.Ignore(os.Interrupt)
		for i := 1; i <= 8; i++ {
			fmt.Printf("KLP:{\"live\":\"True\",\"p\":{\"downloaded_bytes\":%d,\"speed\":1000.0}}\n", i*(256<<10))
			time.Sleep(5 * time.Millisecond)
		}
		_ = os.WriteFile(final, []byte("bytes"), 0o644)
		// A recording's info json announces no runtime.
		_ = os.WriteFile(filepath.Join(dir, "A Video.info.json"),
			[]byte(`{"id":"live1","title":"Weekend Stream","uploader":"Some Channel"}`), 0o644)
		fmt.Println("[download] Destination: " + final)
	case "botcheck":
		// The real bot-check line (diagnose_test.go), a trailing warning and
		// a non-zero exit.
		fmt.Fprintln(os.Stderr, botCheckStderr)
		fmt.Fprintln(os.Stderr, "WARNING: unable to obtain file audio codec with ffprobe")
		os.Exit(1)
	case "cookies":
		// Records the argv and the jar the child could read.
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
		os.Exit(1)
	}
	switch mode {
	case "short":
		// Half the announced 2718 seconds.
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

// runFake drives one Backend.run against the helper and returns the
// destination directory and every update. run is synchronous when called
// directly.
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
	// Nobody asked for the info json itself.
	if _, err := os.Stat(filepath.Join(dir, "A Video.info.json")); !os.IsNotExist(err) {
		t.Errorf("the info json survived the download (stat err = %v)", err)
	}
}

// The half-streams named by the Destination lines no longer exist after the
// merge.
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

// An interrupted merge exits 0 with a plausible file.
func TestRunWarnsAboutAShortFile(t *testing.T) {
	_, rec := runFake(t, "video:short", Options{Measure: Measure{Enabled: true}})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done (the bytes are real, FailOnShort is off)", got)
	}
	if !strings.Contains(got.Note, "short file") {
		t.Errorf("note = %q, want the short-file warning", got.Note)
	}
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

func TestRunKeepsAFullLengthFileQuiet(t *testing.T) {
	_, rec := runFake(t, "video:full", Options{Measure: Measure{Enabled: true}})
	if note := rec.last().Note; strings.Contains(note, "short file") {
		t.Errorf("note = %q, want no warning for a file that is the announced length", note)
	}
}

// A failing ffprobe says nothing about the download.
func TestRunSurvivesAnFFprobeThatFails(t *testing.T) {
	_, rec := runFake(t, "video:broken", Options{Measure: Measure{Enabled: true}})
	got := rec.last()
	if got.Status != core.StatusDone || got.Err != "" {
		t.Fatalf("last update = %+v, want a plain Done", got)
	}
}

// --no-warnings hides "There are no subtitles for the requested languages".
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

func TestRunFinishesAnEmptySubtitleRowWhenStrictIsOff(t *testing.T) {
	_, rec := runFake(t, "nosubs:full", Options{Variant: VariantSubtitle, SubtitleLangs: "de"})
	if got := rec.last(); got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want the unchanged Done", got)
	}
}

func TestRunStopsALiveRecordingAtItsSizeLimit(t *testing.T) {
	_, rec := runFake(t, "live:full", Options{Live: Live{Enabled: true, MaxMB: 1}})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done - a recording that hit its cap is finished, not failed", got)
	}
	if !strings.Contains(got.Note, "1 MiB limit") {
		t.Errorf("note = %q, want it to name the limit that stopped the recording", got.Note)
	}
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

// The guard cancels run's context to stop yt-dlp, and ffprobe must not use
// that context.
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
	// A stream announces no runtime to be short of.
	if strings.Contains(got.Note, "short file") {
		t.Errorf("note = %q, want no short-file warning for a recording", got.Note)
	}
}

func TestRunWithoutTheLiveSwitchReadsThePlainProgressShape(t *testing.T) {
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

// The jar is passed as --cookies, readable by the child and gone once it
// exits.
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

// Updates reach the task, the log and the diagnostics bundle.
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

// The Reason, not the Err text, is what retries, the reconnect veto and the
// interface act on.
func TestRunNamesTheCauseOnTheFailedUpdate(t *testing.T) {
	_, rec := runFake(t, "botcheck:full", Options{})

	got := rec.last()
	if got.Status != core.StatusError {
		t.Fatalf("last update = %+v, want Error", got)
	}
	if got.Reason != core.ReasonBotCheck {
		t.Errorf("Reason = %q, want %q - the diagnosis never reached the update", got.Reason, core.ReasonBotCheck)
	}
	// The ERROR line, not the warning after it.
	if !strings.Contains(got.Err, "not a bot") {
		t.Errorf("Err = %q, want the phrase that names the failure to reach the task as well", got.Err)
	}
	if got.Unsupported {
		t.Error("a bot check was handed to the next backend as though yt-dlp did not claim the link")
	}
}

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
