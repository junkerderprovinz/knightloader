package ytdlp

// These tests run Backend.run end to end with this test binary standing in for
// both yt-dlp and ffprobe. One run spawns both, so the helper tells them apart
// by argv rather than by an environment variable both children inherit.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/workdir"
)

// runHelperEnv carries two modes at once, "<yt-dlp mode>:<ffprobe mode>",
// because the same environment reaches both children.
const runHelperEnv = "KL_YTDLP_RUN_HELPER"

// runHelperMain never returns: it plays whichever of the two programs the
// argv it was given belongs to, then exits.
func runHelperMain(mode string) {
	if len(os.Args) == 3 && os.Args[1] == recordArg {
		ffmpegRecorder(os.Args[2])
	}
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
		// Two half-streams, then the merged file that replaces them. The
		// progress lines count only the stream being fetched.
		audio := filepath.Join(dir, "A Video.f140.m4a")
		_ = os.WriteFile(final, []byte("merged bytes"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "A Video.info.json"), []byte(helperInfoJSON), 0o644)
		fmt.Println("[download] Destination: " + filepath.Join(dir, "A Video.f137.mp4"))
		fmt.Println("[download] Destination: " + audio)
		fmt.Println("KLP:" + `{"downloaded_bytes":5,"total_bytes":5,"speed":1.0,"filename":"` + jsonPath(audio) + `"}`)
		fmt.Printf("[Merger] Merging formats into \"%s\"\n", final)
	case "nosubs":
		// A language the source lacks: exit 0 without a word.
	case "subs":
		sub := filepath.Join(dir, "A Video.de.srt")
		_ = os.WriteFile(sub, []byte("1\n00:00:01,000 --> 00:00:02,000\nHallo\n"), 0o644)
		fmt.Println("[info] Writing video subtitles to: " + sub)
	case "vttsubs", "vttsubs2":
		// A source with WebVTT only, converted by --convert-subs, in the
		// order and wording yt-dlp 2026.08.19 prints.
		langs := []string{"de"}
		if mode == "vttsubs2" {
			langs = []string{"en", "de"}
		}
		var vtts []string
		for _, lang := range langs {
			vtt := filepath.Join(dir, "A Video."+lang+".vtt")
			_ = os.WriteFile(vtt, []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHallo\n"), 0o644)
			fmt.Println("[info] Writing video subtitles to: " + vtt)
			fmt.Println("[download] Destination: " + vtt)
			vtts = append(vtts, vtt)
		}
		fmt.Println("[SubtitlesConvertor] Converting subtitles")
		for _, vtt := range vtts {
			_ = os.WriteFile(strings.TrimSuffix(vtt, ".vtt")+".srt", []byte("1\n00:00:01,000 --> 00:00:02,000\nHallo\n"), 0o644)
			_ = os.Remove(vtt)
			fmt.Println("Deleting original file " + vtt + " (pass -k to keep)")
		}
	case "thumbnail":
		// Fetched as webp, then converted to the jpg the row asked for.
		webp := filepath.Join(dir, "A Video.webp")
		_ = os.WriteFile(webp, []byte("webp"), 0o644)
		fmt.Println("[info] Writing video thumbnail 41 to: " + webp)
		_ = os.WriteFile(filepath.Join(dir, "A Video.jpg"), []byte("jpeg bytes"), 0o644)
		_ = os.Remove(webp)
		fmt.Printf("[ThumbnailsConvertor] Converting thumbnail \"%s\" to jpg\n", webp)
	case "description":
		desc := filepath.Join(dir, "A Video.description")
		_ = os.WriteFile(desc, []byte("Line one.\nLine two.\n"), 0o644)
		fmt.Println("[info] Writing video description to: " + desc)
	case "partial":
		// Part way through the video stream of a merged download, fetched in
		// fragments, and staying there until it is killed.
		info := filepath.Join(dir, "A Video.info.json")
		stream := filepath.Join(dir, "A Video.f137.mp4")
		_ = os.WriteFile(info, []byte(helperInfoJSON), 0o644)
		fmt.Println("[info] Writing video metadata as JSON to: " + info)
		fmt.Println("[download] Destination: " + stream)
		for _, suffix := range []string{".part", ".ytdl", ".part-Frag3"} {
			_ = os.WriteFile(stream+suffix, []byte("partial"), 0o644)
		}
		fmt.Println("KLP:" + `{"downloaded_bytes":5,"total_bytes":50,"speed":1.0,"filename":"` + jsonPath(stream) + `"}`)
		time.Sleep(time.Minute)
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
	case "livelong":
		// A recording that is still going when the test looks, and ends by
		// itself a moment later.
		for i := 1; i <= 3; i++ {
			fmt.Printf("KLP:{\"live\":\"True\",\"p\":{\"downloaded_bytes\":%d,\"speed\":1000.0}}\n", i*(256<<10))
		}
		time.Sleep(time.Second)
	case "recording":
		// A live stream handed to ffmpeg, which shares yt-dlp's output.
		ffmpeg := exec.Command(os.Args[0], recordArg, final+".part")
		ffmpeg.Stdout = os.Stdout
		if err := ffmpeg.Start(); err != nil {
			os.Exit(1)
		}
		fmt.Println("[download] Destination: " + final)
		fmt.Println("KLP:" + `{"live":"True","p":{"downloaded_bytes":5,"speed":1.0,"filename":"` + jsonPath(final) + `"}}`)
		time.Sleep(time.Minute)
	case "hang":
		// Part way through, and staying there until it is killed.
		fmt.Println("KLP:" + `{"downloaded_bytes":5,"total_bytes":50,"speed":1.0,"filename":"` + jsonPath(final) + `"}`)
		time.Sleep(time.Minute)
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

// recordArg makes the helper play ffmpeg recording a stream into the path
// after it.
const recordArg = "-record-into"

// ffmpegRecorder appends to the recording until it is killed. It opens the
// file for every write, so a recording deleted under it comes back.
func ffmpegRecorder(path string) {
	for end := time.Now().Add(time.Minute); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString("frame")
			_ = f.Close()
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

// A removal with its files deletes the file the task recorded, and only while
// it is still the size the task recorded.
func TestRunReportsTheMergedFileAndItsSize(t *testing.T) {
	dir, rec := runFake(t, "merge:full", Options{})
	got := rec.last()
	if got.Status != core.StatusDone {
		t.Fatalf("last update = %+v, want Done", got)
	}
	final := filepath.Join(dir, "A Video.mkv")
	if got.File != final {
		t.Errorf("File = %q, want the merged file %q", got.File, final)
	}
	if got.Size != int64(len("merged bytes")) || got.Loaded != got.Size {
		t.Errorf("Size = %d, Loaded = %d, want both to be the merged file's %d bytes", got.Size, got.Loaded, len("merged bytes"))
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

// Halt stops yt-dlp without a word to the app and returns once it has exited,
// so its files can be moved. Resume then starts it in the folder Dir names by
// that time.
func TestHaltStopsYtdlpQuietlyAndResumeGoesOnInTheNewFolder(t *testing.T) {
	t.Setenv(runHelperEnv, "hang:full")
	first, second := t.TempDir(), t.TempDir()
	var mu sync.Mutex
	dir := first
	rec := &recorder{}
	b := NewBackend(os.Args[0], first, rec.add)
	b.FFprobe = os.Args[0]
	b.Options = func(string) Options { return Options{} }
	b.Dir = func(string) string {
		mu.Lock()
		defer mu.Unlock()
		return dir
	}
	b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
	deadline := time.Now().Add(30 * time.Second)
	for rec.last().Loaded == 0 {
		if time.Now().After(deadline) {
			t.Fatal("yt-dlp never reported progress")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !b.Halt("task-1") {
		t.Fatal("Halt found no yt-dlp running")
	}
	b.mu.Lock()
	_, running := b.runs["task-1"]
	b.mu.Unlock()
	if running {
		t.Error("Halt returned while the run was still going")
	}
	for _, u := range rec.all() {
		if u.Status == core.StatusPaused || u.Status == core.StatusError {
			t.Errorf("the app was told %q: %s", u.Status, u.Err)
		}
	}
	if b.Halt("task-1") {
		t.Error("a second Halt found a yt-dlp to stop")
	}

	mu.Lock()
	dir = second
	mu.Unlock()
	t.Setenv(runHelperEnv, "video:full")
	b.Resume("task-1")
	deadline = time.Now().Add(30 * time.Second)
	for rec.last().Status != core.StatusDone {
		if time.Now().After(deadline) {
			t.Fatalf("the resumed run never finished; last update %+v", rec.last())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(second, "A Video.mkv")); err != nil {
		t.Errorf("the resumed run did not write into the folder Dir names after the halt: %v", err)
	}
}

// Recording tells a live stream being recorded from a download, since a halt
// ends a recording instead of pausing it.
func TestRecordingTellsALiveStreamFromADownload(t *testing.T) {
	for _, tc := range []struct {
		mode string
		live bool
	}{{"livelong:full", true}, {"hang:full", false}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv(runHelperEnv, tc.mode)
			rec := &recorder{}
			b := NewBackend(os.Args[0], t.TempDir(), rec.add)
			b.FFprobe = os.Args[0]
			b.Options = func(string) Options { return Options{Live: Live{Enabled: tc.live}} }
			b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
			defer b.Remove("task-1", false)
			deadline := time.Now().Add(30 * time.Second)
			for rec.last().Loaded == 0 {
				if time.Now().After(deadline) {
					t.Fatal("yt-dlp never reported progress")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if got := b.Recording("task-1"); got != tc.live {
				t.Errorf("Recording = %v, want %v", got, tc.live)
			}
			if !tc.live {
				return
			}
			for rec.last().Status != core.StatusDone {
				if time.Now().After(deadline) {
					t.Fatal("the recording never ended")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if b.Recording("task-1") {
				t.Error("Recording is still true once the recording has ended")
			}
		})
	}
}

// The thumbnail, subtitle and description rows announce their file with
// yt-dlp's "Writing" lines rather than a Destination, and a removal with files
// deletes the file the task recorded.
func TestRunReportsTheFileOfASidecarRow(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		variant Variant
		file    string
	}{
		{"thumbnail:full", VariantThumbnail, "A Video.jpg"},
		{"description:full", VariantDescription, "A Video.description"},
		{"subs:full", VariantSubtitle, "A Video.de.srt"},
	} {
		t.Run(string(tc.variant), func(t *testing.T) {
			dir, rec := runFake(t, tc.mode, Options{Variant: tc.variant, SubtitleLangs: "de", Embed: Embed{NFO: true}})
			got := rec.last()
			if got.Status != core.StatusDone {
				t.Fatalf("last update = %+v, want Done", got)
			}
			want := filepath.Join(dir, tc.file)
			fi, err := os.Stat(want)
			if err != nil {
				t.Fatal(err)
			}
			if got.File != want || got.Size != fi.Size() {
				t.Errorf("File = %q, Size = %d, want %q of %d bytes", got.File, got.Size, want, fi.Size())
			}
			if _, err := os.Stat(nfoPath(want)); err == nil {
				t.Error("a sidecar row wrote an NFO of its own")
			}
		})
	}
}

// yt-dlp names the file after the title with the characters a file name cannot
// hold replaced, and the media rows take that name from the progress lines.
// The thumbnail, subtitle and description rows have none and take it from the
// file once they finish, without the extension the row shows for its kind.
func TestASidecarRowIsNamedAfterTheFileItWrote(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		variant Variant
		name    string
	}{
		{"thumbnail:full", VariantThumbnail, "A Video"},
		{"description:full", VariantDescription, "A Video"},
		{"subs:full", VariantSubtitle, "A Video.de"},
	} {
		t.Run(string(tc.variant), func(t *testing.T) {
			_, rec := runFake(t, tc.mode, Options{Variant: tc.variant, SubtitleLangs: "de"})
			if got := rec.last(); got.Status != core.StatusDone || got.Name != tc.name {
				t.Errorf("last update = %+v, want Done named %q", got, tc.name)
			}
		})
	}
}

// A source with WebVTT subtitles only, as many sites offer, still gives the
// row the .srt it shows, and the row reports that file rather than the .vtt
// the conversion deleted, so a removal with files finds it.
func TestASubtitleRowFromAWebVTTSourceReportsTheSrtItWasConvertedTo(t *testing.T) {
	dir, rec := runFake(t, "vttsubs:full", Options{Variant: VariantSubtitle, SubtitleLangs: "de"})
	got := rec.last()
	want := filepath.Join(dir, "A Video.de.srt")
	if got.Status != core.StatusDone || got.File != want || got.Name != "A Video.de" {
		t.Errorf("last update = %+v, want Done with File %q named %q", got, want, "A Video.de")
	}
}

// A subtitle row of several languages writes a file for each, and the row
// carries all of them, so a removal with files finds every one.
func TestASubtitleRowOfSeveralLanguagesReportsEveryFileItWrote(t *testing.T) {
	dir, rec := runFake(t, "vttsubs2:full", Options{Variant: VariantSubtitle, SubtitleLangs: "en,de"})
	got := rec.last()
	en, de := filepath.Join(dir, "A Video.en.srt"), filepath.Join(dir, "A Video.de.srt")
	if got.Status != core.StatusDone || got.File != de || !slices.Equal(got.OtherFiles, []string{en}) {
		t.Errorf("last update = %+v, want Done with File %q and %q beside it", got, de, en)
	}
	for _, f := range []string{en, de} {
		if _, err := os.Stat(f); err != nil {
			t.Error(err)
		}
	}
}

// runSubtitleBeside runs a subtitle row for de into a folder that already
// holds "A Video.de.srt" from another source, under policy.
func runSubtitleBeside(t *testing.T, policy collide.Policy) (string, *recorder) {
	t.Helper()
	t.Setenv(runHelperEnv, "subs:full")
	dir := t.TempDir()
	theirs := filepath.Join(dir, "A Video.de.srt")
	if err := os.WriteFile(theirs, []byte("another source"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.Options = func(string) Options { return Options{Variant: VariantSubtitle, SubtitleLangs: "de"} }
	b.Placing = func(string) workdir.Options { return workdir.Options{Policy: policy} }
	b.run("task-1", "https://example.invalid/watch?v=x")
	return theirs, rec
}

// folderHolds fails the test unless dir holds exactly names.
func folderHolds(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	slices.Sort(names)
	if !slices.Equal(got, names) {
		t.Errorf("the folder holds %q, want %q", got, names)
	}
}

func TestASubtitleRowIsRenamedAroundASubtitleFileFromAnotherSource(t *testing.T) {
	theirs, rec := runSubtitleBeside(t, collide.Rename)
	dir := filepath.Dir(theirs)
	want := filepath.Join(dir, "A Video.de (2).srt")
	if got := rec.last(); got.Status != core.StatusDone || got.File != want {
		t.Errorf("last update = %+v, want Done with File %q", got, want)
	}
	if b, _ := os.ReadFile(theirs); string(b) != "another source" {
		t.Errorf("the other source's subtitle file now holds %q", b)
	}
	folderHolds(t, dir, "A Video.de.srt", "A Video.de (2).srt")
}

func TestASubtitleRowUnderSkipLeavesASubtitleFileFromAnotherSource(t *testing.T) {
	theirs, rec := runSubtitleBeside(t, collide.Skip)
	got := rec.last()
	if got.Status != core.StatusError || got.Code != core.CodeFileExists || got.File != "" {
		t.Errorf("last update = %+v, want an error with code %q and no file", got, core.CodeFileExists)
	}
	if b, _ := os.ReadFile(theirs); string(b) != "another source" {
		t.Errorf("the other source's subtitle file now holds %q", b)
	}
	folderHolds(t, filepath.Dir(theirs), "A Video.de.srt")
}

func TestASubtitleRowUnderOverwriteReplacesTheFileOfTheSameName(t *testing.T) {
	theirs, rec := runSubtitleBeside(t, collide.Overwrite)
	if got := rec.last(); got.Status != core.StatusDone || got.File != theirs {
		t.Errorf("last update = %+v, want Done with File %q", got, theirs)
	}
	if b, _ := os.ReadFile(theirs); string(b) == "another source" {
		t.Error("the subtitle file was not replaced")
	}
	folderHolds(t, filepath.Dir(theirs), "A Video.de.srt")
}

// Nothing of an unfinished download is the app's to delete: the stream files,
// their .part, .ytdl and fragment files and the info json are yt-dlp's, and
// only the backend saw their names.
func TestRemovingWithFilesTakesWhatAnUnfinishedDownloadWrote(t *testing.T) {
	for _, paused := range []bool{false, true} {
		name := "running"
		if paused {
			name = "paused"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(runHelperEnv, "partial:full")
			dir := t.TempDir()
			theirs := filepath.Join(dir, "Another Video.f137.mp4.part")
			if err := os.WriteFile(theirs, []byte("someone else's"), 0o644); err != nil {
				t.Fatal(err)
			}
			rec := &recorder{}
			b := NewBackend(os.Args[0], dir, rec.add)
			b.Options = func(string) Options { return Options{} }
			b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
			deadline := time.Now().Add(30 * time.Second)
			for rec.last().Loaded == 0 {
				if time.Now().After(deadline) {
					t.Fatal("yt-dlp never reported progress")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if paused {
				b.Pause("task-1")
				for {
					b.mu.Lock()
					_, running := b.runs["task-1"]
					b.mu.Unlock()
					if !running {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("the paused yt-dlp never exited")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}

			b.Remove("task-1", true)

			stream := filepath.Join(dir, "A Video.f137.mp4")
			for _, f := range []string{stream + ".part", stream + ".ytdl", stream + ".part-Frag3", filepath.Join(dir, "A Video.info.json")} {
				if _, err := os.Stat(f); err == nil {
					t.Errorf("%s survived a removal with files", filepath.Base(f))
				}
			}
			if _, err := os.Stat(theirs); err != nil {
				t.Errorf("a file the task never wrote was deleted: %v", err)
			}
		})
	}
}

// The app takes a task off a backend that gave up on its link from inside the
// failure update, and a removal with files waits for the run to end.
func TestRemovingWithFilesFromTheFailureUpdateDoesNotWaitForItself(t *testing.T) {
	t.Setenv(runHelperEnv, "botcheck:full")
	removed := make(chan struct{})
	var b *Backend
	b = NewBackend(os.Args[0], t.TempDir(), func(id string, u core.Update) {
		if u.Status == core.StatusError {
			b.Remove(id, true)
			close(removed)
		}
	})
	b.Options = func(string) Options { return Options{} }
	b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
	select {
	case <-removed:
	case <-time.After(30 * time.Second):
		t.Fatal("Remove called from the failure update never returned")
	}
}

// yt-dlp records a live stream through ffmpeg, which shares its output. A
// removal with files ends ffmpeg too, whether live mode is on or not, and does
// not wait for it: a 24/7 stream never ends by itself, and an ffmpeg left
// running writes into the recording that was just deleted.
func TestRemovingARecordingWithFilesEndsItsFFmpegAtOnce(t *testing.T) {
	for _, live := range []bool{true, false} {
		name := "live mode off"
		if live {
			name = "live mode on"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(runHelperEnv, "recording:full")
			dir := t.TempDir()
			rec := &recorder{}
			b := NewBackend(os.Args[0], dir, rec.add)
			b.Options = func(string) Options { return Options{Live: Live{Enabled: live}} }
			b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
			part := filepath.Join(dir, "A Video.mkv.part")
			deadline := time.Now().Add(30 * time.Second)
			for {
				if _, err := os.Stat(part); err == nil && rec.last().Loaded > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the recording never started")
				}
				time.Sleep(10 * time.Millisecond)
			}

			removed := make(chan struct{})
			go func() {
				b.Remove("task-1", true)
				close(removed)
			}()
			select {
			case <-removed:
			case <-time.After(10 * time.Second):
				t.Fatal("the removal waited for ffmpeg")
			}
			time.Sleep(200 * time.Millisecond)
			if _, err := os.Stat(part); err == nil {
				t.Error("ffmpeg went on recording into the deleted file")
			}
		})
	}
}

// A removal without files can be undone, and the row that comes back can then
// be removed with its files.
func TestRemovingWithFilesAfterAnUndoneRemovalStillTakesThePart(t *testing.T) {
	t.Setenv(runHelperEnv, "partial:full")
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.Options = func(string) Options { return Options{} }
	b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
	deadline := time.Now().Add(30 * time.Second)
	for rec.last().Loaded == 0 {
		if time.Now().After(deadline) {
			t.Fatal("yt-dlp never reported progress")
		}
		time.Sleep(10 * time.Millisecond)
	}

	b.Remove("task-1", false)
	for {
		b.mu.Lock()
		_, running := b.runs["task-1"]
		b.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the removed yt-dlp never exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
	part := filepath.Join(dir, "A Video.f137.mp4.part")
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("a removal without files deleted the .part: %v", err)
	}

	b.Remove("task-1", true)
	if _, err := os.Stat(part); err == nil {
		t.Error("the .part survived the removal with files")
	}
}

// startPartial starts the helper's unfinished download as task-1 and returns
// once it has reported progress.
func startPartial(t *testing.T, b *Backend, rec *recorder) {
	t.Helper()
	b.Options = func(string) Options { return Options{} }
	b.Download("task-1", "https://example.invalid/watch?v=x", nil, 0)
	deadline := time.Now().Add(30 * time.Second)
	for rec.last().Loaded == 0 {
		if time.Now().After(deadline) {
			t.Fatal("yt-dlp never reported progress")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The backend's list of what a download wrote is gone after a restart, so the
// task is told of each file as yt-dlp names it.
func TestEveryFileAnUnfinishedDownloadWritesGoesToItsTask(t *testing.T) {
	t.Setenv(runHelperEnv, "partial:full")
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	startPartial(t, b, rec)
	t.Cleanup(func() { b.Remove("task-1", true) })

	var told []string
	for _, u := range rec.all() {
		if u.WorkFile != "" {
			told = append(told, u.WorkFile)
		}
	}
	want := []string{filepath.Join(dir, "A Video.info.json"), filepath.Join(dir, "A Video.f137.mp4")}
	if !slices.Equal(told, want) {
		t.Errorf("the task was told of %q, want %q", told, want)
	}
}

// The video and the audio row of a link write the same info json, and a
// removal with files leaves it while the other row still has it.
func TestRemovingWithFilesLeavesWhatAnotherTaskStillHas(t *testing.T) {
	t.Setenv(runHelperEnv, "partial:full")
	dir := t.TempDir()
	info := filepath.Join(dir, "A Video.info.json")
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.InUse = func(taskID, path string) bool { return taskID == "task-1" && path == info }
	startPartial(t, b, rec)

	b.Remove("task-1", true)
	if _, err := os.Stat(info); err != nil {
		t.Errorf("the info json another task still has was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "A Video.f137.mp4.part")); err == nil {
		t.Error("the .part survived a removal with files")
	}
}
