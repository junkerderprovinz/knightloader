// Package feed picks up links from RSS and Atom subscriptions. Like
// internal/watch it is unattended intake, so every question a person would be
// asked is answered by the configuration.
//
// The layout mirrors internal/watch: this file parses a document (Item, Feed,
// Parse), subscription.go is one configured subscription, poller.go polls one,
// and runner.go runs the configured set.
package feed

import (
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"crypto/sha256"
)

// maxBody caps what is read from one fetch, for an address that turns out to
// be a disk image or an endless stream.
const maxBody = 8 << 20

// maxItems caps how many entries are taken from one document. Every entry is
// also remembered, and that memory is persisted into a store with a size
// limit; a memory too large to write comes back empty after a restart and
// stages everything again. It must stay at or below maxSeen (poller.go).
const maxItems = 200

// Item is one entry of a feed.
type Item struct {
	// Title is the entry's name: the package a staged link goes into, and
	// what a title filter matches.
	Title string
	// Link is the absolute address to stage; Parse drops entries without one.
	Link string
	// GUID is the feed's own identifier for the entry (RSS guid, Atom id), or
	// empty. See Key.
	GUID string
	// Published is when the feed says the entry appeared, or zero. It is only
	// logged.
	Published time.Time
}

// Feed is one parsed document.
type Feed struct {
	// Title is the feed's own name, the package for entries without a title.
	Title string
	Items []Item
}

// Key recognises an entry again on the next poll and after a restart.
//
// It uses the guid or id, which publishers keep across title fixes, new file
// addresses and domain moves, and falls back to the link for feeds that
// publish no id (a link with a changing tracking parameter is then staged
// again). Title plus date is not used: it merges distinct entries that share a
// title such as "Episode" and splits one entry whose title was corrected.
//
// The value is hashed so long ids fit the size-limited store; 8 bytes of
// SHA-256 make a collision among a few hundred entries negligible. No marker
// records which field was used, so a feed that starts publishing its link as
// its guid is not staged a second time.
func (i Item) Key() string {
	raw := i.GUID
	if raw == "" {
		raw = i.Link
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

// Parse reads one RSS or Atom document, whichever it is. base is the address
// the document came from, which relative links resolve against.
func Parse(base string, r io.Reader) (Feed, error) {
	root, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return Feed{}, fmt.Errorf("feed: %s: %w", base, err)
	}
	dec := xml.NewDecoder(io.LimitReader(r, maxBody))
	dec.CharsetReader = charsetReader
	// Feeds often carry stray entities such as &nbsp; in a title; a strict
	// decoder would refuse the whole document over one.
	dec.Strict = false

	var doc document
	if err := dec.Decode(&doc); err != nil {
		return Feed{}, fmt.Errorf("feed: %s: %w", base, err)
	}

	// The formats differ in where the entries sit, so whichever place is
	// filled tells which one this is. Root element names are unreliable (RDF
	// is spelled <rdf:RDF> and <RDF>).
	raw, title := doc.Channel.Items, doc.Channel.Title
	switch {
	case len(raw) > 0:
		// RSS 2.0: <rss><channel><item>.
	case len(doc.Items) > 0:
		// RSS 1.0 puts the items beside the channel.
		raw = doc.Items
	case len(doc.Entries) > 0:
		// Atom: <feed><entry>, with the title at the root.
		raw, title = doc.Entries, doc.Title
	}
	if len(raw) > maxItems {
		raw = raw[:maxItems]
	}

	out := Feed{Title: text(title)}
	for _, e := range raw {
		link := resolve(root, e.link())
		if link == "" {
			// An entry without a link is dropped. Scanning its HTML
			// description is internal/linkscan's job, not a second copy here.
			continue
		}
		out.Items = append(out.Items, Item{
			Title:     text(e.Title),
			Link:      link,
			GUID:      text(e.guid()),
			Published: e.published(),
		})
	}
	return out, nil
}

// document decodes RSS and Atom at once. The formats disagree about names and
// nesting but never give one name two meanings.
type document struct {
	// Title is Atom's feed title; RSS keeps its title in channel.
	Title   string  `xml:"title"`
	Channel channel `xml:"channel"`
	// Items are RSS 1.0's, at the root beside <channel>. Without this an RSS
	// 1.0 feed parses cleanly into zero entries.
	Items   []entry `xml:"item"`
	Entries []entry `xml:"entry"`
}

type channel struct {
	Title string  `xml:"title"`
	Items []entry `xml:"item"`
}

// entry is one RSS <item> or Atom <entry>. Fields of the other format stay
// empty.
type entry struct {
	Title string `xml:"title"`
	// Links is a slice because Atom allows several; rel="self" is the feed's
	// own address and must not be staged.
	Links []link `xml:"link"`
	// GUID is RSS, ID is Atom.
	GUID string `xml:"guid"`
	ID   string `xml:"id"`
	// Enclosure is the file an RSS entry carries, the actual download for a
	// torrent or podcast feed.
	Enclosure enclosure `xml:"enclosure"`

	PubDate   string `xml:"pubDate"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
}

type link struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Text string `xml:",chardata"`
}

type enclosure struct {
	URL string `xml:"url,attr"`
}

// link picks the address to stage. An enclosure wins over the entry's link,
// since the enclosure is the file and the link the article about it.
func (e entry) link() string {
	if u := text(e.Enclosure.URL); u != "" {
		return u
	}
	var fallback string
	for _, l := range e.Links {
		// RSS writes the address as text, Atom as href.
		href := text(l.Href)
		if href == "" {
			href = text(l.Text)
		}
		if href == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(l.Rel)) {
		case "", "alternate":
			return href
		case "enclosure":
			// Atom's enclosure is used only if no plain link follows.
			if fallback == "" {
				fallback = href
			}
		}
	}
	return fallback
}

func (e entry) guid() string {
	if g := text(e.GUID); g != "" {
		return g
	}
	return text(e.ID)
}

// published returns the entry's date: RSS pubDate or Atom published, and Atom
// updated only when neither exists. Identity is Key's job, so a date only
// affects log lines.
func (e entry) published() time.Time {
	for _, raw := range []string{e.PubDate, e.Published, e.Updated} {
		if t, ok := parseTime(raw); ok {
			return t
		}
	}
	return time.Time{}
}

// timeLayouts are the date spellings feeds use. RFC 822 and RFC 3339 are often
// bent: single-digit days and missing time zones are common.
var timeLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
	time.RFC3339Nano,
	time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseTime(raw string) (time.Time, bool) {
	raw = text(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// resolve makes an entry's link absolute, or returns "" when it cannot be.
// Unknown schemes such as magnet: are kept; the resolvers decide what they
// accept.
func resolve(base *url.URL, raw string) string {
	raw = text(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	if base == nil || !base.IsAbs() {
		// A relative link with nothing to resolve against would become a task
		// without a host.
		return ""
	}
	return base.ResolveReference(u).String()
}

// text trims a value and collapses its inner whitespace, since indented XML
// puts newlines into titles that become package and folder names.
func text(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
