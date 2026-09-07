package ytdlp

// nfo.go: the sidecar Jellyfin, Kodi and Plex actually read.
//
// Those three scrape a folder by matching filenames against an online
// database. A file called "Some Channel - How To Fix Anything.mkv" matches
// nothing, so it lands in the library as an untitled entry with no date, no
// description and no artwork - and the information they wanted was sitting in
// the info dict yt-dlp assembled during extraction and then threw away.
//
// yt-dlp writes that dict verbatim (--write-info-json) and nothing else; there
// is no --write-nfo. So the download asks for the json, this file turns it
// into a <movie> document beside the media file, and Backend.run deletes the
// json again - it was scaffolding this package asked for, not something the
// person downloading a video wanted on their disk.
//
// <movie> rather than <episodedetails> or <musicvideo>, for one video that is
// not part of a series: it is the element all three readers accept as a
// standalone item, and guessing a series/season/episode out of a YouTube
// upload would put made-up numbers in a library rather than a missing one.

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"strings"
)

// infoDict is the subset of yt-dlp's --write-info-json document this package
// reads. Everything is optional: extractors differ wildly in what they fill
// in, and a field nobody set has to leave the NFO element out rather than
// write an empty one, which some scrapers store as a real empty title.
type infoDict struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	FullTitle   string   `json:"fulltitle"`
	Description string   `json:"description"`
	Uploader    string   `json:"uploader"`
	Channel     string   `json:"channel"`
	UploadDate  string   `json:"upload_date"` // YYYYMMDD, yt-dlp's own format
	Duration    float64  `json:"duration"`    // seconds
	Thumbnail   string   `json:"thumbnail"`
	WebpageURL  string   `json:"webpage_url"`
	Extractor   string   `json:"extractor_key"`
	Categories  []string `json:"categories"`
	Tags        []string `json:"tags"`
}

// readInfoJSON loads the document yt-dlp wrote for a finished download.
//
// A missing or unreadable file is not an error worth failing a download over:
// the bytes are on disk and correct, and refusing to settle the task because
// a sidecar could not be written would turn a cosmetic feature into a reason
// downloads fail. Both callers (the NFO and the announced-duration half of
// the ffprobe check) treat a zero value as "nothing to say".
func readInfoJSON(path string) (infoDict, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return infoDict{}, false
	}
	var d infoDict
	if err := json.Unmarshal(b, &d); err != nil {
		return infoDict{}, false
	}
	return d, true
}

// nfoMovie is the document shape, as the three readers expect to find it.
// omitempty throughout, for the reason on infoDict above.
type nfoMovie struct {
	XMLName       xml.Name  `xml:"movie"`
	Title         string    `xml:"title,omitempty"`
	OriginalTitle string    `xml:"originaltitle,omitempty"`
	Plot          string    `xml:"plot,omitempty"`
	Runtime       int       `xml:"runtime,omitempty"` // whole minutes, which is the unit Kodi's own field is in
	Premiered     string    `xml:"premiered,omitempty"`
	Year          string    `xml:"year,omitempty"`
	Studio        string    `xml:"studio,omitempty"`
	Director      string    `xml:"director,omitempty"`
	Thumb         string    `xml:"thumb,omitempty"`
	Genres        []string  `xml:"genre,omitempty"`
	Tags          []string  `xml:"tag,omitempty"`
	UniqueID      *uniqueID `xml:"uniqueid,omitempty"`
	Source        string    `xml:"trailer,omitempty"`
}

// uniqueID is the id the source itself uses, kept so a re-download of the
// same video updates the library entry instead of adding a second one.
type uniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

// maxNFOTags bounds the tag list. A YouTube upload can carry several hundred
// keyword tags stuffed in for search ranking, and copying all of them into a
// library turns the tag browser into a wall of noise. Twenty is enough to
// carry the ones an uploader actually meant, which are the ones they put
// first.
const maxNFOTags = 20

// buildNFO turns one info dict into the document. Split from writeNFO so
// every decision about what goes in it is testable without a filesystem.
func buildNFO(d infoDict) nfoMovie {
	title := firstNonEmpty(d.Title, d.FullTitle)
	uploader := firstNonEmpty(d.Uploader, d.Channel)
	m := nfoMovie{
		Title: title,
		Plot:  strings.TrimSpace(d.Description),
		// Truncated, not rounded: a 90-second clip is one minute of runtime
		// and rounding it to two would make the NFO disagree with the file
		// the same scraper is looking at.
		Runtime:  int(d.Duration) / 60,
		Studio:   uploader,
		Director: uploader,
		Thumb:    d.Thumbnail,
		Source:   d.WebpageURL,
	}
	// originaltitle only when there is a second, longer spelling to record -
	// repeating the title into it says nothing and shows up twice in
	// Jellyfin's own metadata panel.
	if d.FullTitle != "" && d.FullTitle != title {
		m.OriginalTitle = d.FullTitle
	}
	if premiered, year := parseUploadDate(d.UploadDate); premiered != "" {
		m.Premiered, m.Year = premiered, year
	}
	m.Genres = trimList(d.Categories, maxNFOTags)
	m.Tags = trimList(d.Tags, maxNFOTags)
	if d.ID != "" {
		// The extractor's own name as the id type, lower-cased: "youtube",
		// "vimeo". Kodi treats the type as an opaque namespace, so what
		// matters is only that the same source always produces the same
		// word, which extractor_key does and a hand-maintained mapping in
		// this file would not.
		kind := strings.ToLower(strings.TrimSpace(d.Extractor))
		if kind == "" {
			kind = "ytdlp"
		}
		m.UniqueID = &uniqueID{Type: kind, Default: true, Value: d.ID}
	}
	return m
}

// writeNFO renders the document to path. The XML header is written by hand
// because encoding/xml does not emit one, and a scraper that reads the file
// as bytes needs the encoding declared.
func writeNFO(path string, d infoDict) error {
	body, err := xml.MarshalIndent(buildNFO(d), "", "  ")
	if err != nil {
		return err
	}
	out := append([]byte(xml.Header), body...)
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// parseUploadDate converts yt-dlp's own YYYYMMDD into the ISO date Kodi's
// <premiered> wants, plus the year on its own. A value that is not eight
// digits answers empty rather than a guess - an extractor that reports a
// partial date is better represented by no date at all than by 1 January.
func parseUploadDate(s string) (premiered, year string) {
	s = strings.TrimSpace(s)
	if len(s) != 8 {
		return "", ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", ""
		}
	}
	return s[:4] + "-" + s[4:6] + "-" + s[6:], s[:4]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func trimList(in []string, max int) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
		if len(out) == max {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
