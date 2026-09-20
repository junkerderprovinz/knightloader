package mediatools

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status is everything GET /api/mediatools answers and everything the
// diagnostics bundle carries about these three programs. Nothing in it is
// fetched from the network, see Prober.Read.
type Status struct {
	Ytdlp   Tool `json:"ytdlp"`
	FFmpeg  Tool `json:"ffmpeg"`
	FFprobe Tool `json:"ffprobe"`
	// Managed is the record of a fetched copy, present whenever one has been
	// recorded, including when that copy no longer starts, which is the case
	// somebody needs to be told about. Ytdlp.Source says whether it is the one
	// running.
	Managed *ManagedRecord `json:"managed,omitempty"`
	// ManagedPath is where a fetched copy lives, sent alongside Managed. It
	// exists for the one state where Ytdlp.Path is not that file: a recorded
	// copy that no longer starts, where the resolver has fallen back and
	// Ytdlp.Path names the fallback. The page writes "a fetched copy is
	// recorded at X and it does not start", and the browser is never told the
	// data directory X would have to be built from.
	ManagedPath string `json:"managedPath,omitempty"`
	// Shadowed is what would run if the fetched copy were removed, and it is
	// nil unless a fetched copy is actually in force.
	//
	// The container image is rebuilt on every release and its packaged yt-dlp
	// moves forward with it, while a copy fetched into /data in March sits
	// there unchanged and keeps shadowing it. Without this field, and the
	// sentence the settings card builds from it, the operator who fetched once
	// to fix a broken extractor ends up running an older yt-dlp than the image
	// ships.
	Shadowed *Tool `json:"shadowed,omitempty"`
}

// cacheTTL is how long one set of `--version` spawns is reused.
//
// A bound on process creation rather than a performance tweak. GET
// /api/mediatools is read by the Resolvers settings page on every load and by
// buildDiagnostics on every GET /api/diagnostics, and the diagnostics page has
// a refresh button somebody holds down. Without a cache, a browser tab left
// open on it is a fountain of short-lived processes.
const cacheTTL = 60 * time.Second

// Prober reads the three versions and caches the answer.
//
// A value on app.App rather than a package-level singleton, for the reason
// internal/httpx's package doc gives about clients: a test cannot give one
// instance a different data directory without every other instance in the
// process receiving it too, and nothing in the wiring shows that they share.
type Prober struct {
	dataDir string

	// mu is held across the probe itself, not only around the cache fields, so
	// concurrent callers share one set of spawns instead of five simultaneous
	// page loads each starting three processes. The cost is that a caller can
	// wait out another caller's probe, which the per-spawn timeout bounds.
	mu     sync.Mutex
	cached Status
	// stamp fingerprints the fetched copy (its size and modification time) so
	// an install or a removal invalidates the cache even when Invalidate is
	// not called: a file swapped by hand is where a stale version string is
	// most misleading. It is a stat rather than a spawn, so checking it on
	// every read is free.
	stamp string
	at    time.Time
	have  bool
}

// NewProber builds the prober for one data directory. dataDir may be empty,
// which simply means no managed copy is possible.
func NewProber(dataDir string) *Prober { return &Prober{dataDir: dataDir} }

// Invalidate drops the cache, so the next Read spawns again. Called after an
// install or a revert, where waiting up to a minute for the page to tell the
// truth about what just happened is not acceptable.
func (p *Prober) Invalidate() {
	p.mu.Lock()
	p.have = false
	p.mu.Unlock()
}

// Read answers the current status, from the cache when it is still good.
//
// It never touches the network, and that is a property to keep: this is what
// the settings page loads on mount and what the diagnostics bundle embeds, so
// a GitHub call folded in here would turn opening a settings page into an
// outbound request nobody opted into. Asking GitHub is its own route, behind
// its own button and its own opt-in switch.
func (p *Prober) Read() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	stamp := p.fingerprint()
	if p.have && p.stamp == stamp && time.Since(p.at) < cacheTTL {
		return p.cached
	}
	p.cached = p.probe()
	p.stamp = stamp
	p.at = time.Now()
	p.have = true
	return p.cached
}

// fingerprint is a cheap stat of the fetched copy. Only the managed copy is
// fingerprinted: a system yt-dlp replaced by a package upgrade is caught by
// the TTL, while the managed one is the file this process replaces and must
// not be described from a stale reading.
func (p *Prober) fingerprint() string {
	if p.dataDir == "" {
		return ""
	}
	fi, err := os.Stat(BinaryPath(p.dataDir))
	if err != nil {
		return "none"
	}
	return fi.ModTime().UTC().Format(time.RFC3339Nano) + "/" + strconv.FormatInt(fi.Size(), 10)
}

// probe does the actual spawning. Called with mu held.
func (p *Prober) probe() Status {
	var st Status

	path, source, detail := ResolveYtdlp(p.dataDir)
	st.Ytdlp = probeTool(path, "--version", ytdlpVersion)
	st.Ytdlp.Source = source
	// The resolver's own explanation wins over the prober's: "the fetched copy
	// at X does not start" says considerably more than "exec: no such file".
	if detail != "" {
		st.Ytdlp.Detail = detail
	}
	if !st.Ytdlp.Found && st.Ytdlp.Detail == "" {
		st.Ytdlp.Detail = "no yt-dlp found: neither a fetched copy, nor KL_YTDLP, nor \"yt-dlp\" on PATH"
	}

	// ffmpeg and ffprobe take "-version" with one dash, unlike yt-dlp's two.
	// With two dashes ffmpeg exits 1 and prints its usage, and the row would
	// read "not found" for a program that is installed and working.
	st.FFmpeg = probeTool(lookup("ffmpeg"), "-version", ffmpegVersion)
	st.FFprobe = probeTool(lookup("ffprobe"), "-version", ffmpegVersion)

	if p.dataDir != "" {
		if rec, err := LoadRecord(p.dataDir); err == nil && rec != nil {
			st.Managed = rec
			st.ManagedPath = BinaryPath(p.dataDir)
		}
	}

	// Only when the fetched copy is the one running: otherwise "what would
	// run instead" means nothing, and answering it would cost a fourth spawn
	// on every probe for a line nothing draws.
	if source == SourceManaged {
		fallbackPath, fallbackSource, _ := resolveSystemYtdlp()
		shadowed := probeTool(fallbackPath, "--version", ytdlpVersion)
		shadowed.Source = fallbackSource
		st.Shadowed = &shadowed
	}
	return st
}

// resolveSystemYtdlp answers ResolveYtdlp's question with the managed copy
// taken out of the running: KL_YTDLP, then PATH. It is what Status.Shadowed
// reports and what "back to the system copy" would put in charge.
func resolveSystemYtdlp() (string, Source, string) {
	if env := os.Getenv("KL_YTDLP"); env != "" {
		return env, SourceEnv, ""
	}
	if found, err := exec.LookPath("yt-dlp"); err == nil {
		return found, SourcePath, ""
	}
	return "yt-dlp", SourceNone, ""
}

// lookup resolves a bare program name to a full path when PATH has it, and
// hands back the bare name when it does not, so a Tool that was not found
// still names what was looked for rather than showing an empty cell.
func lookup(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

// probeTool runs one program's version flag and reads the answer through
// parse. Whatever went wrong ends up in Detail verbatim, because the
// difference between "not installed", "installed but the wrong architecture"
// and "installed on a volume mounted noexec" is in that string.
func probeTool(bin, flag string, parse func(string) string) Tool {
	t := Tool{Path: bin}
	if bin == "" {
		return t
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	// CombinedOutput and not Output: some ffmpeg builds print the banner on
	// stderr, and a version that landed on the wrong stream would read as a
	// missing program.
	out, err := exec.CommandContext(ctx, bin, flag).CombinedOutput()
	if err != nil {
		t.Detail = strings.TrimSpace(err.Error())
		if ctx.Err() != nil {
			t.Detail = "did not answer " + flag + " within " + probeTimeout.String()
		}
		return t
	}
	t.Found = true
	t.Version = parse(firstLine(string(out)))
	return t
}

func firstLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// ytdlpVersion: `yt-dlp --version` prints the version and nothing else.
func ytdlpVersion(line string) string { return strings.TrimSpace(line) }

// ffmpegVersion pulls the version out of "ffmpeg version 6.1.2-r1 Copyright
// (c) 2000-2024 the FFmpeg developers", the third whitespace token.
//
// A build with an unusual banner falls back to the whole first line rather
// than to an empty cell: an unparsed line is still a fact somebody can read in
// a bug report, while a blank one looks like a missing program.
func ffmpegVersion(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 3 && fields[1] == "version" {
		return fields[2]
	}
	return strings.TrimSpace(line)
}
