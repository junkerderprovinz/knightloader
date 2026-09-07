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

// WHY THERE IS NO WEBDAV LIBRARY IN go.mod.
//
// WebDAV is not a protocol so much as two extra HTTP verbs on top of the one
// this app already speaks fluently. Everything below is a PROPFIND with a
// four-line body, an XML answer with three fields worth reading, and a GET -
// and the GET is the part that matters, because it means a WebDAV download is
// an ORDINARY HTTP DOWNLOAD. Resolver.Resolve hands the plain https URL and an
// Authorization header straight to the existing engine, which already fetches
// it with several connections, byte ranges, the configured outbound route and
// the speed limiter. A library would have brought its own HTTP client along
// and, with it, its own idea of proxies, timeouts and redirects - none of which
// would be internal/httpx's, which is this app's single outbound policy.
//
// So the only thing that had to be written here is the listing, and that is
// this file.

// davTimeout bounds one PROPFIND. Listing a shared folder with a few thousand
// entries on a busy Nextcloud is genuinely slow, and this is not the transfer
// path - see Resolver.Resolve, which never moves bytes.
const davTimeout = 45 * time.Second

// propfindBody asks for exactly the three properties this package reads.
// Asking for <D:allprop/> instead would work everywhere and drag back every
// custom property a Nextcloud, an ownCloud or a SharePoint attaches to every
// file - kilobytes per entry, thousands of entries, for three fields.
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
		// NoTimeout, not davTimeout: this client is also what Open streams a
		// file body through, and a whole-request deadline would cut a long
		// download off mid-transfer. The listing calls bound themselves with
		// their own context instead - see propfind.
		hc = httpx.New(httpx.Options{Timeout: httpx.NoTimeout})
	}
	// Nothing is dialled here on purpose. HTTP has no session to establish, so
	// a connection opened now would only be an idle socket waiting for the
	// first request, and a "connection failed" reported here would be reported
	// again, more accurately, by that request.
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
	// Depth 0 is "this resource and nothing under it" - the whole point of a
	// stat, and the difference between one small answer and the full listing
	// of a folder holding ten thousand files.
	found, err := w.propfind(ctx, p, "0")
	if err != nil {
		return Entry{}, err
	}
	for _, e := range found {
		if samePath(e.path, p) {
			return e.Entry, nil
		}
	}
	// A Depth 0 answer that does not describe the very resource it was asked
	// about is a server disagreeing with itself; the first entry is the only
	// honest reading left, and an empty answer is a missing path.
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
		// Depth 1 includes the collection ITSELF alongside its children, and
		// staging that would turn one folder link into a task for the same
		// folder - a loop the collector has no way out of.
		if samePath(e.path, p) {
			continue
		}
		out = append(out, e.Entry)
	}
	return out, nil
}

// Open issues a ranged GET.
//
// RESUME IS CHECKED, NOT ASSUMED. A server that does not implement ranges is
// entitled to ignore the Range header and answer 200 with the whole file, and
// appending that to a half-finished part file is how a download reports
// success and leaves a corrupt file behind. So a resumed read demands 206 and
// refuses anything else, which turns "this server cannot resume" into a
// visible error instead of silent corruption. (Nextcloud, ownCloud, Apache's
// mod_dav and nginx's dav module all answer 206; the check is for the ones
// that do not.)
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
		// The single most likely mistake this package can be made to commit:
		// an ordinary web server addressed as a WebDAV one, because somebody
		// stored an account for a host that serves plain files. Saying which
		// of the two is wrong costs one branch and saves an afternoon.
		return nil, fmt.Errorf("remotefs: webdav %s: this server answers no PROPFIND, so it is not a WebDAV share", w.t.Host)
	}
	if err := davStatus(p, resp.StatusCode); err != nil {
		return nil, err
	}
	// 207 Multi-Status is the only success PROPFIND has. A 200 here is a
	// server answering something else entirely - a login page, most often -
	// and parsing that as XML produces "no entries" rather than a reason.
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("remotefs: webdav %s: expected a 207 Multi-Status, got %s", p, resp.Status)
	}
	// Capped, like every other body this app parses: a listing is small, and
	// a body that is not is either a mistake or an attack, and neither is
	// worth buffering unbounded.
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

// maxListingBytes caps one PROPFIND answer. Nextcloud emits roughly 700 bytes
// per entry for the three properties asked for above, so this is room for well
// over ten thousand files in one folder.
const maxListingBytes = 16 << 20

// The XML shapes, namespace-qualified. The "DAV: " prefix in each tag is Go's
// way of writing "the DAV: namespace", and it is not optional here: every
// server picks its own prefix letter (D:, d:, lp1:), so matching on the prefix
// instead of the namespace works against exactly the server it was tested on.
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
	// ContentLength is a pointer because a collection legitimately has none,
	// and a plain int64 could not tell "a directory" from "a zero-byte file".
	ContentLength *int64        `xml:"DAV: getcontentlength"`
	ResourceType  *davResType   `xml:"DAV: resourcetype"`
	DisplayName   string        `xml:"DAV: displayname"`
	Ignore        []interface{} `xml:",any"`
}

type davResType struct {
	Collection *struct{} `xml:"DAV: collection"`
}

// entry folds one <response> into an Entry, or reports that it says nothing
// usable.
//
// A response carries one propstat per status: a server that could not read a
// property answers 404 for that one and 200 for the rest, in the same
// response. Reading the 200 block alone is what keeps a "404 Not Found" for
// some property a server happens not to support from being mistaken for the
// file being gone.
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
		// displayname is the name a server wants shown, which for a Nextcloud
		// share is the folder's own title rather than the opaque id in its
		// URL. Taken only when it is a plain file name: a server is free to
		// put anything in there, and a name with a slash in it would build a
		// path outside the folder that was listed.
		if n := strings.TrimSpace(ps.Prop.DisplayName); n != "" && !strings.ContainsAny(n, `/\`) && n != "." && n != ".." {
			e.Name = n
		}
	}
	if !found || e.Name == "" {
		return davEntry{}, false
	}
	// The server's root is kept rather than dropped, even though its name is
	// the useless "/". Stat has to be able to answer for a link that names no
	// path at all (webdavs://cloud.example.com/), which is an ordinary paste
	// and a directory like any other - and dropping it here made that link
	// report itself as missing. It can never be mistaken for a child: List
	// excludes the resource it asked about by path, and walk refuses any name
	// with a separator in it.
	return e, true
}

// hrefPath is the path part of a <href>, which a server may write either as an
// absolute path or as a full URL. Both are legal per RFC 4918, and a client
// that only handles one of them works against half the servers in the world.
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

// samePath compares two server paths ignoring a trailing slash, which is the
// one difference every WebDAV server introduces on its own: a collection's
// href always ends in "/" and the path it was asked about usually does not.
func samePath(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// davStatus turns the HTTP status into this package's vocabulary. 401 and 403
// both mean the credential is the problem: 401 is "you are not logged in", 403
// is "you are, and it does not help", and for somebody looking at a failed
// download the action is the same.
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
