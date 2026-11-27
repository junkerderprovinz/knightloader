package ytdlp

// A listing is what a channel or playlist subscription polls. Unlike
// ProbePlaylist it prints one line per entry as yt-dlp walks the pages, so a
// caller can stop as soon as it has enough, and a channel's tabs (videos,
// shorts, live) are walked rather than dropped as nested playlists.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/nowindow"
)

// maxListingLine caps one printed entry. An entry is a few hundred bytes; a
// longer line is not one of ours.
const maxListingLine = 64 << 10

// listingFields is the template every entry is printed with: only what a
// subscription needs, as one JSON object per line.
const listingFields = "%(.{_type,ie_key,id,url,title,duration,timestamp,live_status," +
	"playlist_title,playlist_channel,playlist_channel_id,playlist_uploader})j"

// ListingEntry is one video of a listing.
type ListingEntry struct {
	// ID is the extractor's id for the video, which is what identifies it
	// across title fixes and address changes.
	ID string
	// Extractor is yt-dlp's name for the site, "Youtube" for YouTube.
	Extractor string
	URL       string
	Title     string
	// Duration is in seconds, 0 when the listing does not say.
	Duration float64
	// Timestamp is when the video appeared in Unix seconds, 0 when unknown.
	// On YouTube it is approximate, worked out from "3 days ago".
	Timestamp int64
	// Playlist is the listing's own title, Channel the name of whoever
	// published it.
	Playlist  string
	Channel   string
	ChannelID string
}

// List walks a channel or playlist and hands each entry to each until it
// returns false. limit caps the entries per listing (per tab on a channel's
// main page), zero for all of them.
func (b *Backend) List(ctx context.Context, rawurl string, limit int, each func(ListingEntry) bool) error {
	// Our own cancel stops yt-dlp once each has had enough.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := listingArgs(limit)
	jar := ""
	if SignInAllowed(ctx) {
		jar = b.session(rawurl)
	}
	if jar != "" {
		path, cleanup, err := writeCookieFile("", jar)
		defer cleanup()
		if err != nil {
			return err
		}
		args = append(args, "--cookies", path)
	}
	args = append(args, b.proxyArgs()...)
	args = append(args, b.denoArgs()...)
	cmd := nowindow.CommandContext(ctx, b.binary(), append(args, rawurl)...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ytdlp: %w", err)
	}
	stopped := false
	readErr := readListing(stdout, func(e ListingEntry) bool {
		if each(e) {
			return true
		}
		stopped = true
		return false
	})
	if stopped {
		cancel()
		_ = cmd.Wait()
		return nil
	}
	err = cmd.Wait()
	if err == nil {
		err = readErr
	}
	if err != nil {
		msg := errorLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ytdlp: %s", msg)
	}
	return nil
}

// listingArgs are the arguments of one listing. approximate_date gives
// YouTube's flat entries a date, without which a backfill by days could not
// tell old from new.
func listingArgs(limit int) []string {
	args := []string{"--flat-playlist", "--no-warnings",
		"--extractor-args", "youtubetab:approximate_date", "-O", listingFields}
	if limit > 0 {
		args = append(args, "--playlist-end", strconv.Itoa(limit))
	}
	return args
}

// readListing parses printed entries until r ends or each returns false.
// Entries that cannot be staged are skipped: nested playlists, bare ids
// instead of addresses, and premieres that have not happened yet, which
// would fail if started and arrive on their own once they are videos.
func readListing(r io.Reader, each func(ListingEntry) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4<<10), maxListingLine)
	for sc.Scan() {
		var raw struct {
			Type       string  `json:"_type"`
			IEKey      string  `json:"ie_key"`
			ID         string  `json:"id"`
			URL        string  `json:"url"`
			Title      string  `json:"title"`
			Duration   float64 `json:"duration"`
			Timestamp  float64 `json:"timestamp"`
			LiveStatus string  `json:"live_status"`
			Playlist   string  `json:"playlist_title"`
			Channel    string  `json:"playlist_channel"`
			ChannelID  string  `json:"playlist_channel_id"`
			Uploader   string  `json:"playlist_uploader"`
		}
		if json.Unmarshal(sc.Bytes(), &raw) != nil {
			continue
		}
		u := strings.TrimSpace(raw.URL)
		if raw.Type == "playlist" || raw.Type == "multi_video" || !addressable(u) || raw.LiveStatus == "is_upcoming" {
			continue
		}
		channel := strings.TrimSpace(raw.Channel)
		if channel == "" {
			channel = strings.TrimSpace(raw.Uploader)
		}
		e := ListingEntry{
			ID:        strings.TrimSpace(raw.ID),
			Extractor: raw.IEKey,
			URL:       u,
			Title:     strings.TrimSpace(raw.Title),
			Duration:  raw.Duration,
			Timestamp: int64(raw.Timestamp),
			Playlist:  strings.TrimSpace(raw.Playlist),
			Channel:   channel,
			ChannelID: raw.ChannelID,
		}
		if !each(e) {
			return nil
		}
	}
	return sc.Err()
}
