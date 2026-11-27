package dedupe

import "testing"

func TestAMediaKeyIsWrittenAsYtdlpsDownloadArchive(t *testing.T) {
	if got := MediaKey("Youtube", "dQw4w9WgXcQ"); got != "youtube dQw4w9WgXcQ" {
		t.Errorf("MediaKey = %q, want the extractor in lower case and the id as it is", got)
	}
	for _, c := range [][2]string{{"", "dQw4w9WgXcQ"}, {"Youtube", ""}, {"Youtube", " "}} {
		if got := MediaKey(c[0], c[1]); got != "" {
			t.Errorf("MediaKey(%q, %q) = %q, want none without both halves", c[0], c[1], got)
		}
	}
}

func TestEveryFormOfAYouTubeLinkNamesTheSameVideo(t *testing.T) {
	for _, u := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&t=42s&utm_source=feed",
		"https://m.youtube.com/watch?feature=share&v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"http://youtu.be/dQw4w9WgXcQ?si=abc",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ?feature=share",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://WWW.YOUTUBE.COM/watch?v=dQw4w9WgXcQ",
	} {
		if got := MediaKeyOf(u); got != "youtube dQw4w9WgXcQ" {
			t.Errorf("MediaKeyOf(%s) = %q, want youtube dQw4w9WgXcQ", u, got)
		}
	}
}

// A link that also names a playlist can download the whole playlist, so it
// is not one video until a probe says so.
func TestALinkThatNamesNoSingleVideoHasNoKeyOfItsOwn(t *testing.T) {
	for _, u := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789",
		"https://www.youtube.com/playlist?list=PL0123456789",
		"https://www.youtube.com/@channel",
		"https://www.youtube.com/watch?v=tooShort",
		"https://youtu.be/",
		"https://notyoutube.com/watch?v=dQw4w9WgXcQ",
		"https://vimeo.com/123456",
		"not a link",
	} {
		if got := MediaKeyOf(u); got != "" {
			t.Errorf("MediaKeyOf(%s) = %q, want none", u, got)
		}
	}
}
