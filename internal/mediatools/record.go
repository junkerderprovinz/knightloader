// Package mediatools is what this build knows about the two external programs
// a media download actually runs: yt-dlp, which reads the page and picks the
// streams, and ffmpeg, which joins them. Neither is part of this binary and
// neither is a Go package - they are separate programs KnightLoader spawns,
// installed by the container image or by whoever set the desktop build up.
//
// Three things live here, and they are three because the first two must never
// touch the network:
//
//   - RESOLVE (resolve.go): which yt-dlp is actually going to run, out of the
//     managed copy, KL_YTDLP and PATH, and why the other two are not.
//   - PROBE (probe.go): what version each of the three programs reports,
//     behind a short cache so the settings page and the diagnostics bundle can
//     both read it on every load without turning into a process fountain.
//   - FETCH (fetch.go): downloading a yt-dlp release from GitHub, verifying it
//     against the release's own SHA2-256SUMS, PROVING IT RUNS on this machine,
//     and only then putting it where the resolver will start it.
//
// WHY THIS IS NOT UNDER internal/resolver/. check-docs-claims.mjs walks every
// directory under internal/resolver/ and fails on one it has no README name
// for, because "the resolvers ARE the architecture". This is not a resolver: it
// resolves no links and answers no Match. It is the tooling underneath one.
//
// WHY THE RECORD IS NOT IN settings.json, which is the obvious place for it and
// the wrong one for three separate reasons. The settings shell PUTs the whole
// document, so a record written here would be clobbered by whatever stale draft
// an unrelated browser tab saved next (which is exactly why
// routes_resolvers.go's own writes go through PatchSettings). settings.json is
// serialised whole into the diagnostics bundle attached to public bug reports.
// And internal/backup zips settings.json plus the database and nothing else, so
// a record in there would travel to a restored machine and point the resolver
// at a binary that does not exist on it. <dataDir>/tools/ytdlp.json is outside
// all three, and being outside all three is the point.
package mediatools

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ManagedRecord is what was fetched, when, and what it turned out to be. Kept
// so the settings page can say "you are running the copy KnightLoader fetched
// on the 3rd, tag 2026.08.11" without spawning anything, and so a diagnostics
// bundle answers "which yt-dlp was that" without a second round trip.
//
// SHA256 is the digest the release published and this file was verified
// against, not one computed later. It is in the diagnostics bundle on purpose
// and is safe to be: it is a public digest of a public file.
type ManagedRecord struct {
	Tag    string `json:"tag"`
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
	// Version is what the staged file PRINTED at the smoke test, not what the
	// tag says it should print. The two agreeing is what Install requires
	// before it replaces anything; storing the printed one means a later build
	// that loosens that rule still records the truth.
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// ToolsDir is where a fetched copy and its record live. One directory rather
// than the data root itself: the root is also the download folder's parent and
// a person browsing it should be able to tell "things KnightLoader fetched to
// run" from their own files at a glance.
func ToolsDir(dataDir string) string { return filepath.Join(dataDir, "tools") }

// BinaryPath is where a fetched yt-dlp is put. The name has no tag in it on
// purpose: the resolver has to be able to name the file without reading the
// record first, and a per-tag name would leave every superseded copy on disk
// for ever.
func BinaryPath(dataDir string) string {
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name = "yt-dlp.exe"
	}
	return filepath.Join(ToolsDir(dataDir), name)
}

func recordPath(dataDir string) string { return filepath.Join(ToolsDir(dataDir), "ytdlp.json") }

// LoadRecord reads the record, or returns nil when there is none. A record that
// exists but cannot be parsed is reported as an error rather than treated as
// absent: "there is a file here I cannot read" and "nothing was ever fetched"
// are different situations, and quietly fetching over the top of the first one
// hides whatever went wrong.
func LoadRecord(dataDir string) (*ManagedRecord, error) {
	b, err := os.ReadFile(recordPath(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec ManagedRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// saveRecord writes the record after a successful install. 0644 and not 0600:
// there is nothing secret in it, and a file the operator cannot read from a
// shell is one more thing to explain in a bug report.
func saveRecord(dataDir string, rec ManagedRecord) error {
	if err := os.MkdirAll(ToolsDir(dataDir), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(recordPath(dataDir), append(b, '\n'), 0o644)
}

// Remove deletes the fetched copy and its record, putting KL_YTDLP and PATH
// back in charge. Both removals are attempted even if the first fails: a record
// left behind with no binary is the "recorded but unusable" state resolve.go
// already has to survive, but a binary left behind with no record is a file
// nothing will ever start and nothing will ever mention again.
func Remove(dataDir string) error {
	binErr := os.Remove(BinaryPath(dataDir))
	if errors.Is(binErr, fs.ErrNotExist) {
		binErr = nil
	}
	recErr := os.Remove(recordPath(dataDir))
	if errors.Is(recErr, fs.ErrNotExist) {
		recErr = nil
	}
	if binErr != nil {
		return binErr
	}
	return recErr
}
