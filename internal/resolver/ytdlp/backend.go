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
	"strings"
	"sync"

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

	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	url    map[string]string // for resume
}

func NewBackend(bin, dir string, onUpdate func(taskID string, u core.Update)) *Backend {
	return &Backend{
		bin: bin, dir: dir, onUpdate: onUpdate,
		cancel: map[string]context.CancelFunc{},
		url:    map[string]string{},
	}
}

// Available reports whether the yt-dlp binary runs.
func (b *Backend) Available() bool {
	return exec.Command(b.bin, "--version").Run() == nil
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
	args := buildArgs(dir, opts)
	if b.RateLimit != nil {
		if lim := b.RateLimit(); lim > 0 {
			// --limit-rate is per fragment connection; fragments default to 1.
			args = append(args, "--limit-rate", fmt.Sprint(lim))
		}
	}
	cmd := exec.CommandContext(ctx, b.bin, append(args, url)...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
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

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // progress JSON lines can be long
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "KLP:"):
			var p struct {
				Status     string  `json:"status"`
				Downloaded int64   `json:"downloaded_bytes"`
				Total      int64   `json:"total_bytes"`
				TotalEst   float64 `json:"total_bytes_estimate"`
				Speed      float64 `json:"speed"`
				Filename   string  `json:"filename"`
			}
			if json.Unmarshal([]byte(line[4:]), &p) != nil {
				continue
			}
			size := p.Total
			if size == 0 {
				size = int64(p.TotalEst)
			}
			u := core.Update{Status: core.StatusRunning, Loaded: p.Downloaded, Size: size, Speed: int64(p.Speed)}
			if p.Filename != "" {
				u.Name = filepath.Base(p.Filename)
			}
			b.onUpdate(taskID, u)
		case strings.Contains(line, "[download] Destination:"):
			name := strings.TrimSpace(line[strings.Index(line, "Destination:")+len("Destination:"):])
			b.onUpdate(taskID, core.Update{Status: core.StatusRunning, Name: filepath.Base(name)})
		}
	}
	scanErr := sc.Err()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return // cancelled by Pause/Remove
	}
	if err == nil && scanErr != nil {
		err = scanErr
	}
	if err != nil {
		msg := tail(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		b.onUpdate(taskID, core.Update{
			Status: core.StatusError,
			Err:    "yt-dlp: " + msg,
			// yt-dlp saying it has no extractor for this link is not a download
			// failure — it means the link belongs to someone else. Saying so
			// lets a plain file whose URL carries no extension still be fetched.
			Unsupported: notMine(msg),
		})
		return
	}
	b.onUpdate(taskID, core.Update{Status: core.StatusDone, Speed: 0})
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
			FormatID       string  `json:"format_id"`
			Ext            string  `json:"ext"`
			Vcodec         string  `json:"vcodec"`
			Acodec         string  `json:"acodec"`
			Height         int     `json:"height"`
			Filesize       int64   `json:"filesize"`
			FilesizeApprox float64 `json:"filesize_approx"`
			Abr            float64 `json:"abr"`
		} `json:"formats"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return ProbeResult{}, fmt.Errorf("ytdlp: probe returned unparseable data: %w", err)
	}
	title := strings.TrimSpace(raw.Title)
	if title == "" {
		return ProbeResult{}, errors.New("ytdlp: probe returned no title")
	}
	res := ProbeResult{Title: title, Formats: make([]FormatEntry, 0, len(raw.Formats))}
	for _, f := range raw.Formats {
		res.Formats = append(res.Formats, FormatEntry{
			FormatID: f.FormatID, Ext: f.Ext, Vcodec: f.Vcodec, Acodec: f.Acodec,
			Height: f.Height, Filesize: f.Filesize, FilesizeApprox: int64(f.FilesizeApprox),
			Abr: f.Abr,
		})
	}
	return res, nil
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
		"--progress-template", "KLP:%(progress)j",
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
	switch o.Variant {
	case VariantAudio:
		args = append(args, "-f", "bestaudio/best", "-x")
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
	}
	tmpl := o.OutputTemplate
	if tmpl == "" {
		tmpl = defaultOutputTemplate
	}
	args = append(args, "-o", filepath.Join(dir, tmpl))
	return args
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

func tail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 200 {
		return s[len(s)-200:]
	}
	return s
}
