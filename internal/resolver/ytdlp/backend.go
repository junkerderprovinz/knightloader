// Package ytdlp is the media-extraction backend: it delegates the download to
// the yt-dlp binary (which handles ~1800 sites incl. HLS/DASH muxing) and
// mirrors yt-dlp's progress into KnightLoader tasks.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

type Backend struct {
	bin string
	dir string

	onUpdate func(taskID string, u core.Update)

	// RateLimit, when set, returns the current download limit in bytes/s
	// (0 = unlimited); applied per spawn via --limit-rate.
	RateLimit func() int64

	// Dir returns the destination for a task; nil or an empty result falls back
	// to the backend's default directory.
	Dir func(taskID string) string

	// Options, when set, returns the yt-dlp configuration for ONE task -
	// which variant it is, format/quality, subtitles, the output template,
	// playlist handling - the same per-task shape Dir already uses just
	// above, and for the identical reason: five sibling tasks sharing one
	// source URL (jdp, 2026-08-25's "Variante" rows) each need their OWN
	// answer, not one instance-wide value applied to all of them. nil, or
	// a nil return for one taskID, reads exactly like Options{}, the zero
	// value that reproduces this backend's pre-Options behaviour, matching
	// RateLimit's own "nil means no opinion" contract.
	Options func(taskID string) Options

	// Cookies, when set, answers the stored cookies.txt text for a URL's
	// host, or "" when nothing is stored for it - CookieStore.Text
	// (cookies.go) is the implementation this expects, and that file's
	// comment is where the handling rules live.
	//
	// A hook rather than a field on Options, and that is a security
	// decision, not a style one: Options is a settings struct. It is
	// marshalled into settings.json, returned by GET /api/settings, and
	// serialised whole into the diagnostics bundle a person attaches to a
	// public bug report. A live session must not travel in any of those, so
	// it never enters the type that goes to all three - it is fetched here,
	// at the moment of the spawn, written to a 0600 file and deleted again.
	//
	// Consulted only when this task's own Options.Cookies is on, so a
	// wired-up store still sends nothing until somebody asks for it.
	Cookies func(rawurl string) string

	// FFprobe is the binary Options.Measure reads a finished file with.
	// Empty means "ffprobe" on PATH, which is what the container image has -
	// the Dockerfile installs ffmpeg for yt-dlp's own muxing and ffprobe
	// ships in that same package. Settable so a test can point it at
	// something that is not a real ffprobe.
	FFprobe string

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	url    map[string]string // for resume
}

// concurrentFragments is how many fragments of one video yt-dlp fetches at
// once, and httpChunkSize how large a single range request is. Four is the
// number yt-dlp's own documentation uses in its examples; ten mebibytes is the
// value its throttling advice quotes. Neither is a knob on the settings page
// on purpose: both are properties of how a fragmented download behaves, not
// preferences, and a person who wants a slower download already has the speed
// limit for that.
const (
	concurrentFragments = 4
	httpChunkSize       = 10 << 20
)

func NewBackend(bin, dir string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		bin: bin, dir: dir, onUpdate: onUpdate,
		cancel: map[string]context.CancelFunc{},
		url:    map[string]string{},
	}
}

// availableTimeout bounds the one spawn Available makes.
//
// It is not a tuning number, it is a bound on a specific failure. A binary that
// is half-written, quarantined mid-scan by a virus scanner, or sitting on a
// network mount that has gone away does not fail to start - it HANGS. Available
// had no context at all until 2026-09-08, and it is called from
// app.rewireBackends, which runs on every account save and on every sweep tick
// via refreshHostListsIfDue. One hanging yt-dlp therefore wedged the account
// routes and the upkeep goroutine together, with nothing in the logs to say
// which of the two dozen things rewireBackends does was the one stuck.
//
// Ten seconds is far longer than `--version` needs even from a cold PyInstaller
// bundle unpacking itself to a temp directory, and far shorter than "for ever".
//
// A var rather than a const only so the test can shrink it, the same reason
// internal/provision's stopGrace is one: a test that proves the ceiling works
// must otherwise sit out the whole ceiling to do it.
var availableTimeout = 10 * time.Second

// Available reports whether the yt-dlp binary runs.
func (b *Backend) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), availableTimeout)
	defer cancel()
	return exec.CommandContext(ctx, b.bin, "--version").Run() == nil
}

func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.url[taskID] = url
	b.mu.Unlock()
	go b.run(taskID, url)
}

func (b *Backend) run(taskID, url string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel[taskID] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.cancel, taskID)
		b.mu.Unlock()
	}()

	dir := b.dir
	if b.Dir != nil {
		if d := b.Dir(taskID); d != "" {
			dir = d
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()})
		return
	}
	var opts Options
	if b.Options != nil {
		opts = b.Options(taskID)
	}
	opts = opts.Sanitize()
	args := buildArgs(dir, opts)
	// The cookie file exists for exactly as long as yt-dlp does. Written
	// before the spawn because --cookies is read at start-up, removed by the
	// deferred cleanup on every exit path from here on, including the ones
	// that return before Wait - a live session left lying in the temp
	// directory because a download failed early is the one outcome this whole
	// feature must not have.
	if opts.Cookies && b.Cookies != nil {
		if text := b.Cookies(url); text != "" {
			path, cleanup, err := writeCookieFile("", text)
			defer cleanup()
			if err != nil {
				// The message carries the failure and nothing about the jar -
				// see cookies.go's file comment on where an Err string ends
				// up. Fatal rather than carrying on without the cookies:
				// this task was configured to arrive logged in, and running
				// it anonymously is how an account gets a site's rate limiter
				// pointed at the address instead.
				b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()})
				return
			}
			args = append(args, "--cookies", path)
		}
	}
	if b.RateLimit != nil {
		if lim := b.RateLimit(); lim > 0 {
			// --limit-rate is per fragment connection, and buildArgs now asks
			// for concurrentFragments of them (see its own comment on why).
			// Passing the whole limit here would therefore let a throttled
			// download run at concurrentFragments TIMES the speed the user
			// set - a speed limit that silently is not one, on the one code
			// path a nightly window exists to enforce. Divided, with a floor
			// so a very low limit cannot round down to zero, which yt-dlp
			// reads as "no limit at all".
			per := lim / concurrentFragments
			if per < 1 {
				per = 1
			}
			args = append(args, "--limit-rate", fmt.Sprint(per))
		}
	}
	cmd := exec.CommandContext(ctx, b.bin, append(args, url)...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	if opts.Live.Enabled {
		// A live recording is the one download where HOW the process is
		// stopped decides whether the file plays. exec.CommandContext kills
		// outright by default, and a killed yt-dlp leaves the fragments it
		// was writing unmerged and unfinalised - a recording somebody set a
		// sixty-minute limit on would end as an unplayable .part, which is
		// the limit destroying exactly what it was set to preserve. An
		// interrupt is what yt-dlp handles: it stops fetching, finishes the
		// fragment in hand and runs its post-processors.
		//
		// os.Interrupt is not implemented on Windows, where Signal answers an
		// error; WaitDelay is what covers that, and any yt-dlp that ignores
		// the interrupt on either platform - after it, the process is killed
		// the old way. The container this ships in is Linux, so the graceful
		// path is the one that runs in production and the fallback is for the
		// desktop build.
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = liveStopGrace
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: err.Error()})
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		b.onUpdate(taskID, core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning})

	// What the run leaves behind for the finish below to act on: the file
	// yt-dlp says it actually produced, how many subtitle files it wrote, and
	// whether a live limit ended the recording rather than the stream doing
	// so on its own.
	var (
		final     string
		subFiles  int
		guard     = newLiveGuard(opts.Live)
		stoppedBy string
	)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // progress JSON lines can be long
	for sc.Scan() {
		line := sc.Text()
		if p, ok := parseProgress(line); ok {
			u := core.Update{Status: core.StatusRunning, Loaded: p.Downloaded, Size: p.Total, Speed: p.Speed}
			if p.Filename != "" {
				u.Name = filepath.Base(p.Filename)
			}
			if opts.Live.Enabled && p.Live {
				guard.begin(time.Now())
				// A recording has no total, so the bar behind it is drawn
				// from nothing. The note is what the row shows instead:
				// how long this has been running and how much has landed,
				// both of which are facts (see liveGuard.note).
				u.Note = guard.note(p.Downloaded)
				if stoppedBy == "" {
					if reason := guard.exceeded(p.Downloaded); reason != "" {
						stoppedBy = reason
						// Stops the process the graceful way set up above.
						// The loop keeps reading until the pipe closes, so
						// yt-dlp's own post-processing still reports the file
						// it finalised.
						cancel()
					}
				}
			}
			b.onUpdate(taskID, u)
			continue
		}
		if name, ok := finishedFile(line); ok {
			final = name
			b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: filepath.Base(name)})
			continue
		}
		if wroteSubtitle(line) {
			subFiles++
		}
	}
	scanErr := sc.Err()
	err = cmd.Wait()
	if ctx.Err() != nil && stoppedBy == "" {
		return // cancelled by Pause/Remove
	}
	if stoppedBy != "" {
		// A recording that hit its own limit is finished, not failed: the
		// bytes up to the cap are the file that was asked for, and the reason
		// it ends where it does is on the task rather than left to be guessed
		// at from the runtime.
		b.onUpdate(taskID, b.finish(opts, final, subFiles, stoppedBy))
		return
	}
	if err == nil && scanErr != nil {
		err = scanErr
	}
	if err != nil {
		raw := stderr.String()
		msg := errorLine(raw)
		if msg == "" {
			msg = err.Error()
		}
		b.onUpdate(taskID, core.Update{
			Status: core.StatusError,
			Err:    "yt-dlp: " + msg,
			// Read from the WHOLE buffer and before anything is cut down to
			// one line, which is the point of doing it here at all: the words
			// that tell a bot check from a dead link from a region block are
			// the ones a truncated sentence loses first. See diagnose.go.
			Reason: Diagnose(raw),
			// yt-dlp saying it has no extractor for this link is not a download
			// failure - it means the link belongs to someone else. Saying so
			// lets a plain file whose URL carries no extension still be fetched.
			Unsupported: notMine(msg),
		})
		return
	}
	b.onUpdate(taskID, b.finish(opts, final, subFiles, ""))
}

// liveStopGrace is how long a live recording gets to shut itself down after
// the interrupt before the process is killed the old way. Long enough for
// yt-dlp to close the fragment it is on and let ffmpeg finalise the container
// it has been writing (both are local work on a file already on disk); short
// enough that a yt-dlp which ignored the signal entirely does not hold a
// finished task open for minutes.
const liveStopGrace = 30 * time.Second

// finish is everything that happens after yt-dlp has exited successfully, and
// it is one function rather than three because all of it acts on the SAME
// answer: the path yt-dlp said it produced. Nothing here can turn a
// successful download into a failure except the two cases that are genuinely
// failures dressed as successes - a subtitle row that wrote no subtitle, and
// (when asked for) a file measurably shorter than the source announced.
//
// stoppedBy, when set, is a live limit's own sentence; it survives whatever
// else this adds, because "why does this recording end here" is the first
// question the row has to answer.
//
// It deliberately does NOT take run()'s own context. That context is already
// cancelled on the one path that reaches here with stoppedBy set - the live
// guard cancels it to stop the recording - and handing it to ffprobe would
// mean the measurement silently never ran for exactly the downloads a cap was
// put on. Everything here is local work on a file already written, and the
// task has already survived; a pause or a remove returns above this instead.
func (b *Backend) finish(o Options, final string, subFiles int, stoppedBy string) core.Update {
	u := core.Update{Status: core.StatusDone, Speed: 0, Note: stoppedBy}
	// The silent-empty-subtitle-task check, and it is deliberately not
	// conditional on which languages were asked for: --no-warnings means the
	// one process that knew the language was missing said so on a channel
	// this backend mutes, so counting what it WROTE is the only evidence
	// left here (see Options.SubtitleStrict).
	if o.SubtitleStrict && o.Variant == VariantSubtitle && subFiles == 0 {
		langs := o.SubtitleLangs
		if langs == "" {
			langs = DefaultSubtitleLangs
		}
		u.Status = core.StatusError
		u.Err = "yt-dlp: no subtitles were written for " + langs
		return u
	}
	if final == "" {
		// Nothing named a finished file, so there is nothing to write an NFO
		// beside or measure. That is the normal case for the thumbnail,
		// subtitle and description rows, which produce files yt-dlp announces
		// with different lines entirely, and it must stay silent rather than
		// become a warning on three rows out of five.
		return u
	}
	info, haveInfo := infoDict{}, false
	if needsInfoJSON(o) {
		path := infoJSONPath(final)
		info, haveInfo = readInfoJSON(path)
		if o.Embed.NFO && haveInfo {
			// A failed write is not worth failing the download over - the
			// media file is complete and correct, and the sidecar can be
			// produced again by re-running the task.
			_ = writeNFO(nfoPath(final), info)
		}
		// Removed either way: this backend asked for the json as scaffolding
		// for the two features above, and leaving one .info.json per download
		// behind would be a filesystem change nobody switched on.
		_ = os.Remove(path)
	}
	if o.Measure.Enabled {
		mctx, cancel := context.WithTimeout(context.Background(), measureTimeout)
		defer cancel()
		bin := b.FFprobe
		if bin == "" {
			bin = "ffprobe"
		}
		if m, err := probeMedia(mctx, bin, final); err == nil {
			u.Note = joinNote(u.Note, m.Summary())
			if haveInfo {
				if pct, ok := gotPercent(info.Duration, m.Duration); ok && pct < o.Measure.ShortPercent {
					warn := shortWarning(info.Duration, m.Duration)
					if o.Measure.FailOnShort {
						u.Status = core.StatusError
						u.Err = "yt-dlp: " + warn
						return u
					}
					// Loud, but still done: the bytes are real and often
					// still worth having, and a person who would rather the
					// row went red has Measure.FailOnShort for that.
					u.Note = joinNote(warn, u.Note)
				}
			}
		}
		// A failed ffprobe says nothing at all. It means this build has no
		// ffprobe, or the file is on a mount that went away - neither is a
		// statement about whether the download worked, and putting "could not
		// measure" on every finished task in an install without ffmpeg would
		// be a permanent false alarm.
	}
	return u
}

// joinNote puts two sentences on one line, dropping whichever is empty. The
// note column is narrow (see columns.tsx), so the order matters: a warning
// goes first because a truncated line has to still carry the warning.
func joinNote(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + ", " + second
}

// needsInfoJSON reports whether this task has to ask yt-dlp for
// --write-info-json. Two features read it: the NFO is built from it, and the
// short-file check needs the duration the SOURCE announced, which exists
// nowhere else by the time a download has finished.
func needsInfoJSON(o Options) bool { return o.Embed.NFO || o.Measure.Enabled }

// infoJSONPath is where yt-dlp put the info json for a finished file.
//
// yt-dlp builds it from the same output template with the extension replaced,
// so "…/Some Title.mkv" is written beside "…/Some Title.info.json" - which is
// why this replaces the extension rather than appending to the name.
func infoJSONPath(final string) string {
	return strings.TrimSuffix(final, filepath.Ext(final)) + ".info.json"
}

// nfoPath is the sidecar's own name, by the same rule: Jellyfin, Kodi and
// Plex all look for "<the media file's name>.nfo" next to the file.
func nfoPath(final string) string {
	return strings.TrimSuffix(final, filepath.Ext(final)) + ".nfo"
}

// finishedFile picks the path of the file yt-dlp actually produced out of its
// own stdout chatter, and the caller keeps the LAST one: these three lines
// arrive in pipeline order, so a merged video's "[download] Destination:"
// lines for the two half-streams are followed by the Merger's line naming the
// file that survives them, and an audio extraction's by the ExtractAudio one.
//
// Read off stdout rather than asked for with --print, and that is not a
// preference: --print implies --quiet, which would take the progress lines
// this backend's entire download display is built on with it.
func finishedFile(line string) (string, bool) {
	if i := strings.Index(line, "[Merger] Merging formats into \""); i >= 0 {
		rest := line[i+len("[Merger] Merging formats into \""):]
		if j := strings.LastIndex(rest, "\""); j > 0 {
			return rest[:j], true
		}
		return "", false
	}
	for _, marker := range []string{"[download] Destination:", "[ExtractAudio] Destination:"} {
		if i := strings.Index(line, marker); i >= 0 {
			name := strings.TrimSpace(line[i+len(marker):])
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

// wroteSubtitle recognises yt-dlp announcing a subtitle file it has written -
// the evidence Options.SubtitleStrict counts. It matches on the tail of the
// sentence rather than the whole of it because yt-dlp says "Writing video
// subtitles to:" for a manual track and prefixes the same line differently
// for automatic captions, and both are a subtitle file landing on disk.
func wroteSubtitle(line string) bool {
	return strings.Contains(line, "subtitles to:")
}

// FormatEntry is one entry from yt-dlp's own "formats" array (-j/
// --dump-json's info dict), reduced to the fields KnightLoader actually
// reads: whether it is a video or audio track (Vcodec/Acodec is "none" for
// the side that format doesn't carry), the quality/size that track is, and
// what container it lands in. Height is 0 for an audio-only entry - yt-dlp
// itself uses that same "not a video dimension" convention.
type FormatEntry struct {
	FormatID string
	Ext      string
	Vcodec   string
	Acodec   string
	Height   int
	// Filesize is the exact byte count when the host reports one (a plain
	// https download); FilesizeApprox is yt-dlp's own estimate when it does
	// not (an m3u8/DASH manifest, most commonly) - never both at once in
	// practice, callers wanting "whatever number is available" should read
	// Filesize first and fall back to FilesizeApprox.
	Filesize       int64
	FilesizeApprox int64
	// Abr is yt-dlp's own "abr" field - the average audio bitrate in
	// kbit/s this specific track carries, 0 when the source did not report
	// one. Meaningful only for an audio track (Acodec set); a video-only
	// entry's own Abr is always 0 by the same convention Height already
	// follows the other way.
	Abr float64
	// Language is the spoken language of this track's audio, as the source
	// reported it ("en", "de-DE"), and "" when it reported none - which is
	// every site that ships one audio track and never had a second one to
	// distinguish.
	Language string
	// LanguagePreference is yt-dlp's own ranking of that language against
	// the others on the same video: above the default for the track the
	// video was actually recorded in, below it for a machine dub. 0 is the
	// field being absent - see AudioLang.Dubbed (languages.go) for why that
	// reads as "original", not as "unknown".
	LanguagePreference int
}

// ProbeResult is what a single -j extraction pass answers: the resolved
// title (setTaskName's own input) and every format the source actually
// offers (what the "Variante" quality/audio-format pickers narrow down to,
// and what a specific pick's own extension/size come from) - one process,
// one network round trip, both answers already sitting in yt-dlp's own info
// dict by the time extraction finishes; see ProbeTitle's own doc comment
// for why asking for more from the SAME call costs nothing extra.
type ProbeResult struct {
	Title   string
	Formats []FormatEntry
	// Subtitles and AutoCaptions are the language codes the source offers in
	// each kind - the keys of yt-dlp's own "subtitles" and
	// "automatic_captions" maps, unsorted, as they came out of the JSON.
	// AvailableSubtitleLangs (languages.go) is what turns them into the two
	// menus a picker shows, and why they are kept apart.
	Subtitles    []string
	AutoCaptions []string
	// Duration is the runtime the source announced, in seconds, 0 when it
	// announced none. A live stream reports none, which is one of the two
	// ways the short-file check knows there is nothing to compare against.
	Duration float64
	// IsLive is the info dict's own is_live. It has been sitting in this
	// document since this backend existed and was read by nothing: a link to
	// a stream that has been running since Friday arrived as an ordinary
	// task with an ordinary progress bar. See Options.Live.
	IsLive bool
}

// ProbeTitle asks yt-dlp for a link's real title AND its real available
// formats without downloading anything - the per-task ASYNC probe
// Resolver's own doc comment (resolver.go) says is a different shape from
// the batched resolver.Checker deliberately left unbuilt there. -j
// (--dump-json) with --skip-download (no -f, no format selection) asks
// yt-dlp to extract just enough metadata to answer, never the muxed formats
// a real download or a "--simulate" would resolve, so this is cheaper than
// the download it stands in for - though it is still one real process per
// call, including whatever anti-bot gauntlet the site puts in front of
// extraction, which is exactly why app.probeYtdlpTitle (the only caller)
// fires this once per staged task rather than batching a paste's worth of
// links into one call the way a Checker would.
//
// -j rather than the older --print %(title)s (jdp, 2026-08-25: "man soll
// nur die varianten auswählen können die wirklich verfügbar sind" / "es
// zeigt die dateiendungen... und die größe nicht an"): yt-dlp builds its
// complete internal info dict - including every format's own id, container,
// codecs and size - during extraction regardless of what gets printed
// afterward; --print only threw the rest away. Asking for it all via -j
// measured indistinguishable from the old --print call in wall-clock time
// (both are dominated by the site's own extraction round trip, not by what
// gets serialized afterward), so there is no real cost to always having the
// format list on hand instead of a second, separate probe for it.
//
// The caller is expected to bound ctx - see app.ytdlpProbeTimeout, applied
// by probeYtdlpTitle the same way app.checkTimeout already bounds
// resolver.Checker.Check. Not baked in here, so a test can hand this
// whatever timeout (or none) it needs without the constant living in this
// package at all.
//
// Known simplification, not a guess dressed up as a decision: --flat-playlist
// is deliberately NOT passed. It would make a playlist/channel URL's probe
// far cheaper - yt-dlp could answer from the listing alone instead of
// opening entries - but it also changes what the info dict answers for an
// ordinary single-video URL in ways this change could not confirm safely
// from documented behaviour alone, and guessing wrong here means a working
// single-video probe breaks instead of a playlist probe staying merely slow.
// Without the flag, a playlist/channel URL's probe still runs rather than
// failing outright - slower, and -j prints one JSON object per entry rather
// than the playlist's own single answer, of which firstLine below still
// takes the first - a known gap for a later pass, not a crash today.
func (b *Backend) ProbeTitle(ctx context.Context, url string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, b.bin, "--skip-download", "--no-warnings", "-j", url)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	out, err := cmd.Output()
	if err != nil {
		return ProbeResult{}, err
	}
	line := firstLine(string(out))
	if line == "" {
		return ProbeResult{}, errors.New("ytdlp: probe returned no data")
	}
	var raw struct {
		Title   string `json:"title"`
		Formats []struct {
			FormatID           string  `json:"format_id"`
			Ext                string  `json:"ext"`
			Vcodec             string  `json:"vcodec"`
			Acodec             string  `json:"acodec"`
			Height             int     `json:"height"`
			Filesize           int64   `json:"filesize"`
			FilesizeApprox     float64 `json:"filesize_approx"`
			Abr                float64 `json:"abr"`
			Language           string  `json:"language"`
			LanguagePreference int     `json:"language_preference"`
		} `json:"formats"`
		// Both maps are language code -> a list of that language's own
		// downloadable formats. Only the keys are read: WHICH languages are
		// on offer is the whole question, and the per-language format list is
		// yt-dlp's own business once --sub-format has named a target.
		// json.RawMessage rather than a typed value so an extractor with an
		// unexpected shape inside costs the keys nothing.
		Subtitles    map[string]json.RawMessage `json:"subtitles"`
		AutoCaptions map[string]json.RawMessage `json:"automatic_captions"`
		Duration     float64                    `json:"duration"`
		IsLive       bool                       `json:"is_live"`
		WasLive      bool                       `json:"was_live"`
		LiveStatus   string                     `json:"live_status"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return ProbeResult{}, fmt.Errorf("ytdlp: probe returned unparseable data: %w", err)
	}
	title := strings.TrimSpace(raw.Title)
	if title == "" {
		return ProbeResult{}, errors.New("ytdlp: probe returned no title")
	}
	res := ProbeResult{
		Title:    title,
		Formats:  make([]FormatEntry, 0, len(raw.Formats)),
		Duration: raw.Duration,
		// is_live OR live_status, because the two do not always agree: newer
		// extractors set live_status ("is_live", "is_upcoming", "was_live",
		// "not_live") and older ones only the boolean. was_live is
		// deliberately NOT counted - a finished stream is an ordinary
		// recording with an ordinary length, and treating it as live would
		// put the recording caps on a normal download.
		IsLive:       raw.IsLive || raw.LiveStatus == "is_live",
		Subtitles:    keysOf(raw.Subtitles),
		AutoCaptions: keysOf(raw.AutoCaptions),
	}
	for _, f := range raw.Formats {
		res.Formats = append(res.Formats, FormatEntry{
			FormatID: f.FormatID, Ext: f.Ext, Vcodec: f.Vcodec, Acodec: f.Acodec,
			Height: f.Height, Filesize: f.Filesize, FilesizeApprox: int64(f.FilesizeApprox),
			Abr: f.Abr, Language: f.Language, LanguagePreference: f.LanguagePreference,
		})
	}
	return res, nil
}

// keysOf is the language codes out of one of the two subtitle maps, in no
// particular order - AvailableSubtitleLangs sorts them, once, where they are
// turned into a menu.
func keysOf(m map[string]json.RawMessage) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// firstLine is the first non-empty line of s. A single-video probe's -j
// prints exactly one JSON object per line; the --flat-playlist gap
// documented on ProbeTitle above means a playlist/channel URL can print one
// object per entry instead, and the first is the best single answer
// available without a second, playlist-aware flag.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// notMine recognises yt-dlp's way of saying a link is not something it handles.
func notMine(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "unsupported url") ||
		strings.Contains(m, "no suitable extractor") ||
		strings.Contains(m, "is not a valid url")
}

// buildArgs turns Options into the flags run() spawns yt-dlp with, minus the
// binary and the URL - the caller still appends the URL last, exactly as it
// did before Options existed. It is a pure function on purpose: everything
// here is unit-tested without spawning yt-dlp at all, which run()'s own
// process-spawning shape cannot be.
//
// exec.CommandContext never invokes a shell (it execs the binary directly
// with an argv array), so nothing built here is a shell-injection vector
// regardless of what CustomFormat or OutputTemplate contain - that is a
// property of os/exec, not of anything this function checks.
//
// o is sanitized on entry rather than trusted from the caller. In practice
// Backend.Options is always wired to a live settings snapshot that has
// already been through Options.Sanitize (settings.sanitize calls it on
// every load and every save), but this function shells out a real process
// on the strength of what it is handed, so it does not get to assume its
// caller remembered - the zero value Options{} must build the exact same
// argument list Sanitize would produce from it, or the "every field's zero
// value changes nothing" promise on Options's own doc comment is false the
// one time something calls buildArgs directly.
func buildArgs(dir string, o Options) []string {
	o = o.Sanitize()
	args := []string{
		"--newline", "--no-warnings", "--no-color",
		// The progress line carries is_live only when the live guard is on -
		// see live.go on why the format an install without it runs must stay
		// byte for byte the one it has always run.
		"--progress-template", progressTemplateFor(o),
		// Speed (jdp, 2026-09-06: "Youtube lädt super langsam herunter. das
		// geht in jd viel schneller"). Two flags, two different reasons, and
		// neither is tuning for its own sake:
		//
		//   --concurrent-fragments: a DASH or HLS video is thousands of small
		//   fragments, and yt-dlp fetches them ONE AT A TIME by default. Every
		//   fragment pays a fresh round trip, so on a link with any latency at
		//   all the connection sits idle most of the time. JDownloader opens
		//   several connections per file as a matter of course; this is the
		//   same idea with the number yt-dlp's own documentation uses in its
		//   examples.
		//
		//   --http-chunk-size: the documented answer to a server that throttles
		//   a single long-running response ("may be useful for bypassing
		//   bandwidth throttling imposed by a webserver"). YouTube is the
		//   textbook case - one open range for a whole file gets metered down
		//   to a trickle, while the same bytes fetched in chunks do not.
		//
		// Both are safe where they do not apply: a non-fragmented download
		// ignores the first, and a server without range support makes yt-dlp
		// fall back to a single request for the second.
		"--concurrent-fragments", strconv.Itoa(concurrentFragments),
		"--http-chunk-size", strconv.Itoa(httpChunkSize),
	}
	if !o.Playlist {
		args = append(args, "--no-playlist")
	}
	// Branches on the VARIANT this one task is - which of the source's own
	// forms it downloads - rather than folding every possible extra file
	// onto a single video job (jdp, 2026-08-25, after a first attempt at
	// this same request built exactly that instead: "ich glaub du hast
	// nicht verstanden was ich mein... genau so soll es auch in KL sein",
	// pointing at JD's own five independently keepable rows per link -
	// video, audio, thumbnail, subtitles, description - each its own task
	// here now, so each gets its own args and its own Enabled switch
	// rather than a bundle of booleans on one task nobody could turn off
	// individually).
	tmpl := outputTemplate(o)
	switch o.Variant {
	case VariantAudio:
		args = append(args, "-f", audioSelector(o.AudioLang), "-x")
		if o.AudioFormat != "" && o.AudioFormat != "best" {
			args = append(args, "--audio-format", o.AudioFormat)
		}
		// Meaningful only once AudioFormat above actually asks for a
		// transcode - a "best" extract copies the source's own stream and
		// ffmpeg has nothing to re-encode, so the flag is passed unconditionally
		// (yt-dlp itself is the one place that decides whether it applies)
		// rather than this function trying to duplicate that rule.
		if o.AudioBitrate != "" {
			args = append(args, "--audio-quality", o.AudioBitrate+"K")
		}
		args = append(args, embedArgs(o, false)...)
		if o.Music {
			args = append(args, musicArgs(dir, tmpl)...)
		}
	case VariantThumbnail:
		// --skip-download: this task's own job is the cover image alone,
		// not a byproduct of a video download it does not also do.
		// --convert-thumbnails jpg: the source's own image format otherwise
		// varies (webp/jpg/png by site), which is exactly why applyProbeFormats
		// (app_ytdlp_variants.go) leaves this row's own Ext unset - forcing
		// the one universally-supported format here makes Ext="jpg" a real
		// fact about what lands on disk instead of a guess about it.
		args = append(args, "--skip-download", "--write-thumbnail", "--convert-thumbnails", "jpg")
	case VariantSubtitle:
		langs := o.SubtitleLangs
		if langs == "" {
			langs = DefaultSubtitleLangs
		}
		// --sub-format srt: the same reasoning as --convert-thumbnails above
		// - a site's own default (usually vtt) varies, srt is the one every
		// media player reads without a second thought, and forcing it is
		// what lets Ext="srt" be a fact rather than a guess.
		args = append(args, "--skip-download", "--write-subs", "--sub-langs", langs, "--sub-format", "srt")
		if o.SubtitleAuto {
			args = append(args, "--write-auto-subs")
		}
	case VariantDescription:
		args = append(args, "--skip-download", "--write-description")
	default: // VariantVideo
		if f := formatSelector(o); f != "" {
			args = append(args, "-f", f)
		}
		// --merge-output-format mkv: with no opinion of its own, yt-dlp's
		// merge target depends on which two streams formatSelector's own
		// selector actually picked - not knowable ahead of time, which is
		// why applyProbeFormats leaves a video row's own Ext unset. mkv
		// accepts any video/audio codec pairing (unlike mp4, which only
		// merges cleanly for a compatible subset), so forcing it here makes
		// the real container a fact instead of a guess - Ext="mkv" below is
		// this flag's own promise, not a prediction of yt-dlp's default.
		args = append(args, "--merge-output-format", "mkv")
		args = append(args, embedArgs(o, true)...)
	}
	// Both of these belong to a download, not to the three rows that fetch a
	// sidecar. An NFO beside a .srt describes nothing, and --live-from-start
	// on a --skip-download row is a flag about bytes nobody is fetching.
	if o.Variant == VariantVideo || o.Variant == VariantAudio {
		if needsInfoJSON(o) {
			args = append(args, "--write-info-json")
		}
		if o.Live.Enabled && o.Live.FromStart {
			args = append(args, "--live-from-start")
		}
	}
	args = append(args, "-o", filepath.Join(dir, tmpl))
	return args
}

// outputTemplate is the -o template this task runs with.
//
// Music mode supplies its own only when nobody typed one. A person who filled
// in the output template field meant it, and having a switch elsewhere on the
// page silently overrule a field they can see is the kind of surprise that
// gets reported as "the template setting does nothing". The cover image still
// follows whatever template wins (see coverTemplate), so the two stay
// together either way.
func outputTemplate(o Options) string {
	if o.OutputTemplate != "" {
		return o.OutputTemplate
	}
	if o.Music && o.Variant == VariantAudio {
		return musicOutputTemplate
	}
	return defaultOutputTemplate
}

// audioSelector is the audio row's own -f value, with a language filter when
// one was asked for.
//
// The fallback chain is the point. "bestaudio[language^=de]" alone would make
// a source that reports no language per track - which is most sites, since
// per-track language is a thing YouTube's auto-dubbing brought - resolve to
// nothing at all, and yt-dlp answers a selector that matches nothing with a
// failed task. Falling through to the unfiltered selector means asking for
// German gets German where German exists and the ordinary best track where
// the source never had an opinion, instead of an error.
//
// `^=` rather than `=`: a track's language is a BCP 47 tag, so the German
// track on a YouTube video is "de-DE" as often as "de", and an exact match
// would miss exactly half of them.
func audioSelector(lang string) string {
	if lang == "" {
		return "bestaudio/best"
	}
	return "bestaudio[language^=" + lang + "]/bestaudio/best"
}

// embedArgs is the container-level extras for a video (video=true) or an
// extracted audio file. See Embed's own doc comment for what each one buys.
func embedArgs(o Options, video bool) []string {
	var args []string
	// Music mode implies the metadata block, because --parse-metadata below
	// only fills in fields and --embed-metadata is what actually writes them
	// into the file. Without it the whole switch would rename files and tag
	// nothing, which is the failure it exists to fix.
	if o.Embed.Metadata || (o.Music && !video) {
		args = append(args, "--embed-metadata")
	}
	if o.Embed.Thumbnail {
		args = append(args, "--embed-thumbnail")
	}
	if o.Embed.Chapters {
		args = append(args, "--embed-chapters")
	}
	if o.Embed.Subs && video {
		// --embed-subs on its own embeds nothing: yt-dlp has to be told to
		// FETCH the subtitles first, which is what --write-subs does, and
		// which languages, which is --sub-langs. Video only - an extracted
		// audio file has no subtitle stream to put them in.
		langs := o.SubtitleLangs
		if langs == "" {
			langs = DefaultSubtitleLangs
		}
		args = append(args, "--write-subs", "--sub-langs", langs, "--embed-subs")
		if o.SubtitleAuto {
			args = append(args, "--write-auto-subs")
		}
	}
	if o.Embed.SplitChapters {
		args = append(args, "--split-chapters")
	}
	return args
}

// musicOutputTemplate is the naming scheme music mode uses: one folder per
// artist, one per album inside it, tracks numbered so they sort in playing
// order rather than alphabetically.
//
// Every field is an alternates chain rather than a single name, because the
// fields a music library needs are the ones a video site is least reliable
// about: "artist" and "album" come from a real music extractor or from
// YouTube's own music metadata when it has any, and from the uploader and the
// playlist otherwise. A chain that ends in something always present is what
// keeps a track out of a folder literally called "NA".
const musicOutputTemplate = "%(artist,album_artist,creator,uploader)s/" +
	"%(album,playlist_title,title)s/" +
	"%(track_number,playlist_index)s-%(track,title)s.%(ext)s"

// musicArgs is the tagging half of music mode: the same four fields as the
// naming scheme, mapped onto the meta_* fields yt-dlp's own metadata
// post-processor writes into the file, plus a cover image beside the tracks.
//
// The mapping direction reads "FROM:TO" - --parse-metadata takes an output
// template on the left and the field to fill on the right, and the meta_
// prefix is what marks a field as one the embedder should write rather than
// one yt-dlp merely knows. Without these four, an --embed-metadata mp3 gets
// the VIDEO's title and uploader, which is why twelve tracks off one album
// arrive in Navidrome as twelve singles by a channel name.
func musicArgs(dir, tmpl string) []string {
	return []string{
		"--parse-metadata", "%(artist,album_artist,creator,uploader)s:%(meta_artist)s",
		"--parse-metadata", "%(album,playlist_title,title)s:%(meta_album)s",
		"--parse-metadata", "%(track,title)s:%(meta_title)s",
		"--parse-metadata", "%(track_number,playlist_index)s:%(meta_track)s",
		// The cover as a real file in the album folder, not only embedded:
		// Navidrome, Jellyfin and Plex all read a cover.jpg beside the tracks,
		// and one image per album is what a library wants where one embedded
		// copy per track is what a player wants. --convert-thumbnails makes
		// the extension a fact - the source's own is webp as often as jpg.
		"--write-thumbnail", "--convert-thumbnails", "jpg",
		"-o", "thumbnail:" + filepath.Join(dir, coverTemplate(tmpl)),
	}
}

// coverTemplate puts the cover beside the tracks: the album folder is
// whatever directory part the track template ends up with, so this follows a
// hand-written output template exactly as it follows the music one, and a
// template with no folders at all leaves the cover in the download directory
// next to the files.
func coverTemplate(tmpl string) string {
	const name = "cover.%(ext)s"
	if i := strings.LastIndexAny(tmpl, `/\`); i >= 0 {
		return tmpl[:i+1] + name
	}
	return name
}

// progressTemplateFor picks between the plain progress template and the one
// that also carries is_live - see live.go for both, and for why the plain one
// has to stay exactly what it was.
func progressTemplateFor(o Options) string {
	if o.Live.Enabled {
		return liveProgressTemplate
	}
	return progressTemplate
}

// formatSelector turns a resolution preset into yt-dlp's own -f value, or ""
// when nothing should be passed at all (QualityBest, or anything Sanitize
// would already have folded onto it - Qualities() no longer offers
// QualityAudioOnly, superseded by the dedicated VariantAudio row, but the
// constant and this fallthrough both stay so an install with one already
// saved just quietly reads as "no opinion" rather than refusing the whole
// settings load). QualityCustom is a verbatim passthrough with no selector
// logic of its own.
func formatSelector(o Options) string {
	if o.Quality == QualityCustom {
		return o.CustomFormat
	}
	if h, ok := heightCaps[o.Quality]; ok {
		return "bestvideo[height<=?" + h + "]+bestaudio/best[height<=?" + h + "]"
	}
	return ""
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	c := b.cancel[taskID]
	b.mu.Unlock()
	if c != nil {
		c()
		b.onUpdate(taskID, core.Update{Status: core.StatusPaused, Speed: 0})
	}
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	url := b.url[taskID]
	b.mu.Unlock()
	if url != "" {
		go b.run(taskID, url) // yt-dlp continues the .part by default
	}
}

func (b *Backend) Remove(taskID string, _ bool) {
	b.mu.Lock()
	if c, ok := b.cancel[taskID]; ok {
		c()
	}
	delete(b.url, taskID)
	b.mu.Unlock()
}

// errMsgRunes is how much of that line reaches a task. Counted in characters
// rather than bytes, which is the whole reason this is not a slice expression.
const errMsgRunes = 200

// errorLine picks the one line of yt-dlp's stderr a task carries, and it is
// deliberately neither the last line nor the last 200 bytes of one.
//
// It used to be both, and that threw away exactly the words that name the
// failure. yt-dlp states its verdict on a line beginning "ERROR:" and then
// keeps talking - a wiki link, a bug-report paragraph, a post-processor's
// warning - so the last line of the buffer is routinely the least informative
// one in it. And what identifies the failure stands at the FRONT of the ERROR
// line: the bot check's own line runs to about four hundred characters, of
// which the tail is a link to a wiki page, so what arrived on the task was
// "...for tips on effectively exporting YouTube cookies" and "Sign in to
// confirm you're not a bot" was gone before anything could read it.
//
// THIS WIDENS WHAT THE REST OF THE APP SEES, which is a behaviour change and
// not only a nicer sentence. notMine now gets the front of an "Unsupported
// URL: <long address>" line it used to lose behind the address, so links that
// died here will start being handed to the next backend instead - which is
// what core.Update.Unsupported was written for, arriving late. The shared
// classifier gets a longer sentence too, and that is precisely why the six
// causes are named in this package from the untouched buffer and sent as
// core.Update.Reason rather than left to a regex over this string; see
// diagnose.go, which says what goes wrong if they are not.
//
// Runes and not bytes, because a cut through the middle of a character reaches
// the browser as U+FFFD, and a video title or a site's own localised message is
// exactly where the non-ASCII in this buffer lives.
func errorLine(s string) string {
	var last string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The FIRST such line. A run that prints several has said the
		// interesting thing first and is reporting the consequences after it -
		// a post-processor with nothing to work on, a merge that had no
		// streams.
		if strings.HasPrefix(line, "ERROR:") {
			return clampRunes(line, errMsgRunes)
		}
		last = line
	}
	// Nothing announced itself as an error: a tool that died on a signal, a
	// binary that is not yt-dlp at all. The last non-empty line is what this
	// function always answered for that shape, and it stays that.
	return clampRunes(last, errMsgRunes)
}

// clampRunes keeps at most n characters from the FRONT of s.
func clampRunes(s string, n int) string {
	// One rune is at least one byte, so a string this short cannot need
	// cutting - and the ordinary failure never walks the loop at all.
	if len(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
