package remotefs

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// WebDAV needs no library here: a download is a plain HTTP GET that the engine
// fetches (see Resolver.Resolve), so only the PROPFIND listing is written
// here, on the app's own httpx client and its outbound policy.

// davTimeout bounds one PROPFIND; listing thousands of entries on a busy
// Nextcloud is slow.
const davTimeout = 45 * time.Second

// propfindBody asks for exactly the three properties this package reads;
// allprop would bring kilobytes of custom properties per entry.
const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:"><D:prop>
<D:resourcetype/><D:getcontentlength/><D:displayname/>
</D:prop></D:propfind>`

type webdavFS struct {
	t     Target
	hc    *http.Client
	login Login
}

func (d Dialer) dialWebDAV(_ context.Context, t Target, login Login) (FS, error) {
	hc := d.HTTPClient
	if hc == nil {
		// No client timeout because Open streams whole files through it;
		// propfind bounds itself with a context.
		hc = httpx.New(httpx.Options{Timeout: httpx.NoTimeout})
	}
	// HTTP has no session to establish, so nothing is dialled until the first
	// request.
	return &webdavFS{t: t, hc: hc, login: login}, nil
}

func (w *webdavFS) Close() error { return nil }

// url builds the http(s) URL for one server path on this target's server.
func (w *webdavFS) url(p string) string {
	t := w.t
	t.Path = p
	return t.HTTPURL()
}

func (w *webdavFS) do(ctx context.Context, method, p string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, w.url(p), body)
	if err != nil {
		return nil, err
	}
	if w.login.Username != "" {
		req.SetBasicAuth(w.login.Username, w.login.Password)
	}
	return req, nil
}

func (w *webdavFS) Stat(ctx context.Context, p string) (Entry, error) {
	// Depth 0 asks for this resource only, not its children.
	found, err := w.propfind(ctx, p, "0")
	if err != nil {
		return Entry{}, err
	}
	for _, e := range found {
		if samePath(e.path, p) {
			return e.Entry, nil
		}
	}
	// A single entry under another path is still the answer to Depth 0.
	if len(found) == 1 {
		return found[0].Entry, nil
	}
	return Entry{}, fmt.Errorf("remotefs: webdav %s: %w", p, ErrNotFound)
}

func (w *webdavFS) List(ctx context.Context, p string) ([]Entry, error) {
	found, err := w.propfind(ctx, p, "1")
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(found))
	for _, e := range found {
		// Depth 1 includes the collection itself.
		if samePath(e.path, p) {
			continue
		}
		out = append(out, e.Entry)
	}
	return out, nil
}

// Open issues a ranged GET. A server may ignore Range and send the whole file
// with 200, so a resumed read requires 206 rather than appending the full
// file to the part file.
func (w *webdavFS) Open(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	req, err := w.do(ctx, http.MethodGet, p, nil)
	if err != nil {
		return nil, fmt.Errorf("remotefs: webdav %s: %w", p, err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, DialError("webdav", w.t.Addr(), err)
	}
	if err := davStatus(p, resp.StatusCode); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	if offset > 0 && resp.StatusCode != http.StatusPartialContent {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("remotefs: webdav %s: this server ignores byte ranges, so a paused download cannot be continued; start it again from the beginning", p)
	}
	return resp.Body, nil
}

// davEntry is one <response> element: what it describes, and the path it
// describes it for.
type davEntry struct {
	Entry
	path string
}

func (w *webdavFS) propfind(ctx context.Context, p, depth string) ([]davEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, davTimeout)
	defer cancel()
	req, err := w.do(ctx, "PROPFIND", p, strings.NewReader(propfindBody))
	if err != nil {
		return nil, fmt.Errorf("remotefs: webdav %s: %w", p, err)
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, DialError("webdav", w.t.Addr(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		// Most likely an account stored for an ordinary web server.
		return nil, fmt.Errorf("remotefs: webdav %s: this server answers no PROPFIND, so it is not a WebDAV share", w.t.Host)
	}
	if err := davStatus(p, resp.StatusCode); err != nil {
		return nil, err
	}
	// 207 is PROPFIND's only success; a 200 is usually a login page.
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("remotefs: webdav %s: expected a 207 Multi-Status, got %s", p, resp.Status)
	}
	var ms davMultistatus
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxListingBytes)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("remotefs: webdav %s: the listing could not be read: %w", p, err)
	}
	out := make([]davEntry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		e, ok := r.entry()
		if !ok {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// maxListingBytes caps one PROPFIND answer. At roughly 700 bytes per
// Nextcloud entry it holds well over ten thousand files.
const maxListingBytes = 16 << 20

// The XML shapes match on the DAV: namespace, since servers choose their own
// prefix (D:, d:, lp1:).
type davMultistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href     string        `xml:"DAV: href"`
	Propstat []davPropstat `xml:"DAV: propstat"`
}

type davPropstat struct {
	Status string  `xml:"DAV: status"`
	Prop   davProp `xml:"DAV: prop"`
}

type davProp struct {
	// ContentLength is nil for a collection.
	ContentLength *int64        `xml:"DAV: getcontentlength"`
	ResourceType  *davResType   `xml:"DAV: resourcetype"`
	DisplayName   string        `xml:"DAV: displayname"`
	Ignore        []interface{} `xml:",any"`
}

type davResType struct {
	Collection *struct{} `xml:"DAV: collection"`
}

// entry folds one <response> into an Entry, or reports that it says nothing
// usable. Only the 200 propstat is read, since a server answers 404 for each
// property it does not support.
func (r davResponse) entry() (davEntry, bool) {
	p, err := url.PathUnescape(hrefPath(r.Href))
	if err != nil || p == "" {
		return davEntry{}, false
	}
	var e davEntry
	e.path = strings.TrimSuffix(p, "/")
	if e.path == "" {
		e.path = "/"
	}
	e.Name = path.Base(e.path)
	found := false
	for _, ps := range r.Propstat {
		if !strings.Contains(ps.Status, " 200 ") {
			continue
		}
		found = true
		if ps.Prop.ResourceType != nil && ps.Prop.ResourceType.Collection != nil {
			e.Dir = true
		}
		if ps.Prop.ContentLength != nil {
			e.Size = *ps.Prop.ContentLength
		}
		// displayname gives a Nextcloud share its title instead of the id in
		// the URL. It is used only as a plain file name, so it cannot build a
		// path outside the listed folder.
		if n := strings.TrimSpace(ps.Prop.DisplayName); n != "" && !strings.ContainsAny(n, `/\`) && n != "." && n != ".." {
			e.Name = n
		}
	}
	if !found || e.Name == "" {
		return davEntry{}, false
	}
	// The root is kept even though its name is "/", so Stat can answer for a
	// link without a path. List drops it by path and walk refuses names with
	// a separator, so it is never taken for a child.
	return e, true
}

// hrefPath is the path part of a <href>, which RFC 4918 allows as an absolute
// path or a full URL.
func hrefPath(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if u, err := url.Parse(href); err == nil && u.Host != "" {
		return u.EscapedPath()
	}
	return href
}

// samePath compares two server paths ignoring a trailing slash, which a
// collection's href always carries.
func samePath(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// davStatus turns the HTTP status into this package's errors. 401 and 403
// both point at the credential.
func davStatus(p string, code int) error {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("remotefs: webdav %s: %w", p, ErrAuth)
	case code == http.StatusNotFound || code == http.StatusGone:
		return fmt.Errorf("remotefs: webdav %s: %w", p, ErrNotFound)
	case code >= 400:
		return fmt.Errorf("remotefs: webdav %s: the server answered %d", p, code)
	}
	return nil
}
