package torrent

import (
	"slices"
	"strconv"
	"testing"
)

const publicMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Show&tr=udp%3A%2F%2Ftracker.example.org%3A6969%2Fannounce"

func TestValidTrackerTakesTheSchemesATorrentCanAnnounceTo(t *testing.T) {
	for _, ok := range []string{
		"udp://tracker.opentrackr.org:1337/announce",
		"https://tracker.example.org:443/announce",
		"http://tracker.example.org/announce.php",
		"wss://tracker.webtorrent.dev",
	} {
		if !ValidTracker(ok) {
			t.Errorf("ValidTracker(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "tracker.example.org", "ftp://tracker.example.org/announce", "udp://", "# a comment"} {
		if ValidTracker(bad) {
			t.Errorf("ValidTracker(%q) = true", bad)
		}
	}
}

func TestBannedHostReadsANameANameWithAPortAndAWholeAddress(t *testing.T) {
	cases := map[string]string{
		"tracker.example.org":                          "tracker.example.org",
		"  Tracker.Example.ORG  ":                      "tracker.example.org",
		"tracker.example.org:6969":                     "tracker.example.org",
		"udp://tracker.example.org:6969/announce":      "tracker.example.org",
		"https://tracker.example.org/a/0123abcd/annou": "tracker.example.org",
		"*.example.org":                                "example.org",
		"":                                             "",
		"   ":                                          "",
	}
	for line, want := range cases {
		if got := BannedHost(line); got != want {
			t.Errorf("BannedHost(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestABannedHostCoversItsSubdomainsButNotALookalike(t *testing.T) {
	banned := []string{"example.org"}
	if host, ok := Banned([]string{"http://open.tracker.dev/announce", "udp://tracker.example.org:6969/announce"}, banned); !ok || host != "tracker.example.org" {
		t.Fatalf("Banned = %q, %v, want the subdomain named", host, ok)
	}
	if host, ok := Banned([]string{"udp://notexample.org:6969/announce"}, banned); ok {
		t.Fatalf("notexample.org was taken for a subdomain of example.org (%q)", host)
	}
	if _, ok := Banned([]string{"udp://tracker.example.org:6969/announce"}, nil); ok {
		t.Fatal("an empty ban list banned something")
	}
}

func TestExtraTrackersGoToAPublicMagnetWithoutWhatItHasOrWhatIsBanned(t *testing.T) {
	got := ExtraTrackers(publicMagnet, []string{
		"udp://tracker.opentrackr.org:1337/announce",
		"udp://tracker.example.org:6969/announce", // the magnet names it already
		"udp://tracker.opentrackr.org:1337/announce",
		"not a tracker",
		"http://tracker.banned.example/announce",
		"  https://tracker.gbitt.info:443/announce  ",
	}, []string{"banned.example"})
	want := []string{"udp://tracker.opentrackr.org:1337/announce", "https://tracker.gbitt.info:443/announce"}
	if !slices.Equal(got, want) {
		t.Fatalf("ExtraTrackers = %q, want %q", got, want)
	}
}

func TestAPrivateTorrentIsGivenNoExtraTrackers(t *testing.T) {
	extra := []string{"udp://tracker.opentrackr.org:1337/announce"}
	private := EncodeBytes(multiFile(t, "Private", []fileInfo{file(1<<20, "a.mkv")}, true))
	if got := ExtraTrackers(private, extra, nil); got != nil {
		t.Fatalf("a private .torrent was given %q", got)
	}
	public := EncodeBytes(multiFile(t, "Public", []fileInfo{file(1<<20, "a.mkv")}, false))
	if got := ExtraTrackers(public, extra, nil); !slices.Equal(got, extra) {
		t.Fatalf("a public .torrent was given %q, want %q", got, extra)
	}
}

// A magnet says nothing about privacy before its metadata arrives, so the
// passkey in its own tracker has to speak for it.
func TestAMagnetWhoseTrackerCarriesAPasskeyIsGivenNoExtraTrackers(t *testing.T) {
	extra := []string{"udp://tracker.opentrackr.org:1337/announce"}
	for _, tr := range []string{
		"https%3A%2F%2Ftracker.private.example%2Fa%2F0123456789abcdef0123456789abcdef%2Fannounce",
		"https%3A%2F%2Fprivate.example%2Fannounce.php%3Fpasskey%3Dsecret",
		"http%3A%2F%2Fprivate.example%3A34000%2F9f8e7d6c5b4a39281706f5e4d3c2b1a0%2Fannounce",
	} {
		magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=" + tr
		if got := ExtraTrackers(magnet, extra, nil); got != nil {
			t.Errorf("the magnet with tracker %s was given %q", tr, got)
		}
	}
}

// The default has no extra trackers, and an uploaded torrent is not parsed a
// second time on every start for nothing.
func TestWithNoExtraTrackersTheTorrentIsNotReadAgain(t *testing.T) {
	files := make([]fileInfo, 2000)
	for i := range files {
		files[i] = file(1<<20, "Season", "episode"+strconv.Itoa(i)+".mkv")
	}
	uri := EncodeBytes(multiFile(t, "Big", files, false))
	if n := testing.AllocsPerRun(3, func() { ExtraTrackers(uri, nil, []string{"banned.example"}) }); n != 0 {
		t.Fatalf("ExtraTrackers with nothing to add allocated %v times per call, want no parse at all", n)
	}
}

func TestExtraTrackersForSomethingThatIsNoTorrentAreNone(t *testing.T) {
	if got := ExtraTrackers("https://example.org/file.bin", []string{"udp://tracker.opentrackr.org:1337/announce"}, nil); got != nil {
		t.Fatalf("a plain download was given %q", got)
	}
}
