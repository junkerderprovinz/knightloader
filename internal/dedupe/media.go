package dedupe

// The same video under another address. A video site hands one video out
// under many links (a share link, a tracking parameter, its place in a
// playlist), and yt-dlp knows them all as one extractor and one id. That pair
// is what its --download-archive keeps, and what the media archive here is
// keyed by.

import (
	"net/url"
	"regexp"
	"strings"
)

// MediaKey is a video's identity as yt-dlp's download archive writes it: the
// extractor in lower case, a space, and the id as the site spells it. It is
// "" unless both are known.
func MediaKey(extractor, id string) string {
	extractor, id = strings.TrimSpace(extractor), strings.TrimSpace(id)
	if extractor == "" || id == "" {
		return ""
	}
	return strings.ToLower(extractor) + " " + id
}

// youtubeID is the shape of a YouTube video id.
var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// MediaKeyOf reads the identity off a link where the address alone settles
// it, so the check needs no probe and answers before an automatic start could
// begin the download. That is YouTube only: elsewhere the id in an address is
// not reliably the one yt-dlp reports. A link that also names a playlist is
// left to the probe, since it may download the whole list.
func MediaKeyOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if u.Query().Has("list") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	var id string
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
			break
		}
		for _, prefix := range []string{"/shorts/", "/live/", "/embed/"} {
			if rest, ok := strings.CutPrefix(u.Path, prefix); ok {
				id = strings.TrimSuffix(rest, "/")
			}
		}
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	}
	if !youtubeID.MatchString(id) {
		return ""
	}
	return MediaKey("Youtube", id)
}
