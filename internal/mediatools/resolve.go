package mediatools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Source names where the yt-dlp that is about to run came from. A string and
// not a bool pair, because the settings page says the word out loud: "the copy
// KnightLoader fetched", "KL_YTDLP", "found on PATH", "nowhere". Somebody
// looking at a card that says "2026.08.11" needs to know which one that is.
type Source string

const (
	// SourceManaged is the copy Install put beside the data directory.
	SourceManaged Source = "managed"
	// SourceEnv is KL_YTDLP, which the container image pins to
	// /usr/bin/yt-dlp on every install (Dockerfile).
	SourceEnv Source = "env"
	// SourcePath is a plain "yt-dlp" found on PATH.
	SourcePath Source = "path"
	// SourceNone is none of the three. The path returned alongside it is
	// still "yt-dlp", which is what the backend is handed when nothing was
	// found: its own Available turns that into "the media resolver is not
	// registered", and this package leaves that decision to it.
	SourceNone Source = "none"
)

// Tool is one external program as it stands on this machine right now.
//
// Detail is a fact about this machine in English, not a translated sentence,
// the convention every Feature.Reason in routes_features.go follows: it names
// a path, an errno or a program's own output, none of which survive being
// turned into a phrase in 42 languages.
type Tool struct {
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	// Source is filled in for yt-dlp only. ffmpeg and ffprobe are whatever is
	// on PATH, with nothing to choose between, so a source word on their rows
	// would never vary.
	Source Source `json:"source,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// probeTimeout bounds every `--version` spawn in this package.
//
// A binary that is half-written, quarantined mid-scan or sitting on a network
// mount that has gone away does not fail to start, it hangs.
// internal/resolver/ytdlp Backend.Available is called from rewireBackends,
// which runs on every account save and every sweep tick, so one hanging yt-dlp
// wedges both.
const probeTimeout = 10 * time.Second

// ResolveYtdlp answers which yt-dlp will actually be started, in the order the
// answer is decided.
//
// The managed copy outranks KL_YTDLP. The container image pins
// KL_YTDLP=/usr/bin/yt-dlp on every install, so the other precedence would
// make "fetch a newer yt-dlp" a no-op on exactly the deployment where the
// distribution package lagging behind is the problem: a 200, a new file on
// disk, a version in the record, and nothing changed about what runs. Nothing
// changes on upgrade either, because no managed copy exists until somebody
// presses the button, and the settings card says that KL_YTDLP is not being
// started and offers "back to the system copy".
//
// A recorded copy that does not run loses. The managed copy only wins when the
// record exists, the file exists and it answers --version with exit 0.
// Anything else falls through to KL_YTDLP or PATH and says so in detail.
// Windows Defender has a long history of quarantining PyInstaller-built
// yt-dlp.exe after it is installed, and an operator who deletes /data/tools by
// hand has to end up with a working yt-dlp rather than a dead path in the
// resolver table.
//
// detail is empty when the first choice won and carries the reason it did not
// otherwise. It is never a reason to refuse: this function always returns
// something to run, even if that something is the bare word "yt-dlp".
func ResolveYtdlp(dataDir string) (path string, source Source, detail string) {
	if dataDir != "" {
		managed := BinaryPath(dataDir)
		rec, err := LoadRecord(dataDir)
		switch {
		case err != nil:
			detail = fmt.Sprintf("the record of the fetched copy (%s) could not be read: %v", recordPath(dataDir), err)
		case rec == nil:
			// Nothing was ever fetched. Not a detail: it is the ordinary case
			// on every install that has not pressed the button.
		default:
			if _, statErr := os.Stat(managed); statErr != nil {
				detail = fmt.Sprintf("a fetched copy is recorded at %s and the file is not there", managed)
			} else if runErr := runsAtAll(managed); runErr != nil {
				detail = fmt.Sprintf("the fetched copy at %s does not start: %v", managed, runErr)
			} else {
				return managed, SourceManaged, ""
			}
		}
	}

	if env := os.Getenv("KL_YTDLP"); env != "" {
		return env, SourceEnv, detail
	}
	if found, err := exec.LookPath("yt-dlp"); err == nil {
		return found, SourcePath, detail
	}
	// Hand the backend the bare name and let its own Available decide.
	return "yt-dlp", SourceNone, detail
}

// runsAtAll is the cheapest question worth asking about a binary: does the
// operating system start it and does it exit 0. The output is thrown away,
// because a caller that wants the version string reads it separately and this
// runs on the path every rewireBackends takes.
func runsAtAll(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, bin, "--version").Run()
}
