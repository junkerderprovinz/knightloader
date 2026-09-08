package mediatools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Source names where the yt-dlp that is about to run came from. It is a string
// and not a bool pair because the settings page says the word out loud: "the
// copy KnightLoader fetched", "KL_YTDLP", "found on PATH", "nowhere". A person
// looking at a card that says "2026.08.11" needs to know WHICH 2026.08.11 that
// is before they can act on it.
type Source string

const (
	// SourceManaged is the copy Install put beside the data directory.
	SourceManaged Source = "managed"
	// SourceEnv is KL_YTDLP, which the container image pins to
	// /usr/bin/yt-dlp on every install (Dockerfile).
	SourceEnv Source = "env"
	// SourcePath is a plain "yt-dlp" found on PATH.
	SourcePath Source = "path"
	// SourceNone is none of the three. The path returned alongside it is still
	// "yt-dlp", because that is what this app has always handed the backend
	// when it found nothing - the backend's own Available() is what turns that
	// into "the media resolver is not registered", and this package does not
	// take that decision away from it.
	SourceNone Source = "none"
)

// Tool is one external program as it stands on this machine right now.
//
// Detail is a fact about THIS machine in English, not a translated sentence:
// the same convention every Feature.Reason in routes_features.go follows, and
// for the same reason - it names a path, an errno or a program's own output,
// none of which survive being turned into a phrase in 42 languages.
type Tool struct {
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	// Source is filled in for yt-dlp only. ffmpeg and ffprobe are whatever is
	// on PATH and there is nothing to choose between, so a source word on their
	// rows would be one more thing to read that never varies.
	Source Source `json:"source,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// probeTimeout bounds every `--version` spawn in this package.
//
// It exists because of a specific failure mode, not as a round number: a binary
// that is half-written, quarantined mid-scan, or sitting on a network mount
// that has gone away does not fail to start - it hangs. internal/resolver/ytdlp
// Backend.Available() had no timeout at all until this change, and it is called
// from rewireBackends, which runs on every account save and on every sweep
// tick; one hanging yt-dlp wedged both.
const probeTimeout = 10 * time.Second

// ResolveYtdlp answers which yt-dlp will actually be started, in the order the
// answer is decided.
//
// THE MANAGED COPY OUTRANKS KL_YTDLP, and that is a deliberate decision rather
// than an oversight (jdp, 2026-09-08). The container image pins
// KL_YTDLP=/usr/bin/yt-dlp on every single install, so the other precedence
// would make "fetch a newer yt-dlp" a silent no-op on exactly the deployment
// where the distribution package lagging behind is the problem people hit. It
// would look like it worked - a 200, a new file on disk, a version in the
// record - and change nothing about what runs, which is the same shape as the
// KL_CNL drift the Dockerfile's own comment documents. It is safe for existing
// installs because no managed copy exists until somebody presses the button, so
// nothing changes on upgrade; and it is visible and reversible, because the
// settings card says plainly that KL_YTDLP is not being started and offers
// "back to the system copy".
//
// A RECORDED COPY THAT DOES NOT RUN LOSES. The managed copy only wins when the
// record exists AND the file exists AND it answers --version with exit 0.
// Anything else falls through to KL_YTDLP or PATH and says so in detail. That
// branch is not hypothetical: Windows Defender has a long history of
// quarantining PyInstaller-built yt-dlp.exe after it has been installed, and an
// operator who deletes /data/tools by hand must end up with a working yt-dlp
// again rather than with a dead path in the resolver table.
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
	// The historical fallback, unchanged: hand the backend the bare name and
	// let its own Available() decide. Returning an error here instead would
	// change what a machine with no yt-dlp does, which is not what this change
	// is for.
	return "yt-dlp", SourceNone, detail
}

// runsAtAll is the cheapest question worth asking about a binary: does the
// operating system start it and does it exit 0. Output is thrown away - the
// caller that wants the version string reads it separately, and this one is on
// the path taken by every rewireBackends.
func runsAtAll(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, bin, "--version").Run()
}
