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
// diagnostics bundle carries about these three programs. It NEVER contains
// anything fetched from the network - see Prober.Read.
type Status struct {
	Ytdlp   Tool `json:"ytdlp"`
	FFmpeg  Tool `json:"ffmpeg"`
	FFprobe Tool `json:"ffprobe"`
	// Managed is the record of a fetched copy, present whenever one has been
	// recorded - INCLUDING when that copy no longer starts, which is exactly
	// the case somebody needs to be told about. Ytdlp.Source says whether it is
	// the one running.
	Managed *ManagedRecord `json:"managed,omitempty"`
	// ManagedPath is where a fetched copy lives, sent alongside Managed. It
	// exists because of the one state where Ytdlp.Path is NOT that file: a
	// recorded copy that no longer starts. There the resolver has already
	// fallen back, Ytdlp.Path names the fallback, and the sentence the page has
	// to write is "a fetched copy is recorded at X and it does not start" - so
	// X has to arrive as its own field rather than be reconstructed by the
	// browser from a data directory it is never told.
	ManagedPath string `json:"managedPath,omitempty"`
	// Shadowed is what would run if the fetched copy were removed, and it is
	// nil unless a fetched copy is actually in force.
	//
	// This field is the answer to the slow failure this whole feature would
	// otherwise create. The container image is rebuilt on every release and its
	// packaged yt-dlp moves forward with it; a copy fetched into /data in March
	// sits there unchanged for ever and keeps shadowing that. So the operator
	// who fetched once to fix a broken extractor ends up, a year and a half
	// later, running an OLDER yt-dlp than the image ships - having "fixed" it.
	// Without this field and the sentence the settings card builds from it, the
	// feature makes the long run worse rather than better.
	Shadowed *Tool `json:"shadowed,omitempty"`
}

// cacheTTL is how long one set of `--version` spawns is reused.
//
// It is not a performance tweak, it is a bound on process creation. GET
// /api/mediatools is read by the Resolvers settings page on every load AND by
// buildDiagnostics on every GET /api/diagnostics, and the diagnostics page has
// a refresh button somebody holds down. Without a cache, a browser tab left
// open on it is a fountain of three (sometimes four) short-lived processes.
const cacheTTL = 60 * time.Second

// Prober reads the three versions and caches the answer.
//
// A value on app.App rather than a package-level singleton, for the reason
// internal/httpx's own package doc gives about clients: "a package-level client
// is a dependency nothing declares". A test cannot give one instance a
// different data directory without every other instance in the process
// silently receiving it too, and nothing in the wiring would show that they
// share.
type Prober struct {
	dataDir string

	// mu is held across the probe itself, not just around the cache fields.
	// That serialises concurrent callers onto ONE set of spawns instead of
	// letting five simultaneous page loads each start their own three
	// processes - which is the behaviour this cache exists for in the first
	// place. The cost is that a caller can wait out another caller's probe;
	// with the per-spawn timeout above that is bounded and it is the cheaper
	// side of the trade.
	mu     sync.Mutex
	cached Status
	// stamp fingerprints the fetched copy (its size and modification time) so
	// an install or a removal invalidates the cache even if Invalidate is not
	// called - a file swapped underneath us by hand is exactly the situation
	// where a stale version string is most misleading. It is a stat, not a
	// spawn, so checking it on every read is free.
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
// IT NEVER TOUCHES THE NETWORK, and that is a hard property rather than a
// current fact: this is what the settings page loads on mount and what the
// diagnostics bundle embeds, so a GitHub call folded in here would turn opening
// a settings page into an outbound request nobody opted into. Asking GitHub is
// its own route, behind its own button and its own opt-in switch.
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
// fingerprinted: a system yt-dlp replaced by a package upgrade is caught by the
// TTL, while the managed one is the file THIS process replaces and must never
// be described from a stale reading.
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

	// ffmpeg and ffprobe take "-version" with ONE dash, unlike yt-dlp's two.
	// That is not a typo to be tidied up: with two dashes ffmpeg exits 1 and
	// prints its usage, and the row would read "not found" for a program that
	// is installed and working.
	st.FFmpeg = probeTool(lookup("ffmpeg"), "-version", ffmpegVersion)
	st.FFprobe = probeTool(lookup("ffprobe"), "-version", ffmpegVersion)

	if p.dataDir != "" {
		if rec, err := LoadRecord(p.dataDir); err == nil && rec != nil {
			st.Managed = rec
			st.ManagedPath = BinaryPath(p.dataDir)
		}
	}

	// Only when the fetched copy is the one running: "what would run instead"
	// is a question with no meaning otherwise, and answering it anyway would
	// mean a fourth spawn on every probe for a line nothing draws.
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
// hands back the bare name when it does not - so a Tool that was not found
// still names what was looked for rather than showing an empty cell.
func lookup(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

// probeTool runs one program's version flag and reads the answer through parse.
// Everything it can go wrong with ends up in Detail verbatim, because the
// difference between "not installed", "installed but the wrong architecture"
// and "installed on a volume mounted noexec" is entirely in that string.
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
// (c) 2000-2024 the FFmpeg developers" - the third whitespace token.
//
// A build with an unusual banner falls back to the whole first line rather than
// to an empty cell: an unparsed line is still a fact somebody can read in a bug
// report, while a blank one looks like the program is missing.
func ffmpegVersion(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 3 && fields[1] == "version" {
		return fields[2]
	}
	return strings.TrimSpace(line)
}
