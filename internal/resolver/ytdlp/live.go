package ytdlp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// A live stream has no total, so a progress bar toward one is meaningless
// while the disk fills. With Options.Live.Enabled the progress line also
// carries is_live and a liveGuard bounds the recording; without it the plain
// template and parse are used, which is why parseProgress accepts both shapes.

// progressTemplate is the plain --progress-template: yt-dlp's progress dict as
// JSON behind a marker that sets it apart from other stdout lines.
const progressTemplate = "KLP:%(progress)j"

// liveProgressTemplate adds the info dict's is_live to every progress line
// (yt-dlp exposes info fields to progress templates under "info"), so no extra
// process is needed to spot a stream.
//
// is_live is rendered with %s inside quotes: True, False or NA all stay valid
// JSON, while a JSON conversion of an unset field would not be predictable.
const liveProgressTemplate = `KLP:{"live":"%(info.is_live)s","p":%(progress)j}`

// progressMarker prefixes both templates.
const progressMarker = "KLP:"

// progress is one reading of yt-dlp's progress dict.
type progress struct {
	Downloaded int64
	// Total is the exact total, else yt-dlp's estimate, else 0, which it
	// stays for a live stream.
	Total    int64
	Speed    int64
	Filename string
	// Live is what the info dict said, and LiveKnown whether it said anything.
	Live      bool
	LiveKnown bool
}

// progressPayload is the JSON shape of yt-dlp's progress dict.
type progressPayload struct {
	Downloaded int64   `json:"downloaded_bytes"`
	Total      int64   `json:"total_bytes"`
	TotalEst   float64 `json:"total_bytes_estimate"`
	Speed      float64 `json:"speed"`
	Filename   string  `json:"filename"`
}

func (p progressPayload) toProgress() progress {
	size := p.Total
	if size == 0 {
		size = int64(p.TotalEst)
	}
	return progress{Downloaded: p.Downloaded, Total: size, Speed: int64(p.Speed), Filename: p.Filename}
}

// parseProgress decodes one KLP: line in either template's shape. The wrapper
// is recognised by its "p" member rather than by the template this spawn
// used, since a dropped progress line leaves a working download stuck at zero
// on screen.
func parseProgress(line string) (progress, bool) {
	if !strings.HasPrefix(line, progressMarker) {
		return progress{}, false
	}
	payload := line[len(progressMarker):]
	var wrapper struct {
		Live string           `json:"live"`
		P    *json.RawMessage `json:"p"`
	}
	if err := json.Unmarshal([]byte(payload), &wrapper); err == nil && wrapper.P != nil {
		var inner progressPayload
		if err := json.Unmarshal(*wrapper.P, &inner); err != nil {
			return progress{}, false
		}
		p := inner.toProgress()
		p.Live, p.LiveKnown = parseLiveFlag(wrapper.Live)
		return p, true
	}
	var flat progressPayload
	if err := json.Unmarshal([]byte(payload), &flat); err != nil {
		return progress{}, false
	}
	return flat.toProgress(), true
}

// parseLiveFlag reads what "%(info.is_live)s" renders to: Python's True or
// False, NA for an unset field, "" for None. Anything else counts as unknown,
// so an ordinary download never gets the live caps.
func parseLiveFlag(s string) (live, known bool) {
	switch strings.TrimSpace(s) {
	case "True", "true":
		return true, true
	case "False", "false":
		return false, true
	default:
		return false, false
	}
}

// liveGuard watches a recording and says when it has run long enough or grown
// large enough to stop.
type liveGuard struct {
	limits  Live
	started time.Time
	// now is stubbed in tests.
	now func() time.Time
}

func newLiveGuard(l Live) *liveGuard {
	return &liveGuard{limits: l, now: time.Now}
}

// begin starts the clock once, at the first progress line that reports a
// live stream rather than at spawn, so extraction and waiting for a premiere
// do not count against the time limit.
func (g *liveGuard) begin(t time.Time) {
	if g.started.IsZero() {
		g.started = t
	}
}

// active reports whether a recording is under way, i.e. whether begin has run.
func (g *liveGuard) active() bool { return !g.started.IsZero() }

// elapsed is how long the recording has been running.
func (g *liveGuard) elapsed() time.Duration {
	if !g.active() {
		return 0
	}
	return g.now().Sub(g.started)
}

// exceeded returns the sentence naming the limit this reading passed, or "".
// The finished task carries that sentence.
func (g *liveGuard) exceeded(loaded int64) string {
	if !g.active() {
		return ""
	}
	if g.limits.MaxMinutes > 0 {
		limit := time.Duration(g.limits.MaxMinutes) * time.Minute
		if g.elapsed() >= limit {
			return fmt.Sprintf("live recording stopped at the %d minute limit", g.limits.MaxMinutes)
		}
	}
	if g.limits.MaxMB > 0 && loaded >= int64(g.limits.MaxMB)<<20 {
		return fmt.Sprintf("live recording stopped at the %d MiB limit", g.limits.MaxMB)
	}
	return ""
}

// note is what a running recording shows instead of a percentage: its running
// time and the bytes received.
func (g *liveGuard) note(loaded int64) string {
	return "live recording, " + formatDuration(g.elapsed()) + ", " + formatBytes(loaded)
}

// formatDuration writes a running time as h:mm:ss, or m:ss without hours.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h, m, s := total/3600, (total/60)%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// formatBytes writes a byte count in binary units, with one decimal above a
// kibibyte.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
