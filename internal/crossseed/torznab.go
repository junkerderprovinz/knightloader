package crossseed

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// sizeSlack is how far a search result's size may be off the files' before
// its .torrent is not even fetched. Indexers round sizes, and the files are
// compared exactly once the .torrent is here.
const sizeSlack = 0.02

const maxTorrentBytes = 16 << 20

// FileEntry is one file of a torrent: its path, starting with the torrent's
// folder for a torrent of several files, and its size.
type FileEntry struct {
	Path string
	Size int64
}

// FilesOf reads a .torrent's name and files.
func FilesOf(torrent []byte) (string, []FileEntry, error) {
	raw, err := bencode.Raw(torrent, "info")
	if err != nil {
		return "", nil, err
	}
	v, err := bencode.Decode(raw)
	if err != nil {
		return "", nil, err
	}
	info, _ := v.(map[string]any)
	nameBytes, _ := info["name"].([]byte)
	name := string(nameBytes)
	if !cleanPart(name) {
		return "", nil, fmt.Errorf("the torrent's name %q is not a plain file name", name)
	}
	if n, ok := info["length"].(int64); ok {
		return name, []FileEntry{{name, n}}, nil
	}
	list, ok := info["files"].([]any)
	if !ok {
		return "", nil, errors.New("the torrent lists no files")
	}
	out := make([]FileEntry, 0, len(list))
	for _, f := range list {
		d, _ := f.(map[string]any)
		n, ok := d["length"].(int64)
		parts, _ := d["path"].([]any)
		if !ok || len(parts) == 0 {
			return "", nil, errors.New("a file of the torrent has no length or path")
		}
		p := []string{name}
		for _, part := range parts {
			b, _ := part.([]byte)
			if !cleanPart(string(b)) {
				return "", nil, fmt.Errorf("the torrent's path %q is not a plain path", b)
			}
			p = append(p, string(b))
		}
		out = append(out, FileEntry{strings.Join(p, "/"), n})
	}
	return name, out, nil
}

func cleanPart(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}

// SameFiles reports whether the torrent has exactly the files local has, by
// path and size, which is the only case in which its pieces are the same.
func SameFiles(local []FileEntry, torrent []byte) (bool, error) {
	_, theirs, err := FilesOf(torrent)
	if err != nil {
		return false, err
	}
	if len(theirs) != len(local) {
		return false, nil
	}
	sort := func(a, b FileEntry) int { return strings.Compare(a.Path, b.Path) }
	a, b := slices.SortedFunc(slices.Values(local), sort), slices.SortedFunc(slices.Values(theirs), sort)
	return slices.Equal(a, b), nil
}

// Torznab searches one Torznab feed, a Prowlarr or Jackett indexer.
type Torznab struct {
	name, api, key string
	http           *http.Client
}

// NewTorznab is a client for the Torznab endpoint at api, such as
// http://prowlarr:9696/1/api, with its API key.
func NewTorznab(name, api, key string) *Torznab {
	return &Torznab{name: name, api: api, key: key, http: httpx.New(httpx.Options{Timeout: time.Minute})}
}

// Result is one search result.
type Result struct {
	Title string
	Link  string
	Size  int64
}

// TorznabMatch is a result whose files are local's, with its .torrent.
type TorznabMatch struct {
	Result
	Indexer string
	Torrent []byte
}

// Find searches for name and returns the first result with exactly local's
// files, or nil. Results whose name or size differ are passed over without
// fetching their .torrent.
func (t *Torznab) Find(ctx context.Context, name string, local []FileEntry) (*TorznabMatch, error) {
	results, err := t.Search(ctx, name)
	if err != nil {
		return nil, err
	}
	var total int64
	for _, f := range local {
		total += f.Size
	}
	for _, r := range results {
		if !sameName(r.Title, name) || !nearSize(r.Size, total) {
			continue
		}
		b, err := t.fetch(ctx, r.Link)
		if err != nil {
			continue
		}
		if ok, err := SameFiles(local, b); err == nil && ok {
			return &TorznabMatch{Result: r, Indexer: t.name, Torrent: b}, nil
		}
	}
	return nil, nil
}

// Search asks the feed for query.
func (t *Torznab) Search(ctx context.Context, query string) ([]Result, error) {
	u, err := url.Parse(t.api)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("t", "search")
	q.Set("q", query)
	q.Set("apikey", t.key)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s could not be reached: %w", t.name, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var refusal struct {
		XMLName     xml.Name `xml:"error"`
		Description string   `xml:"description,attr"`
	}
	if xml.Unmarshal(body, &refusal) == nil {
		return nil, fmt.Errorf("%s refused the search: %s", t.name, refusal.Description)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered the search with %s", t.name, resp.Status)
	}
	var feed struct {
		Items []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			Size  int64  `xml:"size"`
			Attrs []struct {
				Name  string `xml:"name,attr"`
				Value string `xml:"value,attr"`
			} `xml:"attr"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("%s answered with something that is not a Torznab feed: %w", t.name, err)
	}
	out := make([]Result, 0, len(feed.Items))
	for _, it := range feed.Items {
		r := Result{Title: it.Title, Link: it.Link, Size: it.Size}
		for _, a := range it.Attrs {
			if a.Name == "size" {
				if n, err := strconv.ParseInt(a.Value, 10, 64); err == nil {
					r.Size = n
				}
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (t *Torznab) fetch(ctx context.Context, link string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s for a .torrent", t.name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxTorrentBytes {
		return nil, errors.New("the .torrent is larger than any real one")
	}
	return b, nil
}

// sameName compares two release names by their letters and digits alone, so
// dots, spaces and underscores between the words do not count.
func sameName(a, b string) bool {
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, s)
	}
	return norm(a) == norm(b)
}

func nearSize(got, want int64) bool {
	if want == 0 {
		return got == 0
	}
	d := float64(got-want) / float64(want)
	return d >= -sizeSlack && d <= sizeSlack
}
