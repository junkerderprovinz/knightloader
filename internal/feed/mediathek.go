package feed

// The mediathek kind: a saved search on MediathekViewWeb, which indexes the
// public broadcasters' media libraries and answers with the files themselves,
// so nothing has to scrape a broadcaster's page.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// MediathekAPI is MediathekViewWeb's search endpoint.
const MediathekAPI = "https://mediathekviewweb.de/api/query"

// mediathekSite is where a search is shown, and the address a search row is
// known by.
const mediathekSite = "https://mediathekviewweb.de/#query="

// mediathekWindow is how many results one poll asks for, newest first. A
// show rarely publishes more between two polls, and the first poll seeds
// with all of them.
const mediathekWindow = 50

// MediathekURL is the address of a search on MediathekViewWeb in the site's
// own syntax: !channel, #topic, +title, >minimum and <maximum minutes.
func MediathekURL(m Mediathek, minMinutes, maxMinutes int) string {
	var words []string
	for _, f := range []struct {
		mark  string
		value string
	}{{"!", m.Channel}, {"#", m.Topic}, {"+", m.Title}} {
		for _, w := range strings.Fields(f.value) {
			words = append(words, f.mark+w)
		}
	}
	if minMinutes > 0 {
		words = append(words, fmt.Sprintf(">%d", minMinutes))
	}
	if maxMinutes > 0 {
		words = append(words, fmt.Sprintf("<%d", maxMinutes))
	}
	// QueryEscape keeps a literal "+" from reading back as a space; the site
	// writes spaces as %20.
	return mediathekSite + strings.ReplaceAll(url.QueryEscape(strings.Join(words, " ")), "+", "%20")
}

// ParseMediathekURL reads a search address copied from MediathekViewWeb into
// a subscription row. Words without a mark go to the topic, where a show's
// name sits, or to the title when a topic is given as well. The row's address
// is rebuilt from what was read, so the same search pasted twice is one
// subscription.
func ParseMediathekURL(raw string) (Subscription, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Subscription{}, false
	}
	if h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."); h != "mediathekviewweb.de" {
		return Subscription{}, false
	}
	frag, err := url.ParseQuery(u.EscapedFragment())
	if err != nil {
		return Subscription{}, false
	}
	var channel, topic, title, plain []string
	minMinutes, maxMinutes := 0, 0
	for _, w := range strings.Fields(frag.Get("query")) {
		body := w[1:]
		switch w[0] {
		case '!':
			channel = append(channel, body)
		case '#':
			topic = append(topic, body)
		case '+':
			title = append(title, body)
		case '>':
			fmt.Sscanf(body, "%d", &minMinutes)
		case '<':
			fmt.Sscanf(body, "%d", &maxMinutes)
		case '*':
			// A description search has no field in a row; the other words
			// still narrow the search.
		default:
			plain = append(plain, w)
		}
	}
	if len(topic) == 0 {
		topic, plain = plain, nil
	}
	title = append(title, plain...)
	m := Mediathek{
		Channel: strings.Join(channel, " "),
		Topic:   strings.Join(topic, " "),
		Title:   strings.Join(title, " "),
		Quality: QualityHD,
	}
	if m.Channel == "" && m.Topic == "" && m.Title == "" {
		return Subscription{}, false
	}
	minMinutes, maxMinutes = max(minMinutes, 0), max(maxMinutes, 0)
	return Subscription{
		URL:        MediathekURL(m, minMinutes, maxMinutes),
		Kind:       KindMediathek,
		Title:      mediathekTitle(m),
		MinMinutes: minMinutes,
		MaxMinutes: maxMinutes,
		Mediathek:  &m,
	}, true
}

// mediathekTitle names a search after the show, else the title words, else
// the channel.
func mediathekTitle(m Mediathek) string {
	for _, s := range []string{m.Topic, m.Title, m.Channel} {
		if s != "" {
			return s
		}
	}
	return ""
}

// mediathekQuery is the body the API takes.
type mediathekQuery struct {
	Queries     []mediathekField `json:"queries"`
	SortBy      string           `json:"sortBy"`
	SortOrder   string           `json:"sortOrder"`
	Future      bool             `json:"future"`
	Offset      int              `json:"offset"`
	Size        int              `json:"size"`
	DurationMin int              `json:"duration_min,omitempty"`
	DurationMax int              `json:"duration_max,omitempty"`
}

type mediathekField struct {
	Fields []string `json:"fields"`
	Query  string   `json:"query"`
}

type mediathekResult struct {
	Channel     string          `json:"channel"`
	Topic       string          `json:"topic"`
	Title       string          `json:"title"`
	Timestamp   int64           `json:"timestamp"`
	Duration    json.RawMessage `json:"duration"`
	URLVideo    string          `json:"url_video"`
	URLVideoLow string          `json:"url_video_low"`
	URLVideoHD  string          `json:"url_video_hd"`
	URLSubtitle string          `json:"url_subtitle"`
	ID          string          `json:"id"`
}

// file is the result's video in quality q, or the next one down, or failing
// that the next one up, so a result is never dropped for lacking one size.
func (r mediathekResult) file(q Quality) string {
	order := []string{r.URLVideoHD, r.URLVideo, r.URLVideoLow}
	switch q {
	case QualityNormal:
		order = []string{r.URLVideo, r.URLVideoLow, r.URLVideoHD}
	case QualityLow:
		order = []string{r.URLVideoLow, r.URLVideo, r.URLVideoHD}
	}
	for _, u := range order {
		if u = strings.TrimSpace(u); u != "" {
			return u
		}
	}
	return ""
}

// seconds reads the duration, which the index stores as a number or, for
// some entries, as an empty string.
func (r mediathekResult) seconds() time.Duration {
	var n float64
	if json.Unmarshal(r.Duration, &n) == nil && n > 0 {
		return time.Duration(n * float64(time.Second))
	}
	return 0
}

// queryMediathek runs a subscription's search once and returns the results as
// a feed, newest first.
func queryMediathek(ctx context.Context, hc *http.Client, api string, s Subscription, size int) (Feed, error) {
	m := s.Mediathek
	if m == nil {
		return Feed{}, fmt.Errorf("this subscription has no Mediathek search")
	}
	q := mediathekQuery{SortBy: "timestamp", SortOrder: "desc", Size: size,
		DurationMin: s.MinMinutes * 60, DurationMax: s.MaxMinutes * 60}
	for _, f := range []struct{ field, value string }{{"channel", m.Channel}, {"topic", m.Topic}, {"title", m.Title}} {
		if f.value != "" {
			q.Queries = append(q.Queries, mediathekField{Fields: []string{f.field}, Query: f.value})
		}
	}
	body, err := json.Marshal(q)
	if err != nil {
		return Feed{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(body))
	if err != nil {
		return Feed{}, err
	}
	req = req.WithContext(httpx.Confine(ctx, httpx.ScopeOfHost(ctx, req.URL.Hostname())))
	// The API reads its JSON body only when it is sent as plain text.
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return Feed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Feed{}, fmt.Errorf("MediathekViewWeb answered %s", resp.Status)
	}
	var doc struct {
		Err    []string `json:"err"`
		Result *struct {
			Results []mediathekResult `json:"results"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&doc); err != nil {
		return Feed{}, fmt.Errorf("MediathekViewWeb sent an answer that could not be read: %w", err)
	}
	if len(doc.Err) > 0 {
		return Feed{}, fmt.Errorf("MediathekViewWeb could not run the search: %s", strings.Join(doc.Err, "; "))
	}
	if doc.Result == nil {
		return Feed{}, fmt.Errorf("MediathekViewWeb sent no results")
	}
	out := Feed{Title: mediathekTitle(*m)}
	for _, r := range doc.Result.Results {
		link := r.file(m.Quality)
		if link == "" || r.ID == "" {
			continue
		}
		title := text(r.Title)
		it := Item{
			Title:    title,
			Link:     link,
			GUID:     r.ID,
			Duration: r.seconds(),
			FileName: title + ".mp4",
		}
		if r.Timestamp > 0 {
			it.Published = time.Unix(r.Timestamp, 0)
		}
		if m.Subtitles {
			it.Subtitle = strings.TrimSpace(r.URLSubtitle)
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}
