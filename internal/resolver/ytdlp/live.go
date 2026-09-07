package ytdlp

// live.go: reading yt-dlp's progress lines, and bounding the ones that
// describe a recording rather than a download.
//
// is_live has been in the info dict every probe reads since this backend
// existed, and nothing ever looked at it. The result is that a link to a
// stream which has been running since Friday arrives as an ordinary task with
// an ordinary progress bar - a bar drawn from a total nobody knows, creeping
// toward a finish that will not happen, while the disk fills behind it. A
// weekend stream is not a slow download, and a percentage is the wrong thing
// to show for it.
//
// Everything here is off unless Options.Live.Enabled, INCLUDING the parse: the
// progress template buildArgs emits without that switch is byte for byte the
// one it has always emitted, so an install that never turns this on runs the
// same line format and the same decode it always did. That is why parseProgress
// below accepts two shapes rather than one.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// progressTemplate is the --progress-template this backend has always used:
// yt-dlp's whole progress dict, as JSON, behind a marker that tells it apart
// from every other line yt-dlp prints on stdout.
const progressTemplate = "KLP:%(progress)j"

// liveProgressTemplate is the same line with the one info-dict field the live
// guard needs folded in beside it. yt-dlp's progress templates expose the
// video's own fields under the "info" key (its documented example is
// "download-title:%(info.id)s-%(progress.eta)s"), so this asks for is_live on
// every progress line rather than paying a second process to find out whether
// a link is a stream.
//
// is_live is interpolated with %s and QUOTED, not with the json conversion:
// %s renders a Python bool as the literal text True or False and a missing
// field as NA, all three of which are valid inside a JSON string, whereas a
// json conversion of a field the extractor never set would have to produce
// something this decode is guessing at. A quoted string cannot break the
// document however it renders, and parseProgress reads exactly those three
// words.
const liveProgressTemplate = `KLP:{"live":"%(info.is_live)s","p":%(progress)j}`

// progressMarker prefixes both templates above. Everything after it on the
// line is the JSON one of them produced.
const progressMarker = "KLP:"

// progress is one reading off yt-dlp's own progress dict, reduced to what
// this backend does anything with.
type progress struct {
	Downloaded int64
	// Total is the exact total when the host reports one, otherwise yt-dlp's
	// estimate, otherwise 0 - and for a live stream it is 0 forever, which
	// is precisely the case the bar cannot describe.
	Total    int64
	Speed    int64
	Filename string
	// Live is what the info dict said, and LiveKnown whether it said
	// anything at all. Two fields rather than a *bool because every caller
	// wants "treat unknown as not live" and a pointer makes that a nil check
	// at each of them.
	Live      bool
	LiveKnown bool
}

// progressPayload is the JSON shape of yt-dlp's progress dict itself.
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

// parseProgress decodes one KLP: line, in either of the two shapes the two
// templates above produce.
//
// The wrapper is recognised by its "p" member rather than by which template
// this spawn happened to pass, so a line that arrives in the other shape than
// expected still decodes instead of being dropped. That matters more than it
// looks: dropping progress lines does not fail a download, it makes one that
// is working perfectly sit at zero bytes forever on the screen, which is a
// far more confusing bug report than an error would have been.
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

// parseLiveFlag reads what "%(info.is_live)s" renders to. Python's str() of a
// bool is the capitalised word; yt-dlp's own template engine writes NA for a
// field the extractor never set, and an empty string is what an extractor
// that set it to None produces. Anything else is treated as "did not say",
// because a value this function does not recognise is not evidence either way
// and guessing "live" would put the caps on an ordinary download.
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

// liveGuard watches a recording and says when it has run long enough or big
// enough to stop. Zero value: no limits, nothing started.
type liveGuard struct {
	limits  Live
	started time.Time
	// now is time.Now in production and a stub in tests, so the wall-clock
	// limit can be exercised without a test that actually waits minutes.
	now func() time.Time
}

func newLiveGuard(l Live) *liveGuard {
	return &liveGuard{limits: l, now: time.Now}
}

// begin starts the wall clock, once, on the first progress line that says
// this really is a stream.
//
// Started at the first LIVE line rather than at spawn on purpose: yt-dlp does
// real work before a byte arrives (extraction, the anti-bot gauntlet, waiting
// for a scheduled premiere to actually begin), and charging that against a
// "record for 60 minutes" limit would hand back a recording noticeably
// shorter than the number somebody set, for reasons nothing on screen
// explains.
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

// exceeded reports which limit, if any, this reading has passed - the empty
// string when none has. The returned sentence is what the finished task ends
// up carrying, so it names the limit and the number rather than saying that
// something was reached.
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

// note is what a running recording puts on the task in place of a percentage:
// how long it has been recording and how much has arrived. Both are facts; the
// share of a total is not, because there is no total.
func (g *liveGuard) note(loaded int64) string {
	return "live recording, " + formatDuration(g.elapsed()) + ", " + formatBytes(loaded)
}

// formatDuration writes a running time as h:mm:ss, dropping the hours only
// when there are none. Its own function rather than time.Duration's String
// because "1h23m45.123456789s" is not a running clock.
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

// formatBytes writes a byte count the way the rest of this app's own numbers
// read: binary units, one decimal above a kibibyte, none below.
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
