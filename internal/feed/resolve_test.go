package feed

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// testdata/youtube-channel-page.html is the head of
// https://www.youtube.com/@kurzgesagt cut to its title, og:title and canonical
// link; testdata/youtube-channel-feed.xml is that channel's feed cut to two
// entries.

// youtube answers requests the way YouTube does, from the fixtures, and
// records what was asked.
type youtube struct {
	t     *testing.T
	mu    sync.Mutex
	asked []string
	// pageless makes every channel page answer without a canonical link, as
	// a consent page would.
	pageless bool
	// feedDown makes every feed answer 503, as YouTube's feeds sometimes do.
	feedDown bool
}

func (y *youtube) RoundTrip(r *http.Request) (*http.Response, error) {
	y.mu.Lock()
	y.asked = append(y.asked, r.URL.String())
	y.mu.Unlock()
	answer := func(status int, file string) (*http.Response, error) {
		body := "not found"
		if file != "" {
			b, err := os.ReadFile(file)
			if err != nil {
				y.t.Fatal(err)
			}
			body = string(b)
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
	if r.URL.Host != "www.youtube.com" {
		return answer(http.StatusNotFound, "")
	}
	if y.feedDown && r.URL.Path == "/feeds/videos.xml" {
		return answer(http.StatusServiceUnavailable, "")
	}
	switch {
	case r.URL.Path == "/feeds/videos.xml" && r.URL.Query().Get("channel_id") == "UCsXVk37bltHxD1rDPwtNM8Q":
		return answer(http.StatusOK, "testdata/youtube-channel-feed.xml")
	case r.URL.Path == "/feeds/videos.xml" && r.URL.Query().Get("playlist_id") == "PLFs4vir_WsTwEd-nJgVJCZPNL3HALHHpF":
		return answer(http.StatusOK, "testdata/youtube-channel-feed.xml")
	case r.URL.Path == "/@kurzgesagt" || r.URL.Path == "/c/inanutshell" || r.URL.Path == "/user/Kurzgesagt":
		if y.pageless {
			return answer(http.StatusOK, "testdata/mediathek-tagesschau.json")
		}
		return answer(http.StatusOK, "testdata/youtube-channel-page.html")
	}
	return answer(http.StatusNotFound, "")
}

func ytSources(t *testing.T) (Sources, *youtube) {
	y := &youtube{t: t}
	return Sources{HTTP: &http.Client{Transport: y}}, y
}

// isKurzgesagt checks the channel's name around the dash it is spelled with.
func isKurzgesagt(s string) bool {
	return strings.HasPrefix(s, "Kurzgesagt ") && strings.HasSuffix(s, " In a Nutshell")
}

func TestAYouTubeChannelResolvesToItsFeedAndItsListing(t *testing.T) {
	for _, pasted := range []string{
		"https://www.youtube.com/@kurzgesagt",
		"https://youtube.com/@kurzgesagt/videos",
		"https://m.youtube.com/c/inanutshell",
		"https://www.youtube.com/user/Kurzgesagt?app=desktop",
		"https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q",
		"https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/featured",
		"www.youtube.com/@kurzgesagt",
	} {
		src, _ := ytSources(t)
		got, err := Resolve(context.Background(), src, pasted)
		if err != nil {
			t.Errorf("%s: %v", pasted, err)
			continue
		}
		if got.Kind != KindRSS || got.URL != "https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q" || !isKurzgesagt(got.Title) {
			t.Errorf("%s resolved to %+v", pasted, got.Subscription)
		}
		c := got.Channel
		if c == nil || c.Kind != KindChannel || c.URL != "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/videos" || c.Title != got.Title {
			t.Errorf("%s offers no channel listing: %+v", pasted, c)
		}
	}
}

func TestAPlaylistResolvesToItsFeed(t *testing.T) {
	src, _ := ytSources(t)
	got, err := Resolve(context.Background(), src, "https://www.youtube.com/watch?v=4_aOIA-vyBo&list=PLFs4vir_WsTwEd-nJgVJCZPNL3HALHHpF")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://www.youtube.com/feeds/videos.xml?playlist_id=PLFs4vir_WsTwEd-nJgVJCZPNL3HALHHpF" || got.Kind != KindRSS {
		t.Errorf("resolved to %+v", got.Subscription)
	}
	if got.Channel == nil || got.Channel.URL != "https://www.youtube.com/playlist?list=PLFs4vir_WsTwEd-nJgVJCZPNL3HALHHpF" {
		t.Errorf("channel alternative %+v", got.Channel)
	}
}

const listedTitle = "Kurzgesagt, In a Nutshell"

// listing is a Lister that knows one channel, for a page that hides its
// canonical link.
type listing struct{ calls int }

func (l *listing) List(_ context.Context, url string, q ListQuery) (Feed, error) {
	l.calls++
	if !strings.Contains(url, "kurzgesagt") {
		return Feed{}, errors.New("nothing to list")
	}
	return Feed{Title: listedTitle, Link: "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q",
		Items: []Item{{Title: "x", Link: "https://www.youtube.com/watch?v=x"}}}, nil
}

func TestAHandleIsResolvedWithYtdlpWhenThePageDoesNotSay(t *testing.T) {
	src, y := ytSources(t)
	y.pageless = true
	l := &listing{}
	src.Lister = l
	got, err := Resolve(context.Background(), src, "https://www.youtube.com/@kurzgesagt")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q" || l.calls != 1 {
		t.Fatalf("resolved to %+v after %d listings", got.Subscription, l.calls)
	}
}

func TestAMediathekSearchResolvesWithoutAFetch(t *testing.T) {
	src, y := ytSources(t)
	got, err := Resolve(context.Background(), src, "https://mediathekviewweb.de/#query=%21ard%20%23tatort%20%3E80")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindMediathek || got.Mediathek == nil || got.Mediathek.Topic != "tatort" || got.MinMinutes != 80 || got.Title != "tatort" {
		t.Fatalf("resolved to %+v", got.Subscription)
	}
	if len(y.asked) != 0 {
		t.Errorf("resolving a search fetched %v", y.asked)
	}
}

func TestAnyOtherAddressIsTestedAsAFeed(t *testing.T) {
	srv := newServer(t, item(1, "Folge 1"))
	got, err := Resolve(context.Background(), Sources{HTTP: srv.Client()}, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindRSS || got.URL != srv.URL || got.Title != "Kellerfunk" || got.Channel != nil {
		t.Fatalf("resolved to %+v", got)
	}

	srv.answer(http.StatusNotFound)
	if _, err := Resolve(context.Background(), Sources{HTTP: srv.Client()}, srv.URL); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a dead address resolved, or the error does not say why: %v", err)
	}
}

func TestAChannelOnAnotherSiteResolvesToItsListing(t *testing.T) {
	srv := newServer(t)
	srv.answer(http.StatusNotFound)
	l := &listing{}
	got, err := Resolve(context.Background(), Sources{HTTP: srv.Client(), Lister: l}, srv.URL+"/kurzgesagt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindChannel || got.URL != srv.URL+"/kurzgesagt" || got.Title != listedTitle {
		t.Fatalf("resolved to %+v", got)
	}
}

func TestResolveRefusesWhatCannotBeFetched(t *testing.T) {
	for _, raw := range []string{"", "file:///etc/passwd", "ftp://example.invalid/feed"} {
		var refused *AddressError
		if _, err := Resolve(context.Background(), Sources{}, raw); !errors.As(err, &refused) {
			t.Errorf("%q came back as %v, want a refusal of the address", raw, err)
		}
	}
}

func TestAChannelStillResolvesWhileYouTubeWithholdsItsFeed(t *testing.T) {
	src, y := ytSources(t)
	y.feedDown = true
	got, err := Resolve(context.Background(), src, "https://www.youtube.com/@kurzgesagt")
	if err != nil {
		t.Fatalf("a feed answering 503 refused the channel: %v", err)
	}
	if got.URL != "https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q" || !isKurzgesagt(got.Title) {
		t.Errorf("resolved to %+v, want the channel's feed named after its page", got.Subscription)
	}
	if got.Channel == nil || got.Channel.URL != "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q/videos" {
		t.Errorf("the listing is missing: %+v", got.Channel)
	}
}

func TestAChannelYouTubeDoesNotKnowIsRefused(t *testing.T) {
	src, _ := ytSources(t)
	if got, err := Resolve(context.Background(), src, "https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxxxxxx"); err == nil {
		t.Fatalf("an unknown channel resolved to %+v", got.Subscription)
	}
}
