package ytdlp

// Listing a playlist is a separate probe from ProbeTitle because
// --flat-playlist changes what a single-video URL answers there. Here a
// failure costs nothing: the link is simply staged as it is.

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

// maxPlaylistJSON caps how much listing one probe reads: over fifty thousand
// flat entries, far past any playlist meant for download. A larger listing is
// refused rather than truncated, since half a JSON document does not parse.
const maxPlaylistJSON = 32 << 20

// ErrPlaylistTooLarge is a listing over maxPlaylistJSON.
var ErrPlaylistTooLarge = errors.New("ytdlp: this playlist listing is larger than this app will read")

// PlaylistEntry is one video a listing points at, with the title the listing
// already carries.
type PlaylistEntry struct {
	URL   string
	Title string
}

// Playlist is one --flat-playlist listing: the playlist's title and its
// videos in source order. A single-video URL yields no entries and no error.
type Playlist struct {
	Title   string
	Entries []PlaylistEntry
	// Dropped counts entries that cannot be staged: deleted videos, nested
	// playlists (a channel's tabs), or bare ids instead of URLs.
	Dropped int
}

// ProbePlaylist asks yt-dlp what a link lists without extracting any entry.
// --flat-playlist answers from the listing page alone, cheap enough for paste
// time, and -J prints one object carrying the playlist's own title.
//
// A URL that is not a playlist yields no entries, which the caller stages as
// a single link; every failure path ends in that same behaviour. The caller
// bounds ctx (app.ytdlpPlaylistTimeout).
func (b *Backend) ProbePlaylist(ctx context.Context, rawurl string) (Playlist, error) {
	// Our own cancel stops yt-dlp when the size check below gives up, so it
	// does not block on a pipe nobody reads.
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
	// Read before Wait, which closes the pipe. Reading one byte past the cap
	// is enough to detect an oversized listing.
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
		msg := errorLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Playlist{}, fmt.Errorf("ytdlp: %s", msg)
	}
	return parsePlaylist(out)
}

// parsePlaylist reads one -J document.
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
	// yt-dlp uses "multi_video" for one work in several parts, which is
	// handled like a playlist. Anything else lists nothing.
	if raw.Type != "playlist" && raw.Type != "multi_video" {
		return pl, nil
	}
	pl.Entries = make([]PlaylistEntry, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		u := strings.TrimSpace(e.URL)
		// Nested playlists are not followed: a channel lists its tabs, and
		// recursing would stage its entire upload history.
		if e.Type == "playlist" || e.Type == "multi_video" || !addressable(u) {
			pl.Dropped++
			continue
		}
		pl.Entries = append(pl.Entries, PlaylistEntry{URL: u, Title: strings.TrimSpace(e.Title)})
	}
	return pl, nil
}

// addressable reports whether an entry is an http(s) URL. Some extractors
// list bare ids, which no backend could claim.
func addressable(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
