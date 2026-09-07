// Package feed picks up links from an RSS or Atom subscription. It is the same
// kind of intake as internal/watch and is built to the same shape on purpose:
// something out there produces links, nobody is sitting in front of the app when
// they appear, and every question a person would normally be asked has to be
// answered from the configuration instead.
//
// The split is the same one internal/watch uses, so the two can be read side by
// side: this file is what a feed document says (Item, Feed, Parse),
// subscription.go is one configured subscription and what it is allowed to
// contain, poller.go is one subscription being polled, and runner.go is the set
// of subscriptions the app has configured, which is more than one.
//
// Two things are deliberately NOT here, and both have their reasons written
// down where they are decided: an entry with no link at all is dropped rather
// than having its description scanned for one (Parse), and a subscription that
// has never been polled before hands over nothing at all on its first pass
// (poller.seed).
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

// maxBody caps what we are willing to read from one fetch. A feed document is a
// list of titles and links, so anything past this is either a misconfigured
// address pointing at a disk image or a host that answers a GET with an endless
// stream, and reading it would be a self-inflicted denial of service. Same
// reasoning and same order of magnitude as watch.maxIntakeSize.
const maxBody = 8 << 20

// maxItems caps how many entries are taken from one document.
//
// It is a guard, not a preference: every entry taken here is also an entry
// remembered (see poller.remember), and the memory is persisted into a store
// with a size limit of its own. A feed that answers with its entire ten year
// archive in one document would otherwise push a subscription's memory past
// that limit, and a memory that cannot be written is a memory that comes back
// empty on the next boot, which is precisely how the same hundred links get
// staged twice.
//
// It must stay at or below maxSeen (poller.go) so that one document can never
// be larger than what a subscription is able to remember about it.
const maxItems = 200

// Item is one entry of a feed: what it is called, where it points, and the two
// values used to recognise it again later.
type Item struct {
	// Title is the entry's own name. It is what a staged link is packaged
	// under, and it is what a subscription's title filter is matched against.
	Title string
	// Link is the address to stage. It is always absolute and never empty:
	// Parse drops an entry that has neither, because there would be nothing to
	// hand over.
	Link string
	// GUID is the feed's own identifier for the entry (RSS guid, Atom id), empty
	// when the feed publishes none. See Key.
	GUID string
	// Published is when the feed says the entry appeared, zero when it said
	// nothing usable. Nothing acts on it: it is carried for the log line and for
	// whoever comes back to a subscription next month wondering what it did.
	Published time.Time
}

// Feed is one parsed document.
type Feed struct {
	// Title is the feed's own name, used as the package name for an entry that
	// carries no title of its own.
	Title string
	Items []Item
}

// Key is how an entry is recognised again on the next poll and after a restart.
//
// THE THREE CANDIDATES, and why only two of them are used.
//
//	guid / id   The feed's own identifier, and the only value in the document
//	            that exists specifically to answer this question. A publisher
//	            that fixes a typo in a title, re-uploads a file under a new
//	            address or moves the whole site to a new domain keeps it, which
//	            is exactly the behaviour we want: those are all the same entry
//	            and none of them should be staged a second time.
//
//	link        Used when the feed publishes no id, which plenty of hand-rolled
//	            feeds do not. It is weaker than an id in one specific way, and it
//	            is worth knowing which: a feed that appends a tracking or session
//	            parameter to its links produces a new address for an entry that
//	            has not changed, and that entry is then staged again.
//
//	title+date  NOT used, and not as a third fallback either. It fails in both
//	            directions at once. It merges entries that are different: a
//	            weekly feed whose entries are all called "Episode" and carry no
//	            date would collapse into a single remembered entry, and every
//	            episode after the first would be silently dropped. And it splits
//	            entries that are the same: a publisher correcting a title, which
//	            is the commonest edit there is, produces a new key for an entry
//	            already staged. An entry always has a link by the time it gets
//	            here, so there is no gap for this tier to fill anyway.
//
// The result is hashed rather than stored raw because an id is often a long
// URL, and a subscription's memory is written into a store with a byte limit
// (see maxItems above and the app's own state adapter). Eight bytes of SHA-256
// is sixteen hex characters; with a few hundred entries remembered per
// subscription the chance of two of them colliding is far below the chance of
// the feed itself republishing something.
func (i Item) Key() string {
	raw := i.GUID
	if raw == "" {
		raw = i.Link
	}
	// Deliberately no marker saying which of the two the value came from. A feed
	// that starts publishing as its guid the value it used to publish only as its
	// link is describing the same entries it always did, and a marker would turn
	// that one edit into the whole feed arriving a second time.
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

// Parse reads one feed document. RSS 2.0 and Atom are both accepted, and the
// caller never has to say which it asked for: a subscription is an address a
// user pasted, and a publisher is free to switch format between two polls
// without telling anybody.
//
// base is the address the document was fetched from and is what a relative link
// is resolved against. Atom feeds routinely publish href="/2026/01/thing", and
// staging that verbatim would produce a task pointing at a path with no host.
func Parse(base string, r io.Reader) (Feed, error) {
	root, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return Feed{}, fmt.Errorf("feed: %s: %w", base, err)
	}
	dec := xml.NewDecoder(io.LimitReader(r, maxBody))
	// Without this, a document declaring anything but UTF-8 fails to parse in one
	// piece and the whole subscription silently produces nothing. See
	// charsetReader for what is and is not read.
	dec.CharsetReader = charsetReader
	// Feeds in the wild carry stray entities from whatever CMS wrote them
	// (&nbsp; in a title is the usual one). Strict off means such a document is
	// still read instead of being refused over a character in a title, which is
	// the same trade watch.Parse makes when it ignores a crawljob key it does not
	// know.
	dec.Strict = false

	var doc document
	if err := dec.Decode(&doc); err != nil {
		return Feed{}, fmt.Errorf("feed: %s: %w", base, err)
	}

	// Where the entries are is the only thing the three layouts disagree about,
	// so whichever place is populated is the format we were handed. Sniffing the
	// root element's name instead would mean a table of names, and the RDF
	// flavour alone is spelled <rdf:RDF> by one generator and <RDF> by another.
	raw, title := doc.Channel.Items, doc.Channel.Title
	switch {
	case len(raw) > 0:
		// RSS 2.0: <rss><channel><item>.
	case len(doc.Items) > 0:
		// RSS 1.0 keeps the channel, and its title, but hangs the items off the
		// root beside it rather than inside it.
		raw = doc.Items
	case len(doc.Entries) > 0:
		// Atom: <feed><entry>, with the feed's own title at the root.
		raw, title = doc.Entries, doc.Title
	}
	if len(raw) > maxItems {
		raw = raw[:maxItems]
	}

	out := Feed{Title: text(title)}
	for _, e := range raw {
		link := resolve(root, e.link())
		if link == "" {
			// Dropped rather than kept as an entry with nothing to download. Its
			// description may well contain a link in HTML, and scanning that is
			// deliberately not done here: internal/linkscan is what pulls links out
			// of a blob of markup, and a second, feed-flavoured copy of that job is
			// how the two end up disagreeing about what a link is.
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

// document is an RSS document and an Atom document at the same time. One struct
// rather than two plus a sniff of the root element, because the two formats
// disagree about names and nesting but never about the same name meaning two
// different things, so nothing here is ambiguous and there is no second decode
// to keep in step with the first.
type document struct {
	// Title is Atom's feed title. In RSS the title sits inside <channel> and so
	// never matches here, which is why channel carries one of its own.
	Title   string  `xml:"title"`
	Channel channel `xml:"channel"`
	// Items are RSS 1.0's, which is RDF and puts <item> at the root beside
	// <channel> instead of inside it. Without this field an RSS 1.0 feed parses
	// cleanly into zero entries, which is the worst shape of failure this package
	// has: no error anywhere and a subscription that simply never adds anything.
	Items   []entry `xml:"item"`
	Entries []entry `xml:"entry"`
}

type channel struct {
	Title string  `xml:"title"`
	Items []entry `xml:"item"`
}

// entry is one RSS <item> and one Atom <entry>. Every field one of the two
// formats has is declared; the other format simply leaves it empty, and the
// accessors below say which one wins when both are filled.
type entry struct {
	Title string `xml:"title"`
	// Links is a slice because Atom allows several, distinguished by rel: the
	// one worth staging is rel="alternate" or no rel at all, and rel="self" is
	// the feed pointing at itself. Taking the first one blindly is how a whole
	// subscription ends up staging the feed's own address once per entry.
	Links []link `xml:"link"`
	// GUID is RSS, ID is Atom. Both are the same claim, so both feed Key.
	GUID string `xml:"guid"`
	ID   string `xml:"id"`
	// Enclosure is the file an RSS entry carries, which for the feeds this app
	// exists to serve (a .torrent list, a podcast) is the actual download while
	// <link> is the human-readable page beside it. See link().
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

// link picks the address to stage.
//
// The enclosure wins over the entry's own link, and that ordering is the whole
// point of this intake: an entry that carries an enclosure is naming a file, and
// the <link> beside it is the article about that file. Staging the article would
// produce a task pointing at a web page for every single entry, which is a
// download nobody asked for and, for a torrent feed, not a download at all.
func (e entry) link() string {
	if u := text(e.Enclosure.URL); u != "" {
		return u
	}
	var fallback string
	for _, l := range e.Links {
		// RSS writes the address as the element's text, Atom as its href.
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
			// Atom's spelling of the same thing the RSS branch above takes, but it
			// is only used if no plain link turns up later in the entry, because
			// here the two are siblings rather than one being the file and the
			// other the page.
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

// published reads whichever date the entry carries.
//
// RSS pubDate and Atom published both mean "when this appeared". Atom updated
// means "when this last changed" and is taken only when neither of the other two
// is there, because a feed that reports an edit must not look like a new entry:
// the identity is Key's job and never the date's, so the worst an updated
// timestamp can do here is make a log line say the wrong thing.
func (e entry) published() time.Time {
	for _, raw := range []string{e.PubDate, e.Published, e.Updated} {
		if t, ok := parseTime(raw); ok {
			return t
		}
	}
	return time.Time{}
}

// timeLayouts are the spellings feeds actually use. RSS says RFC 822 and Atom
// says RFC 3339, and neither is reliably obeyed: two-digit day numbers without
// the leading zero and a missing timezone are both common enough that refusing
// them would leave real feeds with no date at all.
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

// resolve turns whatever the entry carried into an absolute address, or into
// nothing at all when it cannot be one.
//
// A scheme this app has no idea about is still returned: the resolvers decide
// what they can take, this package does not, and silently dropping a magnet link
// because it is not http would break the one feed shape a downloader is most
// likely to be pointed at.
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
		// Relative, with nothing to resolve it against. Returning it as it stands
		// would stage a task whose URL has no host, which fails later with an error
		// naming the path rather than the feed that produced it.
		return ""
	}
	return base.ResolveReference(u).String()
}

// text trims a value and collapses the whitespace inside it. Feed authors
// indent their XML, so a title routinely arrives with a newline and eight
// spaces in the middle of it, and that title becomes a package name and a folder
// on somebody's disk.
func text(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
