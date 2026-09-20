package ytdlp

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"strings"
)

// Jellyfin, Kodi and Plex match file names against online databases, which a
// downloaded video never matches. An NFO beside the file gives them title,
// date, description and artwork from yt-dlp's info json, which yt-dlp writes
// but has no NFO option for; finish deletes the json afterwards. It uses
// <movie>, the standalone item all three accept, instead of inventing series
// and episode numbers.

// infoDict is the subset of yt-dlp's info json this package reads. Every field
// is optional, and an unset one is left out of the NFO, since some scrapers
// store an empty element as a real empty value.
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

// readInfoJSON loads the document yt-dlp wrote for a finished download. A
// missing or unreadable file only reports false; the download itself is fine.
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

// nfoMovie is the document shape the three readers expect.
type nfoMovie struct {
	XMLName       xml.Name  `xml:"movie"`
	Title         string    `xml:"title,omitempty"`
	OriginalTitle string    `xml:"originaltitle,omitempty"`
	Plot          string    `xml:"plot,omitempty"`
	Runtime       int       `xml:"runtime,omitempty"` // whole minutes, Kodi's unit
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

// uniqueID is the source's own id, so a re-download updates the library entry
// instead of adding a second one.
type uniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

// maxNFOTags bounds the tag list; uploads can carry hundreds of search tags,
// and the meaningful ones come first.
const maxNFOTags = 20

// buildNFO turns one info dict into the document.
func buildNFO(d infoDict) nfoMovie {
	title := firstNonEmpty(d.Title, d.FullTitle)
	uploader := firstNonEmpty(d.Uploader, d.Channel)
	m := nfoMovie{
		Title: title,
		Plot:  strings.TrimSpace(d.Description),
		// Truncated, not rounded, to agree with what scrapers read from the
		// file.
		Runtime:  int(d.Duration) / 60,
		Studio:   uploader,
		Director: uploader,
		Thumb:    d.Thumbnail,
		Source:   d.WebpageURL,
	}
	// Only a different spelling is worth an originaltitle.
	if d.FullTitle != "" && d.FullTitle != title {
		m.OriginalTitle = d.FullTitle
	}
	if premiered, year := parseUploadDate(d.UploadDate); premiered != "" {
		m.Premiered, m.Year = premiered, year
	}
	m.Genres = trimList(d.Categories, maxNFOTags)
	m.Tags = trimList(d.Tags, maxNFOTags)
	if d.ID != "" {
		// Kodi treats the type as an opaque namespace; extractor_key names the
		// same source the same way every time.
		kind := strings.ToLower(strings.TrimSpace(d.Extractor))
		if kind == "" {
			kind = "ytdlp"
		}
		m.UniqueID = &uniqueID{Type: kind, Default: true, Value: d.ID}
	}
	return m
}

// writeNFO renders the document to path, with the XML header encoding/xml
// does not emit.
func writeNFO(path string, d infoDict) error {
	body, err := xml.MarshalIndent(buildNFO(d), "", "  ")
	if err != nil {
		return err
	}
	out := append([]byte(xml.Header), body...)
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// parseUploadDate converts yt-dlp's YYYYMMDD into the ISO date Kodi's
// <premiered> wants, plus the year. Anything but eight digits yields no date
// rather than a guess.
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
