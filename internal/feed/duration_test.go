package feed

import (
	"strings"
	"testing"
	"time"
)

func TestParseReadsTheLengthAFeedAnnounces(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:media="http://search.yahoo.com/mrss/" xmlns:yt="http://gdata.youtube.com/schemas/2007">
<channel><title>Kellerfunk</title>
<item><title>hms</title><guid>1</guid><enclosure url="https://example.invalid/1.mp3"/><itunes:duration>1:02:03</itunes:duration></item>
<item><title>ms</title><guid>2</guid><enclosure url="https://example.invalid/2.mp3"/><itunes:duration>45:10</itunes:duration></item>
<item><title>seconds</title><guid>3</guid><enclosure url="https://example.invalid/3.mp3"/><itunes:duration>3600</itunes:duration></item>
<item><title>media content</title><guid>4</guid><link>https://example.invalid/4</link><media:content url="https://example.invalid/4.mp4" duration="125"/></item>
<item><title>media group</title><guid>5</guid><link>https://example.invalid/5</link><media:group><media:content url="https://example.invalid/5.mp4" duration="61.5"/></media:group></item>
<item><title>yt</title><guid>6</guid><link>https://example.invalid/6</link><media:group><yt:duration seconds="300"/></media:group></item>
<item><title>none</title><guid>7</guid><link>https://example.invalid/7</link></item>
<item><title>nonsense</title><guid>8</guid><link>https://example.invalid/8</link><itunes:duration>about an hour</itunes:duration></item>
</channel></rss>`
	f, err := Parse("https://example.invalid/rss.xml", strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{
		"hms":           time.Hour + 2*time.Minute + 3*time.Second,
		"ms":            45*time.Minute + 10*time.Second,
		"seconds":       time.Hour,
		"media content": 125 * time.Second,
		"media group":   61500 * time.Millisecond,
		"yt":            5 * time.Minute,
		"none":          0,
		"nonsense":      0,
	}
	if len(f.Items) != len(want) {
		t.Fatalf("got %d entries", len(f.Items))
	}
	for _, it := range f.Items {
		if it.Duration != want[it.Title] {
			t.Errorf("%s: %v, want %v", it.Title, it.Duration, want[it.Title])
		}
	}
}

// TestAnAtomContentIsNotALength guards the namespaces: Atom's own <content>
// shares its local name with media:content.
func TestAnAtomContentIsNotALength(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>A</title>
<entry><title>e</title><id>1</id><link href="https://example.invalid/1"/><content type="html" duration="99">text</content></entry></feed>`
	f, err := Parse("https://example.invalid/atom.xml", strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 1 || f.Items[0].Duration != 0 {
		t.Fatalf("got %+v", f.Items)
	}
}
