package feed

import (
	"strings"
	"testing"
	"time"
)

// rssDoc is a plain RSS 2.0 document with the three shapes that matter: an entry
// whose enclosure is the real file, an entry with only a link, and an entry with
// no address at all.
const rssDoc = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Kellerfunk</title>
    <link>https://example.invalid/</link>
    <item>
      <title>
        Folge 1
      </title>
      <link>https://example.invalid/folge-1</link>
      <guid isPermaLink="false">kf-0001</guid>
      <pubDate>Mon, 02 Feb 2026 10:00:00 +0100</pubDate>
      <enclosure url="https://example.invalid/files/folge-1.mp3" length="12" type="audio/mpeg"/>
    </item>
    <item>
      <title>Folge 2</title>
      <link>/folge-2</link>
      <pubDate>2 Feb 2026 11:00:00 +0100</pubDate>
    </item>
    <item>
      <title>Nur eine Ankuendigung</title>
    </item>
  </channel>
</rss>`

// atomDoc is the same feed in the other format, with the two link traps: a
// rel="self" that points at the feed itself, and a relative alternate.
const atomDoc = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Turmwache</title>
  <link rel="self" href="https://example.invalid/atom.xml"/>
  <entry>
    <title>Erste Runde</title>
    <id>urn:uuid:0001</id>
    <link rel="self" href="https://example.invalid/atom.xml"/>
    <link rel="alternate" href="/runde/1"/>
    <updated>2026-02-02T09:00:00Z</updated>
  </entry>
  <entry>
    <title>Zweite Runde</title>
    <id>urn:uuid:0002</id>
    <link href="https://example.invalid/runde/2"/>
    <published>2026-02-03T09:00:00Z</published>
  </entry>
</feed>`

func TestParseRSS(t *testing.T) {
	f, err := Parse("https://example.invalid/rss.xml", strings.NewReader(rssDoc))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Kellerfunk" {
		t.Errorf("feed title is %q, want Kellerfunk", f.Title)
	}
	if len(f.Items) != 2 {
		t.Fatalf("got %d items, want 2: the entry with no address at all must be dropped", len(f.Items))
	}

	first := f.Items[0]
	// The title arrives across three lines with eight spaces of indentation in
	// the document, and it becomes a package name and a folder on somebody's disk.
	if first.Title != "Folge 1" {
		t.Errorf("title is %q, want %q", first.Title, "Folge 1")
	}
	// The enclosure is the file; the link beside it is the page about the file.
	if first.Link != "https://example.invalid/files/folge-1.mp3" {
		t.Errorf("link is %q, want the enclosure", first.Link)
	}
	if first.GUID != "kf-0001" {
		t.Errorf("guid is %q, want kf-0001", first.GUID)
	}
	if want := time.Date(2026, 2, 2, 10, 0, 0, 0, time.FixedZone("", 3600)); !first.Published.Equal(want) {
		t.Errorf("published is %v, want %v", first.Published, want)
	}

	second := f.Items[1]
	if second.Link != "https://example.invalid/folge-2" {
		t.Errorf("relative link resolved to %q, want it against the feed address", second.Link)
	}
	// A day number without its leading zero is not RFC 822 and is extremely
	// common; refusing it would leave a real feed with no date at all.
	if second.Published.IsZero() {
		t.Error("the sloppy date was not read at all")
	}
}

func TestParseAtom(t *testing.T) {
	f, err := Parse("https://example.invalid/atom.xml", strings.NewReader(atomDoc))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Turmwache" {
		t.Errorf("feed title is %q, want Turmwache", f.Title)
	}
	if len(f.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(f.Items))
	}
	// Taking the first link blindly would stage the feed's own address once per
	// entry.
	if got := f.Items[0].Link; got != "https://example.invalid/runde/1" {
		t.Errorf("link is %q; rel=self was taken instead of the alternate, or the relative href was not resolved", got)
	}
	if got := f.Items[0].GUID; got != "urn:uuid:0001" {
		t.Errorf("guid is %q, want the atom id", got)
	}
	if got := f.Items[1].Link; got != "https://example.invalid/runde/2" {
		t.Errorf("link is %q; a link with no rel at all is the entry's own link", got)
	}
}

// TestParseRSS1 covers the third layout. RSS 1.0 is RDF, so its items sit at
// the root beside the channel rather than inside it, and a parser that only
// looks inside produces zero entries with no error at all: a subscription that
// simply never adds anything and never says why.
func TestParseRSS1(t *testing.T) {
	doc := `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/">
  <channel rdf:about="https://example.invalid/">
    <title>Steinbruch</title>
    <items><rdf:Seq><rdf:li resource="https://example.invalid/a"/></rdf:Seq></items>
  </channel>
  <item rdf:about="https://example.invalid/a">
    <title>Abbau</title>
    <link>https://example.invalid/a</link>
  </item>
</rdf:RDF>`
	f, err := Parse("https://example.invalid/rdf.xml", strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	// The channel's own title still names the feed even though the entries are
	// not inside it.
	if f.Title != "Steinbruch" {
		t.Errorf("feed title is %q, want Steinbruch", f.Title)
	}
	if len(f.Items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(f.Items), f.Items)
	}
	if f.Items[0].Title != "Abbau" || f.Items[0].Link != "https://example.invalid/a" {
		t.Errorf("item is %+v", f.Items[0])
	}
}

// TestParseReadsALatin1Feed covers the failure that is invisible from the
// outside: without a charset reader the whole document is refused and the
// subscription silently produces nothing forever.
func TestParseReadsALatin1Feed(t *testing.T) {
	// The title carries 0xE4 (a-umlaut in ISO-8859-1) and 0x93/0x94, which are
	// the quotes Windows-1252 puts where ISO-8859-1 has control characters.
	doc := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" +
		"<rss version=\"2.0\"><channel><title>K\xe4se</title>" +
		"<item><title>\x93Gr\xfc\xdfe\x94</title><link>https://example.invalid/a</link></item>" +
		"</channel></rss>"
	f, err := Parse("https://example.invalid/rss.xml", strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Käse" {
		t.Errorf("feed title is %q, want Käse", f.Title)
	}
	if len(f.Items) != 1 || f.Items[0].Title != "“Grüße”" {
		t.Errorf("item titles are %+v, want the Windows-1252 quotes read as quotes", f.Items)
	}
}

func TestParseRefusesAnEncodingItCannotRead(t *testing.T) {
	doc := `<?xml version="1.0" encoding="Shift_JIS"?><rss version="2.0"><channel></channel></rss>`
	_, err := Parse("https://example.invalid/rss.xml", strings.NewReader(doc))
	if err == nil {
		t.Fatal("an unreadable encoding was accepted")
	}
	// The label has to be in the message, or the log leaves somebody guessing
	// which encoding was asked for.
	if !strings.Contains(err.Error(), "Shift_JIS") {
		t.Errorf("the error does not name the encoding: %v", err)
	}
}

// TestKeyPrefersTheFeedsOwnIdentifier is the dedupe rule stated as a test: a
// publisher who moves a post to a new address has not published a new post.
func TestKeyPrefersTheFeedsOwnIdentifier(t *testing.T) {
	before := Item{Title: "Folge 1", Link: "https://example.invalid/folge-1", GUID: "kf-0001"}
	moved := Item{Title: "Folge 1 (korrigiert)", Link: "https://example.invalid/2026/folge-1", GUID: "kf-0001"}
	if before.Key() != moved.Key() {
		t.Error("an entry that kept its guid but changed its title and address got a new key; it would be staged twice")
	}

	// And with no guid the link has to carry the identity on its own, or every
	// feed without one would re-stage its whole window on every single poll.
	a := Item{Title: "Folge 2", Link: "https://example.invalid/folge-2"}
	b := Item{Title: "Folge 2 (korrigiert)", Link: "https://example.invalid/folge-2"}
	if a.Key() != b.Key() {
		t.Error("two entries with one link got different keys")
	}
	if a.Key() == before.Key() {
		t.Error("two different entries share a key")
	}

	// The guid and the link are hashed the same way on purpose: a feed that
	// starts publishing as its guid the value it used to publish only as its link
	// must not arrive a second time.
	sameValue := Item{Title: "Folge 2", Link: "https://example.invalid/folge-2", GUID: "https://example.invalid/folge-2"}
	if sameValue.Key() != a.Key() {
		t.Error("the same value read out of guid and out of link produced two keys")
	}
}

func TestSanitize(t *testing.T) {
	four := 4
	got := Sanitize([]Subscription{
		{URL: "  https://example.invalid/rss.xml  ", IntervalMinutes: 0, TitleFilter: " Folge "},
		// The same address again, which a person can reasonably type twice. Two
		// pollers over one feed would share one memory and stage everything twice.
		{URL: "https://example.invalid/rss.xml", IntervalMinutes: 60},
		{URL: "https://example.invalid/atom.xml", IntervalMinutes: -5},
		{URL: "https://example.invalid/slow.xml", IntervalMinutes: 999999},
		{URL: "https://example.invalid/prio.xml", Priority: &four},
		// What an interface's "add a row" button produces before anything is
		// typed into it.
		{URL: "   "},
	})
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(got), got)
	}
	if got[0].URL != "https://example.invalid/rss.xml" || got[0].TitleFilter != "Folge" {
		t.Errorf("row 0 was not trimmed: %+v", got[0])
	}
	if got[0].IntervalMinutes != 0 {
		t.Errorf("row 0 interval is %d; zero has to stay zero, it means the default", got[0].IntervalMinutes)
	}
	if got[0].Every() != DefaultIntervalMinutes*time.Minute {
		t.Errorf("a row with no interval polls every %v", got[0].Every())
	}
	if got[1].IntervalMinutes != MinIntervalMinutes {
		t.Errorf("a negative interval became %d", got[1].IntervalMinutes)
	}
	if got[2].IntervalMinutes != MaxIntervalMinutes {
		t.Errorf("an interval past the ceiling became %d", got[2].IntervalMinutes)
	}
	if got[3].Priority == nil || *got[3].Priority != 3 {
		t.Errorf("a priority outside the queue's range was not clamped: %+v", got[3].Priority)
	}
	if got[3].Priority == &four {
		t.Error("the caller's own pointer was kept, so two tasks would share one priority")
	}
	if Sanitize(nil) != nil {
		t.Error("an empty list has to come back nil, so it reads the same way on disk however it got there")
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		sub  Subscription
		ok   bool
	}{
		{"plain", Subscription{URL: "https://example.invalid/rss.xml"}, true},
		{"with a filter", Subscription{URL: "https://example.invalid/rss.xml", TitleFilter: `^Folge \d+`}, true},
		{"no address", Subscription{}, false},
		// A file:// feed would be a way to read any file on the box through a
		// settings field.
		{"a local file", Subscription{URL: "file:///etc/passwd"}, false},
		{"no host", Subscription{URL: "https://"}, false},
		// The one that must never be dropped and silently run unfiltered.
		{"a filter that will not compile", Subscription{URL: "https://example.invalid/rss.xml", TitleFilter: "("}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sub.Validate()
			if tc.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
