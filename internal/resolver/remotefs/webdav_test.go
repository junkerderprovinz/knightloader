package remotefs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// fakeDAV serves the smallest thing that is honestly a WebDAV share: PROPFIND
// at depth 0 and 1, and a GET that honours a byte range. Built on httptest
// rather than a raw listener because WebDAV IS HTTP, which is the same reason
// the resolver hands its downloads to the engine instead of fetching them here.
type fakeDAV struct {
	tree map[string]fakeNode
	user string
	pass string
	// ignoreRange makes the server answer 200 with the whole file even when a
	// range was asked for - what a server without range support does, and the
	// one case a resumed download must refuse rather than append.
	ignoreRange bool
	srv         *httptest.Server
}

func newFakeDAV(t *testing.T, user, pass string, tree map[string]fakeNode) *fakeDAV {
	t.Helper()
	d := &fakeDAV{tree: tree, user: user, pass: pass}
	d.srv = httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fakeDAV) target(p string) Target {
	u, _ := url.Parse(d.srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return Target{Kind: KindWebDAV, Host: u.Hostname(), Port: port, Path: p}
}

func (d *fakeDAV) dialer() Dialer { return Dialer{HTTPClient: d.srv.Client()} }

func (d *fakeDAV) resolver() Resolver {
	return Resolver{
		Accounts: Logins{d.target("/").Host: {Username: d.user, Password: d.pass}},
		Dialer:   d.dialer(),
	}
}

func (d *fakeDAV) serve(w http.ResponseWriter, r *http.Request) {
	if d.user != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != d.user || p != d.pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	p, err := url.PathUnescape(r.URL.Path)
	if err != nil {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	p = cleanPath(p)
	node, ok := d.tree[p]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch r.Method {
	case "PROPFIND":
		var paths []string
		paths = append(paths, p)
		if r.Header.Get("Depth") == "1" && node.dir {
			for child := range d.tree {
				if child != p && path.Dir(child) == p {
					paths = append(paths, child)
				}
			}
		}
		sort.Strings(paths)
		var b strings.Builder
		// A deliberately unusual namespace prefix. Every server picks its own
		// letter, so a client that matched on "D:" instead of on the DAV:
		// namespace would work against exactly the one server it was written
		// for - which is what this fixture is here to catch.
		b.WriteString(`<?xml version="1.0"?><lp1:multistatus xmlns:lp1="DAV:">`)
		for _, q := range paths {
			n := d.tree[q]
			href := (&url.URL{Path: q}).EscapedPath()
			if n.dir {
				href += "/"
			}
			fmt.Fprintf(&b, `<lp1:response><lp1:href>%s</lp1:href><lp1:propstat><lp1:prop>`, href)
			if n.dir {
				b.WriteString(`<lp1:resourcetype><lp1:collection/></lp1:resourcetype>`)
			} else {
				fmt.Fprintf(&b, `<lp1:resourcetype/><lp1:getcontentlength>%d</lp1:getcontentlength>`, len(n.data))
			}
			b.WriteString(`</lp1:prop><lp1:status>HTTP/1.1 200 OK</lp1:status></lp1:propstat>`)
			// A second propstat the client must ignore: a property this server
			// does not carry, reported 404 inside a response about a file that
			// is perfectly present.
			b.WriteString(`<lp1:propstat><lp1:prop><lp1:getcontenttype/></lp1:prop><lp1:status>HTTP/1.1 404 Not Found</lp1:status></lp1:propstat>`)
			b.WriteString(`</lp1:response>`)
		}
		b.WriteString(`</lp1:multistatus>`)
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, b.String())
	case http.MethodGet:
		if node.dir {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rng := r.Header.Get("Range")
		if rng == "" || d.ignoreRange {
			_, _ = w.Write(node.data)
			return
		}
		off, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-"), 10, 64)
		if off > int64(len(node.data)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(node.data)-1, len(node.data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(node.data[off:])
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func davTree() map[string]fakeNode {
	return map[string]fakeNode{
		"/":                    {dir: true},
		"/dav":                 {dir: true},
		"/dav/holiday":         {dir: true},
		"/dav/holiday/one.jpg": {data: []byte("one")},
		"/dav/holiday/two.jpg": {data: []byte("two-two")},
		"/dav/two words.mkv":   {data: []byte("0123456789")},
	}
}

// TestWebDAVResolveHandsTheEngineAPlainHTTPLink is the decision this whole
// package rests on: a WebDAV download is an ordinary HTTP download, so the
// answer is a URL the engine already fetches with ranges, several connections
// and the configured outbound route.
func TestWebDAVResolveHandsTheEngineAPlainHTTPLink(t *testing.T) {
	d := newFakeDAV(t, "me", "pw", davTree())
	r := d.resolver()

	got, err := r.Resolve(context.Background(), request(LinkOf(d.target("/dav/two words.mkv"))))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasPrefix(got.DirectURL, "http://") {
		t.Errorf("DirectURL = %q, want an ordinary http link for the engine", got.DirectURL)
	}
	// The space has to be escaped or the engine's own request line is rejected
	// with a 400 long before the server looks at the file.
	if !strings.Contains(got.DirectURL, "two%20words.mkv") {
		t.Errorf("DirectURL = %q, want the path escaped", got.DirectURL)
	}
	if got.Name != "two words.mkv" || got.Size != 10 {
		t.Errorf("got %+v, want the name and size off the listing", got)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("me:pw"))
	if got.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want the stored login", got.Headers["Authorization"])
	}
	// No per-host ceiling: WebDAV over HTTP takes as many connections as the
	// user configured, unlike FTP where one session is one transfer.
	if got.Connections != 0 {
		t.Errorf("connections = %d, want no opinion so the user's own setting stands", got.Connections)
	}
}

func TestWebDAVWrongPasswordAndMissingFileAreDifferentAnswers(t *testing.T) {
	d := newFakeDAV(t, "me", "pw", davTree())

	bad := Resolver{
		Accounts: Logins{d.target("/").Host: {Username: "me", Password: "wrong"}},
		Dialer:   d.dialer(),
	}
	_, err := bad.Resolve(context.Background(), request(LinkOf(d.target("/dav/holiday/one.jpg"))))
	if !errors.Is(err, ErrAuth) {
		t.Errorf("wrong password: error = %v, want the credential named", err)
	}

	_, err = d.resolver().Resolve(context.Background(), request(LinkOf(d.target("/dav/gone.jpg"))))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: error = %v, want the path named", err)
	}
}

func TestWebDAVFolderExpandsWithoutStagingItself(t *testing.T) {
	d := newFakeDAV(t, "me", "pw", davTree())

	got, err := d.resolver().List(context.Background(), LinkOf(d.target("/dav/holiday")))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List = %+v, want the two files", got)
	}
	// A depth-1 PROPFIND answers with the collection ITSELF alongside its
	// children. Staging that would turn one folder link into a task for the
	// same folder, and the collector has no way out of that loop.
	for _, l := range got {
		if strings.HasSuffix(l.URL, "/holiday") {
			t.Errorf("the folder staged itself: %+v", l)
		}
	}
	sizes := map[string]int64{}
	for _, l := range got {
		sizes[l.Name] = l.Size
	}
	if sizes["one.jpg"] != 3 || sizes["two.jpg"] != 7 {
		t.Errorf("sizes = %v, want them read off the listing", sizes)
	}
}

func TestWebDAVLinkToTheServerRootStillListsRatherThanReportingItselfMissing(t *testing.T) {
	// "webdavs://cloud.example.com/" is an ordinary paste, and the root is a
	// directory like any other. The href a server answers for it is the bare
	// "/", whose base name is useless, and dropping the entry on that ground
	// made the link report itself as gone.
	d := newFakeDAV(t, "me", "pw", davTree())
	got, err := d.resolver().List(context.Background(), LinkOf(d.target("/")))
	if err != nil {
		t.Fatalf("List(root): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List(root) = %+v, want every file under it", got)
	}
}

func TestWebDAVRangedReadResumesAndRefusesAServerThatIgnoresRanges(t *testing.T) {
	d := newFakeDAV(t, "me", "pw", davTree())
	fs, err := d.dialer().Dial(context.Background(), d.target("/"), Login{Username: "me", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	rc, err := fs.Open(context.Background(), "/dav/two words.mkv", 4)
	if err != nil {
		t.Fatalf("Open at an offset: %v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "456789" {
		t.Errorf("read %q, want the tail after four bytes", b)
	}

	// The half that matters. A server entitled to ignore the Range header
	// answers 200 with the whole file, and appending that to a half-finished
	// part file produces a corrupt file that reports success.
	d.ignoreRange = true
	if _, err := fs.Open(context.Background(), "/dav/two words.mkv", 4); err == nil {
		t.Fatal("a server that ignored the range was accepted, which would corrupt a resumed file")
	}
}

func TestWebDAVAnOrdinaryWebServerIsNamedAsSuchRatherThanFailingVaguely(t *testing.T) {
	// The likeliest way to misconfigure this: an account stored for a host
	// that serves plain files. 405 to a PROPFIND is exactly how such a server
	// answers, and "not a WebDAV share" is the sentence that saves an
	// afternoon of looking at the password.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = io.WriteString(w, "hello")
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	r := Resolver{
		Accounts: Logins{u.Hostname(): {Username: "me", Password: "pw"}},
		Dialer:   Dialer{HTTPClient: srv.Client()},
	}
	tgt := Target{Kind: KindWebDAV, Host: u.Hostname(), Port: port, Path: "/thing.bin"}
	_, err := r.Resolve(context.Background(), request(LinkOf(tgt)))
	if err == nil || !strings.Contains(err.Error(), "not a WebDAV share") {
		t.Fatalf("error = %v, want it to name the server as an ordinary one", err)
	}
}
