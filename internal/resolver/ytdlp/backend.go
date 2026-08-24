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

	// Options, when set, returns the current instance-wide yt-dlp
	// configuration - format/quality, subtitles, the output template,
	// playlist handling. nil reads exactly like Options{}, the zero value
	// that reproduces this backend's pre-Options behaviour, matching
	// RateLimit's own "nil means no opinion" contract.
	Options func() Options

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
		opts = b.Options()
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

// ProbeTitle asks yt-dlp for a link's real title without downloading
// anything - the per-task ASYNC probe Resolver's own doc comment (resolver.go)
// says is a different shape from the batched resolver.Checker deliberately
// left unbuilt there. --skip-download and --print %(title)s (no -f, no
// format list) ask yt-dlp to extract just enough metadata to answer, never
// the muxed formats a real download or a "--simulate" would resolve, so this
// is cheaper than the download it stands in for - though it is still one
// real process per call, including whatever anti-bot gauntlet the site puts
// in front of extraction, which is exactly why app.probeYtdlpTitle (the only
// caller) fires this once per staged task rather than batching a paste's
// worth of links into one call the way a Checker would.
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
// opening entries - but it also changes what --print %(title)s answers for
// an ordinary single-video URL in ways this change could not confirm safely
// from documented behaviour alone, and guessing wrong here means a working
// single-video probe breaks instead of a playlist probe staying merely slow.
// Without the flag, a playlist/channel URL's probe still runs rather than
// failing outright - slower, and %(title)s answers with the first entry's
// title rather than the playlist's own name - which is a known gap for a
// later pass, not a crash today.
func (b *Backend) ProbeTitle(ctx context.Context, url string) (string, error) {
	cmd := exec.CommandContext(ctx, b.bin, "--skip-download", "--no-warnings", "--print", "%(title)s", url)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	title := firstLine(string(out))
	if title == "" {
		return "", errors.New("ytdlp: probe returned no title")
	}
	return title, nil
}

// firstLine is the first non-empty line of s. A single-video probe's
// %(title)s prints exactly one line; the --flat-playlist gap documented on
// ProbeTitle above means a playlist/channel URL can print one title per
// entry instead, and the first is the best single answer available without
// a second, playlist-aware flag.
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
	switch {
	case o.Quality == QualityAudioOnly:
		args = append(args, "-f", "bestaudio/best", "-x")
	default:
		if f := formatSelector(o); f != "" {
			args = append(args, "-f", f)
		}
	}
	if o.Subtitles != SubtitlesOff {
		langs := o.SubtitleLangs
		if langs == "" {
			langs = DefaultSubtitleLangs
		}
		args = append(args, "--write-subs", "--sub-langs", langs)
		if o.SubtitleAuto {
			args = append(args, "--write-auto-subs")
		}
		if o.Subtitles == SubtitlesEmbed {
			args = append(args, "--embed-subs")
		}
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
// would already have folded onto it). QualityCustom is a verbatim
// passthrough with no selector logic of its own; QualityAudioOnly is
// handled by buildArgs directly, since it pairs -f with a second flag (-x)
// this function has no way to add.
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
