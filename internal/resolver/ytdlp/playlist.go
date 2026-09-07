package ytdlp

// playlist.go: what a playlist/channel URL LISTS, without touching a single
// video in it.
//
// It is a second probe rather than a widening of ProbeTitle (backend.go), and
// that split is deliberate. ProbeTitle's own doc comment states in so many
// words why it does not pass --flat-playlist: the flag changes what the info
// dict answers for an ORDINARY single-video URL in ways that could not be
// confirmed from documented behaviour alone, and guessing wrong there breaks a
// working single-video probe rather than merely leaving a playlist probe slow.
// That reasoning has not stopped being true, so the flag lives here, in a call
// that asks a different question and whose failure costs nothing: a listing
// that cannot be read leaves the link staged exactly as it was before this file
// existed.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// maxPlaylistJSON caps how much listing this app will read from one probe.
//
// A flat entry is a few hundred bytes (an id, a URL, a title, a duration), so
// thirty-two mebibytes is somewhere north of fifty thousand videos - far past
// any playlist a person means to download and far short of a channel archive
// that would be read into memory only to be thrown away by the caller's own
// entry limit. It is a refusal, not a truncation: half a JSON document is not
// a shorter listing, it is an unparseable one, so the probe reports that it
// could not answer and the link is staged the way it always was.
const maxPlaylistJSON = 32 << 20

// ErrPlaylistTooLarge is a listing over maxPlaylistJSON.
var ErrPlaylistTooLarge = errors.New("ytdlp: this playlist listing is larger than this app will read")

// PlaylistEntry is one video a listing points at - never the video itself.
// The title is the one the listing already carried, which is the whole reason
// a flat listing is worth having: it names every entry without a single
// extraction.
type PlaylistEntry struct {
	URL   string
	Title string
}

// Playlist is one --flat-playlist listing: what the playlist calls itself and
// the videos it points at, in the order the source states them.
//
// An ordinary single-video URL answers with an EMPTY Entries and no error -
// "this link lists nothing" is a real answer, not a failure, and the caller
// treats it exactly like a link that was never a playlist.
type Playlist struct {
	Title   string
	Entries []PlaylistEntry
	// Dropped counts entries this app cannot address: a deleted video the
	// listing still names, a nested playlist (a channel's own tabs), or an
	// extractor that reports an id where a URL should be. Reported rather
	// than hidden, because a listing of thirty that stages twenty-nine is a
	// number the person looking at the collector should be told.
	Dropped int
}

// ProbePlaylist asks yt-dlp what a link lists, WITHOUT extracting any of it.
//
// --flat-playlist is what makes this cheap enough to run at paste time: yt-dlp
// answers from the listing page alone instead of opening every entry, so a
// fifty-video playlist is one request rather than fifty extractions (which is
// exactly the cost resolver.go's own "no Checker here" comment refuses to pay).
// -J (--dump-single-json) prints ONE object for the whole listing, unlike
// ProbeTitle's -j, which prints one per entry: the difference matters here
// because the playlist's own title - what the package these entries land in
// gets named after - only exists on that single object.
//
// A URL that is not a playlist is not an error. yt-dlp reports the kind of
// thing it found in _type, and anything that is not a playlist comes back with
// no entries at all, which the caller reads as "stage this link the way it
// always was". That is the one behaviour this function must never get wrong:
// the whole feature is opt-in and every failure path has to end in today's
// behaviour rather than in a link nobody staged.
//
// The caller bounds ctx - see app.ytdlpPlaylistTimeout, the same arrangement
// ProbeTitle documents for app.ytdlpProbeTimeout.
func (b *Backend) ProbePlaylist(ctx context.Context, rawurl string) (Playlist, error) {
	// A cancel of this function's own, layered under the caller's: the size
	// refusal below has to stop yt-dlp rather than leave it writing into a
	// pipe nobody is reading any more, which is a process wedged for as long
	// as the parent lives.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.bin, "--skip-download", "--no-warnings", "--flat-playlist", "-J", rawurl)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Playlist{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Playlist{}, fmt.Errorf("ytdlp: %w", err)
	}
	// Read before Wait, always: Wait closes this pipe, so the other order
	// loses whatever had not been read yet. One byte over the cap is enough
	// to know the answer is too large, and the read stops there instead of
	// pulling the rest of a channel archive into memory to measure it.
	out, readErr := io.ReadAll(io.LimitReader(stdout, maxPlaylistJSON+1))
	if len(out) > maxPlaylistJSON {
		cancel()
		_ = cmd.Wait()
		return Playlist{}, fmt.Errorf("%w: over %d bytes", ErrPlaylistTooLarge, maxPlaylistJSON)
	}
	err = cmd.Wait()
	if err == nil && readErr != nil {
		err = readErr
	}
	if err != nil {
		msg := tail(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Playlist{}, fmt.Errorf("ytdlp: %s", msg)
	}
	return parsePlaylist(out)
}

// parsePlaylist reads one -J document. Split out from the process handling
// above for the same reason buildArgs is: every decision about what a listing
// means is unit-testable without spawning anything.
func parsePlaylist(out []byte) (Playlist, error) {
	var raw struct {
		Type    string `json:"_type"`
		Title   string `json:"title"`
		Entries []struct {
			Type  string `json:"_type"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Playlist{}, fmt.Errorf("ytdlp: playlist listing returned unparseable data: %w", err)
	}
	pl := Playlist{Title: strings.TrimSpace(raw.Title)}
	// "multi_video" alongside "playlist": yt-dlp uses it for a source whose
	// parts are one work (a stream split into segments, a lecture in
	// chapters). Those are as much "several downloads under one name" as a
	// playlist is, and the caller does the same thing with both. Anything
	// else - an ordinary video, a single storyboard - lists nothing, which is
	// the answer, not a failure.
	if raw.Type != "playlist" && raw.Type != "multi_video" {
		return pl, nil
	}
	pl.Entries = make([]PlaylistEntry, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		u := strings.TrimSpace(e.URL)
		// A nested playlist is NOT followed. A channel URL lists its tabs, and
		// each tab is a playlist of its own, so recursing here turns one paste
		// into somebody's entire upload history - the exact flood the caller's
		// own entry limit exists to prevent, arriving by a door that limit
		// cannot see. Counted as dropped, so the caller can say so.
		if e.Type == "playlist" || e.Type == "multi_video" || !addressable(u) {
			pl.Dropped++
			continue
		}
		pl.Entries = append(pl.Entries, PlaylistEntry{URL: u, Title: strings.TrimSpace(e.Title)})
	}
	return pl, nil
}

// addressable reports whether an entry names something this app can actually
// stage. Some extractors report a bare id in a flat listing rather than a URL,
// and staging one would put a task in the collector that no backend can ever
// claim - a row that exists only to report "no backend handles this link",
// which is worse than an honest count of entries that were left out.
func addressable(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
