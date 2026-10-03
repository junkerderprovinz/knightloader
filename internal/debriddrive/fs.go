package debriddrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

// davFS is the drive as golang.org/x/net/webdav serves it. Every call that
// would change something is refused.
type davFS struct{ d *Drive }

func (davFS) Mkdir(context.Context, string, os.FileMode) error { return os.ErrPermission }
func (davFS) RemoveAll(context.Context, string) error          { return os.ErrPermission }
func (davFS) Rename(context.Context, string, string) error     { return os.ErrPermission }

func (f davFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	s, err := f.d.find(ctx, name)
	if err != nil {
		return nil, err
	}
	return info{s.node}, nil
}

// OpenFile finds the path and nothing more. The PROPFIND of a folder opens
// every entry in it, so opening a file must not unlock it; the first read
// does.
func (f davFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, os.ErrPermission
	}
	s, err := f.d.find(ctx, name)
	if err != nil {
		return nil, err
	}
	return &file{d: f.d, ctx: ctx, at: s}, nil
}

// file is one open folder or file. It lives for one request, whose context it
// keeps for the reads it makes.
type file struct {
	d   *Drive
	ctx context.Context
	at  spot

	// listed is a folder's content once Readdir has read it, and next how much
	// of it Readdir has handed out.
	listed []*node
	read   bool
	next   int

	// pos is where the next Read starts. body is the download server's answer
	// a read is taking bytes from, and bodyAt the offset its next byte has.
	pos    int64
	body   io.ReadCloser
	bodyAt int64
}

func (f *file) Stat() (os.FileInfo, error) { return info{f.at.node}, nil }

func (f *file) Write([]byte) (int, error) { return 0, os.ErrPermission }

func (f *file) Readdir(count int) ([]os.FileInfo, error) {
	if !f.at.node.dir {
		return nil, os.ErrInvalid
	}
	if !f.read {
		kids, err := f.d.kids(f.ctx, f.at)
		if err != nil {
			return nil, err
		}
		f.listed, f.read = kids, true
	}
	rest := f.listed[f.next:]
	if count > 0 {
		if len(rest) == 0 {
			return nil, io.EOF
		}
		rest = rest[:min(count, len(rest))]
	}
	f.next += len(rest)
	out := make([]os.FileInfo, len(rest))
	for i, n := range rest {
		out[i] = info{n}
	}
	return out, nil
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.at.node.size
	}
	if offset < 0 {
		return 0, errors.New("debriddrive: seek before the start of the file")
	}
	f.pos = offset
	return offset, nil
}

// Read carries on with the answer it has while the reader reads on from where
// it stopped, and asks the download server again after a seek.
func (f *file) Read(p []byte) (int, error) {
	if f.at.node.dir {
		return 0, os.ErrInvalid
	}
	if f.pos >= f.at.node.size {
		return 0, io.EOF
	}
	if f.body == nil || f.bodyAt != f.pos {
		f.drop()
		body, err := f.d.open(f.ctx, f.at.acct, f.at.node, f.pos)
		if err != nil {
			f.note(0, err)
			return 0, err
		}
		f.body, f.bodyAt = body, f.pos
	}
	n, err := f.body.Read(p)
	f.pos += int64(n)
	f.bodyAt = f.pos
	if errors.Is(err, io.EOF) && f.pos < f.at.node.size {
		err = io.ErrUnexpectedEOF
	}
	f.note(n, err)
	return n, err
}

// failureKey carries a GET's *error from Serve to the file it reads.
type failureKey struct{}

// note keeps the outcome of a read for Serve, which cannot see it otherwise:
// http.ServeContent drops the error of its copy. A read that fails while the
// client is still there sets it, and one that brings bytes clears it again.
func (f *file) note(n int, err error) {
	failed, _ := f.ctx.Value(failureKey{}).(*error)
	switch {
	case failed == nil:
	case err != nil && !errors.Is(err, io.EOF) && f.ctx.Err() == nil:
		*failed = fmt.Errorf("%s: %w", f.at.acct.Name, err)
	case n > 0:
		*failed = nil
	}
}

func (f *file) drop() {
	if f.body != nil {
		f.body.Close()
		f.body = nil
	}
}

func (f *file) Close() error {
	f.drop()
	return nil
}

// info is a node as os.FileInfo. It answers the content type itself, from the
// name, or webdav would read the start of every file a PROPFIND lists.
type info struct{ n *node }

func (i info) Name() string       { return i.n.name }
func (i info) ModTime() time.Time { return i.n.mod }
func (i info) IsDir() bool        { return i.n.dir }
func (i info) Sys() any           { return nil }

func (i info) Size() int64 {
	if i.n.dir {
		return 0
	}
	return i.n.size
}

func (i info) Mode() fs.FileMode {
	if i.n.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

func (i info) ContentType(context.Context) (string, error) {
	if t := mime.TypeByExtension(path.Ext(i.n.name)); t != "" {
		return t, nil
	}
	return "application/octet-stream", nil
}

// allowed is every method the drive answers.
const allowed = "OPTIONS, GET, HEAD, PROPFIND"

// Serve answers one WebDAV request for the drive mounted at prefix, which
// the request's path starts with. Who may ask is for the caller to decide.
func (d *Drive) Serve(w http.ResponseWriter, r *http.Request, prefix string) {
	switch r.Method {
	case http.MethodOptions:
		// Class 1 only: a share nobody can write to has nothing to lock.
		w.Header().Set("DAV", "1")
		w.Header().Set("Allow", allowed)
		return
	case http.MethodGet, http.MethodHead:
	case "PROPFIND":
		// RFC 4918 9.1 lets a server refuse a listing of everything below a
		// folder, which here would read every download on every account.
		if depth := r.Header.Get("Depth"); depth != "0" && depth != "1" {
			http.Error(w, "the drive lists one folder at a time; send Depth: 0 or 1", http.StatusForbidden)
			return
		}
	default:
		w.Header().Set("Allow", allowed)
		http.Error(w, "the drive is read-only", http.StatusMethodNotAllowed)
		return
	}
	if !d.prepare(w, r, prefix) {
		return
	}
	h := &webdav.Handler{Prefix: prefix, FileSystem: davFS{d}, LockSystem: d.locks}
	if r.Method != http.MethodGet {
		h.ServeHTTP(w, r)
		return
	}
	// http.ServeContent sends the status before its first read reaches the
	// download server, so the status waits for the first byte, and a server
	// in trouble gets a 502 instead of a 200 with nothing after it.
	var failed error
	held := &heldStatus{ResponseWriter: w}
	h.ServeHTTP(held, r.WithContext(context.WithValue(r.Context(), failureKey{}, &failed)))
	if failed == nil {
		held.send()
		return
	}
	d.logf("debrid drive: reading %q failed: %v", r.URL.Path, failed)
	if held.sent {
		return
	}
	for _, k := range []string{"Accept-Ranges", "Content-Range", "ETag", "Last-Modified"} {
		w.Header().Del(k)
	}
	http.Error(w, failed.Error(), http.StatusBadGateway)
}

// heldStatus keeps the status back until the first byte of the body.
type heldStatus struct {
	http.ResponseWriter
	code int
	sent bool
}

func (h *heldStatus) WriteHeader(code int) {
	if !h.sent {
		h.code = code
	}
}

func (h *heldStatus) Write(p []byte) (int, error) {
	h.send()
	return h.ResponseWriter.Write(p)
}

func (h *heldStatus) send() {
	if h.sent {
		return
	}
	h.sent = true
	if h.code != 0 {
		h.ResponseWriter.WriteHeader(h.code)
	}
}

// prepare reads from the service what the request will need before webdav
// starts its answer: the listing a PROPFIND shows, and the link a GET reads
// from. webdav writes its status before it reads a folder or a file, so a
// service in trouble would otherwise show as an empty folder or a download
// that breaks off; the download server behind the link is Serve's to check.
// What prepare read stays in memory for webdav to find. It reports whether
// the request may go on.
func (d *Drive) prepare(w http.ResponseWriter, r *http.Request, prefix string) bool {
	name, ok := strings.CutPrefix(r.URL.Path, prefix)
	if !ok || r.Method == http.MethodHead {
		return true
	}
	ctx := r.Context()
	s, err := d.find(ctx, name)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return true
	case err != nil:
		http.Error(w, "the service could not be read: "+err.Error(), http.StatusBadGateway)
		return false
	case r.Method == http.MethodGet && !s.node.dir:
		if _, err := d.link(ctx, s.acct, s.node); err != nil {
			http.Error(w, s.acct.Name+" did not hand out the file: "+err.Error(), http.StatusBadGateway)
			return false
		}
	case r.Method == "PROPFIND" && s.node.dir && r.Header.Get("Depth") == "1":
		if _, err := d.kids(ctx, s); err != nil {
			http.Error(w, "the service could not be read: "+err.Error(), http.StatusBadGateway)
			return false
		}
	}
	return true
}
