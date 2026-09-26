package ytdlp

// Some sites play their videos in a player of their own that yt-dlp has no
// extractor for, although the stream behind it is plain HLS that yt-dlp
// fetches well. For those sites the backend asks the site for the stream's
// address first and hands yt-dlp that address with a file name, since yt-dlp
// would name the bare stream after its playlist file ("master").

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// player is one site whose embed links are turned into a stream this way.
type player struct {
	// code picks the video's code out of an embed link, and answers "" for
	// any other page of the site, which yt-dlp gets as it is.
	code func(u *url.URL) string
	// stream asks the site for the stream behind code. page is the embed
	// link.
	stream func(ctx context.Context, c *http.Client, page *url.URL, code string) (embedded, error)
}

// players are the sites handled this way, by host.
var players = map[string]player{
	"playmate.to": {code: embedCode, stream: playmateStream},
}

// embedded is the stream behind an embed link.
type embedded struct {
	url string
	// title is the name the file is saved under, without its extension.
	title string
	// referer goes with every request yt-dlp makes for the stream, as the
	// site's own player sends it.
	referer string
}

// errEmbedGone is a site saying that no video goes by the code any more.
var errEmbedGone = errors.New("the site no longer has this video")

// errEmbedChanged is an answer that does not hold what the player script
// reads from it.
var errEmbedChanged = errors.New("the player API answered without a stream address, so the site has probably changed")

// embedTimeout bounds everything asked of the site for one link.
const embedTimeout = 30 * time.Second

// embedUserAgent is sent to the sites, some of which refuse a client that does
// not introduce itself the way a browser does.
const embedUserAgent = "Mozilla/5.0 (compatible; KnightLoader; +https://github.com/junkerderprovinz/knightloader)"

var embedClient = httpx.New(httpx.Options{Timeout: embedTimeout})

func (b *Backend) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return embedClient
}

// unwrap returns the stream behind rawurl when rawurl is an embed link of one
// of the players. ok is false for every other link, and err, prefixed with
// the site, says why a player's link led to no stream.
func unwrap(ctx context.Context, c *http.Client, rawurl string) (s embedded, ok bool, err error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return embedded{}, false, nil
	}
	site := bareHost(u.Hostname())
	p, known := players[site]
	if !known {
		return embedded{}, false, nil
	}
	code := p.code(u)
	if code == "" {
		return embedded{}, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()
	s, err = p.stream(ctx, c, u, code)
	if err != nil {
		return embedded{}, true, fmt.Errorf("%s: %w", site, err)
	}
	return s, true, nil
}

var embedPath = regexp.MustCompile(`^/embed/([A-Za-z0-9_-]+)/?$`)

// embedCode reads the code out of an /embed/<code> path.
func embedCode(u *url.URL) string {
	if m := embedPath.FindStringSubmatch(u.Path); m != nil {
		return m[1]
	}
	return ""
}

// playmateStream asks playmate.to for the stream the way its player script
// does: a POST to /api/s with the code, answered with the address of an HLS
// master playlist in "sx". The title comes from the embed page, which also
// answers 404 for a code that does not exist.
func playmateStream(ctx context.Context, c *http.Client, page *url.URL, code string) (embedded, error) {
	title, err := pageTitle(ctx, c, page)
	if err != nil {
		return embedded{}, err
	}
	origin := page.Scheme + "://" + page.Host
	body, _ := json.Marshal(map[string]string{"c": code, "d": "desktop"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/s", bytes.NewReader(body))
	if err != nil {
		return embedded{}, err
	}
	req.Header.Set("User-Agent", embedUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", page.String())
	resp, err := c.Do(req)
	if err != nil {
		return embedded{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return embedded{}, err
	}
	var ans struct {
		Stream string `json:"sx"`
		Error  string `json:"error"`
	}
	readable := json.Unmarshal(raw, &ans) == nil
	switch {
	case resp.StatusCode == http.StatusNotFound && readable && ans.Error != "":
		return embedded{}, errEmbedGone
	case resp.StatusCode != http.StatusOK:
		// The number alone: the site's own word for it ("forbidden") would
		// file the failure as a missing login, which it is not.
		return embedded{}, fmt.Errorf("the player API refused the request (%d)", resp.StatusCode)
	case !readable || !webAddress(ans.Stream):
		return embedded{}, errEmbedChanged
	}
	return embedded{url: ans.Stream, title: fileTitle(title, code, page.Hostname()), referer: origin + "/"}, nil
}

var titleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// pageTitle fetches the embed page and returns its title, "" when it has none.
func pageTitle(ctx context.Context, c *http.Client, page *url.URL) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", embedUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return "", errEmbedGone
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("the embed page failed to load (%d)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	m := titleTag.FindSubmatch(body)
	if m == nil {
		return "", nil
	}
	return strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " "), nil
}

// webAddress reports whether s is an absolute http(s) address.
func webAddress(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// maxTitleRunes keeps the name well inside a file name's limit and the
// template inside Options.OutputTemplate's.
const maxTitleRunes = 100

// fileTitle is the name the stream is saved under: the page's title when it
// names the video, else the code. A title that is only the site's name or the
// code again names nothing.
func fileTitle(title, code, host string) string {
	t := safeName(title)
	site := bareHost(host)
	name, _, _ := strings.Cut(site, ".")
	if t == "" || strings.EqualFold(t, code) || strings.EqualFold(t, site) || strings.EqualFold(t, name) {
		return code
	}
	return t
}

// safeName makes a title usable as a file name on every system the file can
// end up on, by dropping the characters one of them reserves.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxTitleRunes {
		s = strings.TrimSpace(string(r[:maxTitleRunes]))
	}
	// A trailing dot would run into the extension's.
	return strings.TrimRight(s, ". ")
}

// template is the -o template that saves the stream under its title. yt-dlp
// reads a % in it as the start of a field.
func (s embedded) template() string {
	return strings.ReplaceAll(s.title, "%", "%%") + ".%(ext)s"
}

// args are the flags that belong to the stream, for yt-dlp's command line.
func (s embedded) args() []string {
	return []string{"--add-header", "Referer:" + s.referer}
}
