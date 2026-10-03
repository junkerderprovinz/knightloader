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
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

type Backend struct {
	// bin is guarded by mu, since SetBinary changes it while runs start.
	bin string
	dir string

	onUpdate func(taskID string, u core.Update)

	// RateLimit, when set, returns the current download limit in bytes/s
	// (0 = unlimited); applied per spawn via --limit-rate.
	RateLimit func() int64

	// Dir returns the destination for a task; nil or an empty result falls back
	// to the backend's default directory.
	Dir func(taskID string) string

	// Options, when set, returns the yt-dlp configuration for one task, since
	// the variant rows of one source URL each need their own. nil, or a zero
	// return, means Options{}.
	Options func(taskID string) Options

	// Cookies, when set, returns the stored cookies.txt text for a URL's host,
	// or "" (see CookieStore.Text). It is a hook rather than an Options field
	// because Options is a settings struct that ends up in settings.json, the
	// settings API and the diagnostics bundle. It is consulted only when the
	// task's Options.Cookies is on.
	Cookies func(rawurl string) string

	// Headers, when set, returns the request headers a browser handed over
	// with a task for rawurl, or nil: Cookie, Referer and User-Agent (see
	// browserArgs).
	Headers func(taskID, rawurl string) map[string]string

	// FFprobe is the binary Options.Measure reads a finished file with. Empty
	// means "ffprobe" on PATH, which the container's ffmpeg package provides.
	FFprobe string

	// Client is what the sites in players are asked through (see embed.go).
	// Nil uses a client of the package's own.
	Client *http.Client

	// InUse, when set, reports whether a task other than taskID still has
	// path. Remove leaves such a file where it is.
	InUse func(taskID, path string) bool

	mu   sync.Mutex
	runs map[string]*runState
	url  map[string]string // for resume
	// parts is every file a task's runs said they wrote, kept across a pause
	// until the task finishes, so a removal with files finds what an
	// unfinished download left (see Remove). Each one also goes to the task
	// as Update.WorkFile, which is what is left of the list after a restart.
	parts map[string][]string
}

// runState is one yt-dlp process of a task: cancel stops it, and ended is
// closed once the process has exited and its files are settled. live is set,
// under Backend.mu, once the process reports a live stream it is recording,
// and discard once a removal with files stops it.
type runState struct {
	cancel  context.CancelFunc
	ended   chan struct{}
	live    bool
	discard bool
}

// concurrentFragments is how many fragments of one video yt-dlp fetches at
// once, and httpChunkSize how large a single range request is: the values
// yt-dlp's own documentation uses. They are not settings; the speed limit
// covers anyone who wants a slower download.
const (
	concurrentFragments = 4
	httpChunkSize       = 10 << 20
)

func NewBackend(bin, dir string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		bin: bin, dir: dir, onUpdate: onUpdate,
		runs:  map[string]*runState{},
		url:   map[string]string{},
		parts: map[string][]string{},
	}
}

// availableTimeout bounds the spawn Available makes. A half-written,
// quarantined or unreachable binary hangs instead of failing, and Available
// runs inside app.rewireBackends on every account save and sweep tick. Ten
// seconds covers a cold PyInstaller start; tests shorten it.
var availableTimeout = 10 * time.Second

// SetBinary makes the runs started from now on use bin. A run already going
// keeps the binary it started with.
func (b *Backend) SetBinary(bin string) {
	b.mu.Lock()
	b.bin = bin
	b.mu.Unlock()
}

func (b *Backend) binary() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bin
}

// Available reports whether the yt-dlp binary runs.
func (b *Backend) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), availableTimeout)
	defer cancel()
	return nowindow.CommandContext(ctx, b.binary(), "--version").Run() == nil
}

func (b *Backend) Download(taskID, url string, _ map[string]string, _ int) {
	b.mu.Lock()
	b.url[taskID] = url
	b.mu.Unlock()
	b.launch(taskID, url)
}

// launch starts run on a goroutine of its own, registered before it goes, so
// a Halt that comes straight after finds it.
func (b *Backend) launch(taskID, url string) {
	ctx, r := b.register(taskID)
	go b.runAs(ctx, r, taskID, url)
}

// register records a new run of taskID.
func (b *Backend) register(taskID string) (context.Context, *runState) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{cancel: cancel, ended: make(chan struct{})}
	b.mu.Lock()
	b.runs[taskID] = r
	b.mu.Unlock()
	return ctx, r
}

func (b *Backend) run(taskID, url string) {
	ctx, r := b.register(taskID)
	b.runAs(ctx, r, taskID, url)
}

// runAs sends the run's last update only after ended is closed: the app takes
// a task off a backend that gave up on its link from inside that update, and a
// Remove that waits for the run would otherwise wait for itself.
func (b *Backend) runAs(ctx context.Context, r *runState, taskID, url string) {
	u, report := b.attempt(ctx, r, taskID, url)
	r.cancel()
	b.mu.Lock()
	if b.runs[taskID] == r {
		delete(b.runs, taskID)
	}
	if report && u.Status == core.StatusDone {
		// A finished file belongs to the task, and the app deletes it by the
		// path it recorded.
		delete(b.parts, taskID)
	}
	b.mu.Unlock()
	close(r.ended)
	if report {
		b.onUpdate(taskID, u)
	}
}

// attempt runs yt-dlp once and returns the update it ended with, or false when
// a Pause or Remove stopped it and there is nothing to report.
func (b *Backend) attempt(ctx context.Context, r *runState, taskID, url string) (core.Update, bool) {
	cancel := r.cancel
	dir := b.dir
	if b.Dir != nil {
		if d := b.Dir(taskID); d != "" {
			dir = d
		}
	}
	if err := os.MkdirAll(dir, filemode.Dir); err != nil {
		return core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()}, true
	}
	var opts Options
	if b.Options != nil {
		opts = b.Options(taskID)
	}
	opts = opts.Sanitize()
	target := url
	stream, unwrapped, err := unwrap(ctx, b.client(), url)
	if err != nil {
		if ctx.Err() != nil {
			return core.Update{}, false // cancelled by Pause/Remove
		}
		u := core.Update{Status: core.StatusError, Err: err.Error()}
		if errors.Is(err, errEmbedGone) {
			u.Reason = core.ReasonGone
		}
		return u, true
	}
	args := buildArgs(dir, opts)
	if unwrapped {
		target = stream.url
		args = append(args, stream.args()...)
	}
	var sent map[string]string
	if b.Headers != nil {
		sent = b.Headers(taskID, url)
	}
	jar := ""
	if opts.Cookies && b.Cookies != nil {
		jar = b.Cookies(url)
	}
	jar = withBrowserCookies(jar, url, sent["Cookie"], time.Now())
	// The cookie file lives exactly as long as yt-dlp: written before the
	// spawn, removed by the deferred cleanup on every exit path.
	if jar != "" {
		path, cleanup, err := writeCookieFile("", jar)
		defer cleanup()
		if err != nil {
			// Fatal rather than running anonymously a task that was set up
			// to be logged in.
			return core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()}, true
		}
		args = append(args, "--cookies", path)
	}
	args = append(args, browserArgs(sent)...)
	if b.RateLimit != nil {
		if lim := b.RateLimit(); lim > 0 {
			// --limit-rate applies per fragment connection, so the limit is
			// divided among them. The floor keeps a tiny limit from becoming
			// 0, which yt-dlp reads as unlimited.
			per := lim / concurrentFragments
			if per < 1 {
				per = 1
			}
			args = append(args, "--limit-rate", fmt.Sprint(per))
		}
	}
	cmd := nowindow.CommandContext(ctx, b.binary(), append(args, target)...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	// yt-dlp records and merges through ffmpeg, which shares its output.
	// Killing yt-dlp alone would leave ffmpeg writing, and the output open
	// until ffmpeg is done, which on a live stream is never.
	tree, err := newProcessTree(cmd)
	if err != nil {
		return core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()}, true
	}
	defer tree.close()
	cmd.Cancel = tree.kill
	if opts.Live.Enabled {
		// A killed yt-dlp leaves a live recording as an unplayable .part; on
		// an interrupt it finishes the fragment and runs its post-processors.
		// Windows has no os.Interrupt, so WaitDelay falls back to a kill there
		// and for a yt-dlp that ignores the signal. A recording removed with
		// its files has nothing worth finishing.
		cmd.Cancel = func() error {
			b.mu.Lock()
			discard := r.discard
			b.mu.Unlock()
			if discard {
				return tree.kill()
			}
			return cmd.Process.Signal(os.Interrupt)
		}
		cmd.WaitDelay = liveStopGrace
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return core.Update{Status: core.StatusError, Err: err.Error()}, true
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			// Stopped before the process started, which is no failure.
			return core.Update{}, false
		}
		return core.Update{Status: core.StatusError, Err: "yt-dlp: " + err.Error()}, true
	}
	tree.started()
	b.onUpdate(taskID, core.Update{Status: core.StatusRunning})

	// What finish acts on: the file yt-dlp produced, the subtitle files it
	// wrote, and whether a live limit ended the recording. written is every
	// file it wrote beside a download, or in place of one on the thumbnail,
	// subtitle and description rows.
	var (
		final     string
		written   []string
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
				b.mu.Lock()
				r.live = true
				b.mu.Unlock()
				// A recording has no total, so the note shows running time
				// and bytes instead.
				u.Note = guard.note(p.Downloaded)
				if stoppedBy == "" {
					if reason := guard.exceeded(p.Downloaded); reason != "" {
						stoppedBy = reason
						// Interrupts yt-dlp; the loop keeps reading so the
						// finalised file is still reported.
						cancel()
					}
				}
			}
			b.onUpdate(taskID, u)
			continue
		}
		if name, ok := finishedFile(line); ok {
			final = name
			b.remember(taskID, name)
			b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: filepath.Base(name), WorkFile: name})
			continue
		}
		if path, ok := wroteFile(line); ok {
			written = append(written, path)
			b.remember(taskID, path)
			b.onUpdate(taskID, core.Update{Status: core.StatusRunning, WorkFile: path})
		}
		if wroteSubtitle(line) {
			subFiles++
		}
	}
	scanErr := sc.Err()
	err = cmd.Wait()
	if ctx.Err() != nil && stoppedBy == "" {
		return core.Update{}, false // cancelled by Pause/Remove
	}
	if final == "" {
		final = sidecarFile(opts.Variant, written)
	}
	if stoppedBy != "" {
		// A recording that hit its own limit is finished, with the reason on
		// the task.
		return b.finish(opts, final, subFiles, stoppedBy), true
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
		return core.Update{
			Status: core.StatusError,
			Err:    "yt-dlp: " + msg,
			// Diagnosed from the whole buffer, before errorLine cuts it
			// down (see diagnose.go).
			Reason: Diagnose(raw),
			// No extractor for the link means another backend should try
			// it, such as the plain HTTP fallback.
			Unsupported: notMine(msg),
		}, true
	}
	return b.finish(opts, final, subFiles, ""), true
}

// liveStopGrace is how long a live recording gets after the interrupt to
// finalise its file before the process is killed.
const liveStopGrace = 30 * time.Second

// finish handles a successful yt-dlp exit around the file it produced: NFO,
// measurement and the checks that turn a success into a failure (a subtitle
// row without subtitles, and optionally a file shorter than the source
// announced). stoppedBy, a live limit's reason, stays on the note.
//
// It does not take run's context: the live guard cancels that context to stop
// a recording, and ffprobe would then never run for capped downloads.
func (b *Backend) finish(o Options, final string, subFiles int, stoppedBy string) core.Update {
	u := core.Update{Status: core.StatusDone, Speed: 0, Note: stoppedBy}
	// --no-warnings hides yt-dlp's note about a missing language, so the
	// subtitle files it wrote are the only evidence (see
	// Options.SubtitleStrict).
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
		// A row that wrote nothing new, or several subtitle files, which no
		// one path names.
		return u
	}
	// The progress lines counted one stream at a time, and the app deletes a
	// task's file only at the path and size the task recorded.
	if fi, err := os.Stat(final); err == nil && fi.Mode().IsRegular() {
		u.File, u.Size, u.Loaded = final, fi.Size(), fi.Size()
	}
	if o.Variant != VariantVideo && o.Variant != VariantAudio {
		// The row is named after its file, as the media rows are by their
		// progress lines, and shows the extension of its kind next to the
		// name (see core.Task.Ext). The NFO and the measurement describe
		// media, and an info json beside a thumbnail is the video row's.
		u.Name = strings.TrimSuffix(filepath.Base(final), filepath.Ext(final))
		return u
	}
	info, haveInfo := infoDict{}, false
	if needsInfoJSON(o) {
		path := infoJSONPath(final)
		info, haveInfo = readInfoJSON(path)
		if o.Embed.NFO && haveInfo {
			// A failed sidecar does not fail a complete download.
			_ = writeNFO(nfoPath(final), info)
		}
		// The info json was only requested as input for the steps above.
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
					// Still done unless Measure.FailOnShort: the bytes are
					// often worth having.
					u.Note = joinNote(warn, u.Note)
				}
			}
		}
		// A failed ffprobe (none installed, a lost mount) says nothing about
		// the download and adds no note.
	}
	return u
}

// joinNote puts two sentences on one line, dropping whichever is empty. The
// first goes first because the narrow note column truncates the end.
func joinNote(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + ", " + second
}

// needsInfoJSON reports whether this task asks yt-dlp for --write-info-json:
// the NFO is built from it, and the short-file check needs the duration the
// source announced.
func needsInfoJSON(o Options) bool { return o.Embed.NFO || o.Measure.Enabled }

// infoJSONPath is where yt-dlp put the info json for a finished file: the
// output template with the extension replaced.
func infoJSONPath(final string) string {
	return strings.TrimSuffix(final, filepath.Ext(final)) + ".info.json"
}

// nfoPath is the sidecar name Jellyfin, Kodi and Plex look for next to the
// media file.
func nfoPath(final string) string {
	return strings.TrimSuffix(final, filepath.Ext(final)) + ".nfo"
}

// Sidecars lists the files that describe a finished row's file and go when it
// is deleted: the NFO beside a video or audio file. The thumbnail, subtitle
// and description rows share the video's base name and have none.
func Sidecars(v Variant, file string) []string {
	if v != VariantVideo && v != VariantAudio {
		return nil
	}
	return []string{nfoPath(file)}
}

// finishedFile picks the path of a produced file out of yt-dlp's stdout. The
// caller keeps the last one, since the Merger and ExtractAudio lines follow
// the per-stream Destination lines. --print is not used because it implies
// --quiet and would drop the progress lines.
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

// wroteFile picks the path out of a line where yt-dlp says it wrote a file
// beside the download (info json, thumbnail, subtitles, description) or
// converted a thumbnail into a new one, which replaces the original.
func wroteFile(line string) (string, bool) {
	if i := strings.Index(line, "[info] Writing "); i >= 0 {
		if _, path, ok := strings.Cut(line[i:], " to: "); ok {
			if path = strings.TrimSpace(path); path != "" {
				return path, true
			}
		}
		return "", false
	}
	const converting = `Converting thumbnail "`
	if i := strings.Index(line, converting); i >= 0 {
		rest := line[i+len(converting):]
		j := strings.LastIndex(rest, `" to `)
		if j <= 0 {
			return "", false
		}
		from, ext := rest[:j], strings.TrimSpace(rest[j+len(`" to `):])
		if ext == "" {
			return "", false
		}
		return strings.TrimSuffix(from, filepath.Ext(from)) + "." + ext, true
	}
	return "", false
}

// sidecarFile is the file a thumbnail, subtitle or description row produced:
// the last one it wrote, which for a thumbnail is the converted copy. The
// media rows announce theirs with Destination lines, and several subtitle
// files have no one path to report.
func sidecarFile(v Variant, written []string) string {
	if v == VariantVideo || v == VariantAudio || len(written) == 0 || (v == VariantSubtitle && len(written) > 1) {
		return ""
	}
	return written[len(written)-1]
}

// wroteSubtitle recognises yt-dlp announcing a written subtitle file, the
// evidence Options.SubtitleStrict counts. It matches the end of the line,
// which manual and automatic tracks share.
func wroteSubtitle(line string) bool {
	return strings.Contains(line, "subtitles to:")
}

// FormatEntry is one entry of the "formats" array in yt-dlp's info dict,
// reduced to the fields KnightLoader reads. Vcodec or Acodec is "none" for
// the side a format does not carry, and Width and Height are 0 for audio and
// wherever the host reports none.
type FormatEntry struct {
	FormatID string
	Ext      string
	Vcodec   string
	Acodec   string
	Width    int
	Height   int
	// FPS is the frame rate, 0 when not reported or for an audio format.
	FPS float64
	// Filesize is the exact size when the host reports one. FilesizeApprox is
	// the estimate otherwise: yt-dlp's own, or, for the streaming formats it
	// leaves without one, the bitrate times the duration (see ProbeTitle). Read
	// Filesize first.
	Filesize       int64
	FilesizeApprox int64
	// Protocol is how yt-dlp fetches the format: "https" for one file,
	// "m3u8_native" or "http_dash_segments" for a streaming manifest.
	Protocol string
	// Default marks the formats yt-dlp picks when no -f is given, the video
	// and the audio of the merge or the one file it takes whole.
	Default bool
	// Abr is the average audio bitrate in kbit/s, 0 when not reported or for
	// a video-only format.
	Abr float64
	// Language is the audio language the source reported ("en", "de-DE"), ""
	// when it reported none.
	Language string
	// LanguagePreference is yt-dlp's ranking of the language: above default
	// for the original track, below it for a dub. 0 means absent, which
	// AudioLang.Dubbed reads as original.
	LanguagePreference int
}

// ProbeResult is what one -j extraction answers: the resolved title and every
// format the source offers, from a single process and round trip.
type ProbeResult struct {
	Title   string
	Formats []FormatEntry
	// Subtitles and AutoCaptions are the language codes of yt-dlp's
	// "subtitles" and "automatic_captions" maps, unsorted and kept apart
	// (see AvailableSubtitleLangs).
	Subtitles    []string
	AutoCaptions []string
	// Duration is the runtime the source announced in seconds, 0 when none
	// (as for a live stream).
	Duration float64
	// IsLive reports a running live stream (see Options.Live).
	IsLive bool
}

// ProbeTitle asks yt-dlp for a link's title and available formats without
// downloading anything. It is one process per call, anti-bot checks included,
// which is why app.probeYtdlpTitle runs it once per staged task and no batched
// resolver.Checker exists. -j returns the whole info dict at the cost of the
// title alone. The caller bounds ctx (app.ytdlpProbeTimeout).
//
// --flat-playlist is not passed because it changes the answer for ordinary
// single videos. A playlist URL therefore probes slowly and prints one object
// per entry, of which firstLine takes the first.
//
// sent are the headers a browser handed over with the link, or nil; they go to
// yt-dlp as they do for the download (see browserArgs).
func (b *Backend) ProbeTitle(ctx context.Context, url string, sent map[string]string) (ProbeResult, error) {
	args := []string{"--skip-download", "--no-warnings", "-j"}
	if jar := withBrowserCookies("", url, sent["Cookie"], time.Now()); jar != "" {
		path, cleanup, err := writeCookieFile("", jar)
		defer cleanup()
		if err != nil {
			return ProbeResult{}, err
		}
		args = append(args, "--cookies", path)
	}
	args = append(args, browserArgs(sent)...)
	stream, unwrapped, err := unwrap(ctx, b.client(), url)
	if err != nil {
		return ProbeResult{}, err
	}
	if unwrapped {
		args = append(args, stream.args()...)
		url = stream.url
	}
	cmd := nowindow.CommandContext(ctx, b.binary(), append(args, url)...)
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
		Title string `json:"title"`
		// FormatID is the default selection's ids, "401+251" for a merge.
		FormatID string `json:"format_id"`
		Formats  []struct {
			FormatID           string  `json:"format_id"`
			Ext                string  `json:"ext"`
			Vcodec             string  `json:"vcodec"`
			Acodec             string  `json:"acodec"`
			Width              int     `json:"width"`
			Height             int     `json:"height"`
			FPS                float64 `json:"fps"`
			Filesize           int64   `json:"filesize"`
			FilesizeApprox     float64 `json:"filesize_approx"`
			TBR                float64 `json:"tbr"`
			Abr                float64 `json:"abr"`
			Protocol           string  `json:"protocol"`
			Language           string  `json:"language"`
			LanguagePreference int     `json:"language_preference"`
		} `json:"formats"`
		// Only the language keys are read; RawMessage values keep an
		// unexpected shape inside from breaking the decode.
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
		// Newer extractors set live_status, older ones only is_live. A
		// finished stream (was_live) is an ordinary recording.
		IsLive:       raw.IsLive || raw.LiveStatus == "is_live",
		Subtitles:    keysOf(raw.Subtitles),
		AutoCaptions: keysOf(raw.AutoCaptions),
	}
	picked := map[string]bool{}
	for _, id := range strings.Split(raw.FormatID, "+") {
		if id != "" {
			picked[id] = true
		}
	}
	for _, f := range raw.Formats {
		approx := int64(f.FilesizeApprox)
		if f.Filesize <= 0 && approx <= 0 {
			approx = sizeFromBitrate(f.TBR, raw.Duration)
		}
		res.Formats = append(res.Formats, FormatEntry{
			FormatID: f.FormatID, Ext: f.Ext, Vcodec: f.Vcodec, Acodec: f.Acodec,
			Width: f.Width, Height: f.Height, FPS: f.FPS,
			Filesize: f.Filesize, FilesizeApprox: approx,
			Abr: f.Abr, Language: f.Language, LanguagePreference: f.LanguagePreference,
			Protocol: f.Protocol, Default: picked[f.FormatID],
		})
	}
	return res, nil
}

// sizeFromBitrate estimates a format's bytes from its total bitrate in kbit/s,
// as yt-dlp's filesize_from_tbr does. yt-dlp skips that estimate for streaming
// manifests, where tbr is often the peak rather than the average, which on
// YouTube leaves most of the HLS formats with no size at all. Overshooting a
// little beats a blank column, 0 for a live stream without a duration.
func sizeFromBitrate(tbr, duration float64) int64 {
	if tbr <= 0 || duration <= 0 {
		return 0
	}
	return int64(duration * tbr * 1000 / 8)
}

// keysOf returns the keys of one subtitle map, unsorted.
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

// firstLine is the first non-empty line of s: the only object for a single
// video, the first entry for a playlist (see ProbeTitle).
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

// buildArgs turns Options into yt-dlp's flags, without the binary and the URL
// the caller appends. It is pure so it can be tested without spawning yt-dlp.
// os/exec runs no shell, so CustomFormat and OutputTemplate cannot inject
// commands. o is sanitized again here so that Options{} yields the same
// arguments whether or not the caller sanitized it.
func buildArgs(dir string, o Options) []string {
	o = o.Sanitize()
	args := []string{
		"--newline", "--no-warnings", "--no-color",
		"--progress-template", progressTemplateFor(o),
		// yt-dlp fetches DASH and HLS fragments one at a time by default,
		// paying a round trip for each; YouTube throttles a single long range
		// response but not chunked ones. Both flags are harmless where they
		// do not apply.
		"--concurrent-fragments", strconv.Itoa(concurrentFragments),
		"--http-chunk-size", strconv.Itoa(httpChunkSize),
	}
	if !o.Playlist {
		args = append(args, "--no-playlist")
	}
	// Each variant (video, audio, thumbnail, subtitles, description) is its
	// own task with its own arguments.
	tmpl := outputTemplate(o)
	switch o.Variant {
	case VariantAudio:
		switch tr, ok := parseAudioTrack(o.AudioTrack); {
		case ok:
			// -x without --audio-format copies the picked track as it is.
			args = append(args, "-f", tr.selector(o.AudioLang), "-x")
		case o.AudioFormat != "" && o.AudioFormat != "best":
			// A track already in the format is copied, and only a source
			// without one is converted.
			fam := familyNamed(o.AudioFormat, audioFamilies)
			args = append(args, "-f", audioFamilySelector(fam, o.AudioLang), "-x", "--audio-format", extractFormat(o.AudioFormat))
		default:
			args = append(args, "-f", audioSelector(o.AudioLang), "-x")
		}
		// yt-dlp itself ignores the quality when nothing is transcoded.
		if o.AudioBitrate != "" {
			args = append(args, "--audio-quality", o.AudioBitrate+"K")
		}
		args = append(args, embedArgs(o, false)...)
		if o.Music {
			args = append(args, musicArgs(dir, tmpl)...)
		}
	case VariantThumbnail:
		// Converted to jpg so the row's extension is known in advance.
		args = append(args, "--skip-download", "--write-thumbnail", "--convert-thumbnails", "jpg")
	case VariantSubtitle:
		langs := o.SubtitleLangs
		if langs == "" {
			langs = DefaultSubtitleLangs
		}
		// srt instead of the site's default (usually vtt), so every player
		// reads it and the extension is known.
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
		// mkv takes any codec pairing, unlike mp4, and makes the container
		// known in advance. A picked track or format asks for its own
		// container first.
		merge := "mkv"
		if c, ok := pickedContainer(o.VideoPick); ok {
			merge = c.mergeFormat(o.Embed.Thumbnail)
		}
		args = append(args, "--merge-output-format", merge)
		args = append(args, embedArgs(o, true)...)
	}
	// Only the rows that download media need these.
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

// outputTemplate is the -o template this task runs with. Music mode supplies
// its own only when no template was typed; the cover follows whichever wins
// (see coverTemplate).
func outputTemplate(o Options) string {
	if o.OutputTemplate != "" {
		return o.OutputTemplate
	}
	if o.Music && o.Variant == VariantAudio {
		return musicOutputTemplate
	}
	return defaultOutputTemplate
}

// audioSelector is the audio row's -f value, with a language filter when one
// was asked for. Most sites report no per-track language, so the chain falls
// back to the unfiltered best track instead of failing. ^= also matches
// regional tags such as "de-DE".
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
	// Music mode needs --embed-metadata to write the fields --parse-metadata
	// fills in.
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
		// --embed-subs needs --write-subs and --sub-langs to fetch them
		// first. Audio files have no subtitle stream.
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

// musicOutputTemplate is the naming scheme music mode uses: artist folder,
// album folder, numbered tracks. Each field falls back to something video
// sites always provide, so no folder ends up named "NA".
const musicOutputTemplate = "%(artist,album_artist,creator,uploader)s/" +
	"%(album,playlist_title,title)s/" +
	"%(track_number,playlist_index)s-%(track,title)s.%(ext)s"

// musicArgs maps the same four fields onto the meta_* fields yt-dlp embeds
// (--parse-metadata FROM:TO) and writes a cover beside the tracks. Without the
// mapping, an album's tracks are tagged with the video's title and uploader.
func musicArgs(dir, tmpl string) []string {
	return []string{
		"--parse-metadata", "%(artist,album_artist,creator,uploader)s:%(meta_artist)s",
		"--parse-metadata", "%(album,playlist_title,title)s:%(meta_album)s",
		"--parse-metadata", "%(track,title)s:%(meta_title)s",
		"--parse-metadata", "%(track_number,playlist_index)s:%(meta_track)s",
		// Navidrome, Jellyfin and Plex read a cover.jpg beside the tracks.
		"--write-thumbnail", "--convert-thumbnails", "jpg",
		"-o", "thumbnail:" + filepath.Join(dir, coverTemplate(tmpl)),
	}
}

// coverTemplate puts the cover in the directory part of the track template,
// or in the download directory when the template has none.
func coverTemplate(tmpl string) string {
	const name = "cover.%(ext)s"
	if i := strings.LastIndexAny(tmpl, `/\`); i >= 0 {
		return tmpl[:i+1] + name
	}
	return name
}

// progressTemplateFor picks the plain progress template or the one that also
// carries is_live (see live.go).
func progressTemplateFor(o Options) string {
	if o.Live.Enabled {
		return liveProgressTemplate
	}
	return progressTemplate
}

// formatSelector turns a picked track, a preset's format wish or a resolution
// preset into yt-dlp's -f value, or "" for no selector (QualityBest and
// anything else without a resolution cap, including a stored QualityAudioOnly).
// QualityCustom passes through verbatim.
func formatSelector(o Options) string {
	if v, ok := parseVideoFormat(o.VideoPick); ok {
		return v.selector()
	}
	if w, ok := parseVideoWish(o.VideoPick); ok {
		return w.selector()
	}
	if o.Quality == QualityCustom {
		return o.CustomFormat
	}
	if r, ok := ResCap(o.Quality); ok {
		return capSelector(r)
	}
	return ""
}

func (b *Backend) Pause(taskID string) {
	b.mu.Lock()
	r := b.runs[taskID]
	b.mu.Unlock()
	if r != nil {
		r.cancel()
		b.onUpdate(taskID, core.Update{Status: core.StatusPaused, Speed: 0})
	}
}

func (b *Backend) Resume(taskID string) {
	b.mu.Lock()
	url := b.url[taskID]
	b.mu.Unlock()
	if url != "" {
		b.launch(taskID, url) // yt-dlp continues the .part by default
	}
}

// Recording reports whether the task's yt-dlp is recording a live stream,
// which Halt would end: yt-dlp finishes a recording when it is interrupted.
func (b *Backend) Recording(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.runs[taskID]
	return r != nil && r.live
}

// Halt stops the task's yt-dlp without reporting a pause and returns once it
// has exited, so the files it was writing can be moved. Resume starts it again
// in the folder Dir names by then. It reports whether one was running.
func (b *Backend) Halt(taskID string) bool {
	b.mu.Lock()
	r := b.runs[taskID]
	b.mu.Unlock()
	if r == nil {
		return false
	}
	r.cancel()
	<-r.ended
	return true
}

// Remove stops the task's yt-dlp and forgets the task. With deleteFiles it
// waits for the process to exit and deletes what the task's unfinished runs
// wrote; the file of a finished download is the app's to delete, by the path
// the task recorded. Without deleteFiles the list of those files is kept,
// since an undo can bring the task back to be removed with its files.
func (b *Backend) Remove(taskID string, deleteFiles bool) {
	b.mu.Lock()
	r := b.runs[taskID]
	if r != nil {
		r.discard = deleteFiles
	}
	delete(b.url, taskID)
	b.mu.Unlock()
	if r != nil {
		r.cancel()
	}
	if !deleteFiles {
		return
	}
	if r != nil {
		<-r.ended
	}
	b.mu.Lock()
	parts := b.parts[taskID]
	delete(b.parts, taskID)
	b.mu.Unlock()
	Discard(taskID, parts, b.InUse)
}

// remember notes a file a run of taskID wrote, for Remove.
func (b *Backend) remember(taskID, path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Contains(b.parts[taskID], path) {
		b.parts[taskID] = append(b.parts[taskID], path)
	}
}

// Discard deletes each file an unfinished download of taskID wrote together
// with its .part, its .ytdl progress file, its fragments and the .temp copy a
// post-processor writes beside it. A file inUse reports as another task's
// stays, and so do its .part and the rest.
func Discard(taskID string, paths []string, inUse func(taskID, path string) bool) {
	for _, p := range paths {
		if inUse != nil && inUse(taskID, p) {
			continue
		}
		gone := append([]string{p, p + ".part", p + ".ytdl", tempPath(p)}, fragmentFiles(p)...)
		for _, f := range gone {
			if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Printf("could not delete %s: %v (task %s)", f, err, taskID)
			}
		}
	}
}

// tempPath is the name yt-dlp's post-processors write to before replacing p.
func tempPath(p string) string {
	ext := filepath.Ext(p)
	return strings.TrimSuffix(p, ext) + ".temp" + ext
}

// fragmentFiles lists the fragments yt-dlp keeps beside p's .part while it
// fetches a DASH or HLS stream, named "<p>.part-Frag<n>".
func fragmentFiles(p string) []string {
	dir := filepath.Dir(p)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefix := filepath.Base(p) + ".part-Frag"
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// errMsgRunes is how many characters of the error line reach a task.
const errMsgRunes = 200

// errorLine picks the line of yt-dlp's stderr a task carries: the first line
// starting with "ERROR:", cut to its first errMsgRunes characters, or the last
// non-empty line when none does.
//
// yt-dlp keeps writing after its ERROR line (wiki links, bug-report text), and
// the words that identify a failure stand at the front of that line: a bot
// check's line is about four hundred characters, ending in a link. Keeping the
// front also lets notMine see "Unsupported URL" before a long address. Runes,
// not bytes, so a cut never splits a character.
func errorLine(s string) string {
	var last string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Later ERROR lines report consequences of the first.
		if strings.HasPrefix(line, "ERROR:") {
			return clampRunes(line, errMsgRunes)
		}
		last = line
	}
	return clampRunes(last, errMsgRunes)
}

// clampRunes keeps at most n characters from the front of s.
func clampRunes(s string, n int) string {
	// A rune is at least one byte, so a short string needs no cutting.
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
