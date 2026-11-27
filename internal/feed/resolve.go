package feed

// Turning whatever somebody pasted into a subscription row: a YouTube channel
// or playlist page into its feed, a MediathekViewWeb search into a search
// row, and anything else into a feed once a test fetch shows it is one.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Resolved is a subscription ready to save, plus the channel listing of the
// same channel, which also reaches its older videos.
type Resolved struct {
	Subscription
	Channel *Subscription `json:"channel,omitempty"`
}

// AddressError is a pasted address this process will not fetch at all, as
// opposed to one where nothing usable was found.
type AddressError struct{ Err error }

func (e *AddressError) Error() string { return e.Err.Error() }
func (e *AddressError) Unwrap() error { return e.Err }

// maxChannelPage caps what is read of a YouTube channel page. Its canonical
// link sits about a megabyte in.
const maxChannelPage = 4 << 20

var (
	channelID     = regexp.MustCompile(`^UC[0-9A-Za-z_-]{22}$`)
	canonicalLink = regexp.MustCompile(`<link rel="canonical" href="https://www\.youtube\.com/channel/(UC[0-9A-Za-z_-]{22})"`)
	ogTitle       = regexp.MustCompile(`<meta property="og:title" content="([^"]*)"`)
)

// Resolve works out the subscription for a pasted address. A YouTube handle
// needs no API key: the channel page names its id in its canonical link, and
// yt-dlp can tell it where the page does not.
func Resolve(ctx context.Context, src Sources, raw string) (Resolved, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if s, ok := ParseMediathekURL(raw); ok {
		return Resolved{Subscription: s}, nil
	}
	sub := Subscription{URL: raw, Kind: KindRSS}
	if err := sub.Validate(); err != nil {
		return Resolved{}, &AddressError{err}
	}
	src = src.withDefaults()
	u, _ := url.Parse(raw)
	if isYouTube(u) {
		return resolveYouTube(ctx, src, u)
	}
	f, err := fetch(ctx, src.HTTP, raw)
	if err == nil {
		sub.Title = f.Title
		return Resolved{Subscription: sub}, nil
	}
	if src.Lister != nil {
		if l, lerr := src.Lister.List(ctx, raw, ListQuery{Limit: 1}); lerr == nil && len(l.Items) > 0 {
			return Resolved{Subscription: Subscription{URL: raw, Kind: KindChannel, Title: l.Title}}, nil
		}
	}
	return Resolved{}, fmt.Errorf("no feed was found at %s: %w", raw, err)
}

func isYouTube(u *url.URL) bool {
	switch strings.ToLower(u.Hostname()) {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		return true
	}
	return false
}

func resolveYouTube(ctx context.Context, src Sources, u *url.URL) (Resolved, error) {
	if list := u.Query().Get("list"); list != "" {
		return youTubeRows(ctx, src,
			"https://www.youtube.com/feeds/videos.xml?playlist_id="+url.QueryEscape(list),
			"https://www.youtube.com/playlist?list="+url.QueryEscape(list), "")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id, title string
	switch {
	case len(parts) >= 2 && parts[0] == "channel" && channelID.MatchString(parts[1]):
		id = parts[1]
	case len(parts) >= 1 && strings.HasPrefix(parts[0], "@"),
		len(parts) >= 2 && (parts[0] == "c" || parts[0] == "user"):
		n := 2
		if strings.HasPrefix(parts[0], "@") {
			n = 1
		}
		page := "https://www.youtube.com/" + strings.Join(parts[:n], "/")
		id, title = channelPage(ctx, src.HTTP, page)
		if id == "" && src.Lister != nil {
			if l, err := src.Lister.List(ctx, page, ListQuery{Limit: 1}); err == nil {
				id, _ = strings.CutPrefix(l.Link, "https://www.youtube.com/channel/")
				title = l.Title
			}
		}
		if !channelID.MatchString(id) {
			return Resolved{}, fmt.Errorf("the channel behind %s could not be found; check the address, or paste the channel page from YouTube", u)
		}
	default:
		return Resolved{}, fmt.Errorf("%s is not a channel or a playlist on YouTube", u)
	}
	return youTubeRows(ctx, src,
		"https://www.youtube.com/feeds/videos.xml?channel_id="+id,
		"https://www.youtube.com/channel/"+id+"/videos", title)
}

// youTubeRows builds the two rows for one channel or playlist and names them
// after the feed. Only a 404 refuses them: YouTube's feeds also fail for a
// while on channels that exist, and the address follows a fixed pattern, so
// the subscription is offered anyway and its polls try again.
func youTubeRows(ctx context.Context, src Sources, feedURL, listURL, title string) (Resolved, error) {
	f, err := fetch(ctx, src.HTTP, feedURL)
	var status *statusError
	switch {
	case err == nil:
		if f.Title != "" {
			title = f.Title
		}
	case errors.As(err, &status) && status.code == http.StatusNotFound:
		return Resolved{}, fmt.Errorf("YouTube knows no feed for %s: %w", listURL, err)
	case title == "" && src.Lister != nil:
		if l, lerr := src.Lister.List(ctx, listURL, ListQuery{Limit: 1}); lerr == nil {
			title = l.Title
		}
	}
	return Resolved{
		Subscription: Subscription{URL: feedURL, Kind: KindRSS, Title: title},
		Channel:      &Subscription{URL: listURL, Kind: KindChannel, Title: title},
	}, nil
}

// channelPage reads a channel's id and name off its page, or returns "" for
// a page that does not carry them.
func channelPage(ctx context.Context, hc *http.Client, page string) (id, title string) {
	req, err := http.NewRequestWithContext(httpx.Confine(ctx, httpx.ScopePublic), http.MethodGet, page, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Accept-Language", "en")
	// Without it a visitor from the EU is sent to a consent page that names no
	// channel.
	req.Header.Set("Cookie", "SOCS=CAI")
	resp, err := hc.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChannelPage))
	if err != nil && !errors.Is(err, io.EOF) {
		return "", ""
	}
	if m := canonicalLink.FindSubmatch(body); m != nil {
		id = string(m[1])
	}
	if m := ogTitle.FindSubmatch(body); m != nil {
		title = text(html.UnescapeString(string(m[1])))
	}
	return id, title
}
