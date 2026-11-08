// Package kftest is a stand-in for Krakenfiles: the JSON its embed player
// reads, the file pages with their download form and Turnstile widget, the
// form's answer and the download server. The JSON, the file page and the gone
// page are shaped as the live site's are. A successful answer to the form
// needs a solved Turnstile check and follows JDownloader's plugin.
package kftest

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"
)

//go:embed pages
var pageFiles embed.FS

// SiteKey is the key of the file page's Turnstile widget, and Token the one
// answer the form takes.
const (
	SiteKey = "0x4AAAAAAB4S-Cq-7quNHQy8"
	Token   = "turnstile-answer"
)

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Used is how much of the day's free transfer the visitor has used up,
	// as the page writes it, such as "5 GB". Empty is none.
	Used string
	// Refuse has the form answer with this error instead of the link.
	Refuse string
}

// Server is a fake Krakenfiles. URL is the site the client takes.
type Server struct {
	URL string
	srv *httptest.Server

	pages *template.Template

	mu    sync.Mutex
	files map[string]File
	posts []Post
}

// Post is one answer to the download form as it came in.
type Post struct {
	ID      string
	Hash    string
	XHR     bool
	Token   string
	Answer  string
	Referer string
}

func New(t *testing.T) *Server {
	s := &Server{files: map[string]File{}, pages: template.Must(template.ParseFS(pageFiles, "pages/*.html"))}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// Add stores f under id and returns its link as people paste it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://krakenfiles.com/view/" + id + "/file.html"
}

// Posts lists every answer to a download form, oldest first.
func (s *Server) Posts() []Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Post(nil), s.posts...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "json":
		s.json(w, parts[1])
	case len(parts) == 3 && parts[0] == "view" && parts[2] == "file.html":
		s.page(w, parts[1])
	case len(parts) == 2 && parts[0] == "download" && r.Method == http.MethodPost:
		s.form(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "force-download":
		s.download(w, r, parts[1])
	default:
		http.NotFound(w, r)
	}
}

// size writes a size as the site does, in binary units with two decimals.
func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (s *Server) json(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	f, ok := s.files[id]
	if !ok {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title": f.Name, "size": size(len(f.Body)), "downloads": 11762, "views": 30549,
		"uploadDate": "13-08-2022", "hash": id, "type": "other",
		"url": "https://krakenfiles.com/view/" + id + "/file.html",
	})
}

func (s *Server) page(w http.ResponseWriter, id string) {
	f, ok := s.files[id]
	name, status := "file.html", http.StatusOK
	if !ok {
		name, status = "gone.html", http.StatusNotFound
	}
	used := f.Used
	if used == "" {
		used = "0 bytes"
	}
	var b bytes.Buffer
	data := map[string]any{"ID": id, "Name": html.EscapeString(f.Name), "Size": size(len(f.Body)), "Used": used, "SiteKey": SiteKey}
	if err := s.pages.ExecuteTemplate(&b, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	http.SetCookie(w, &http.Cookie{Name: "kfsession", Value: "s-" + id, Path: "/"})
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}

// form answers the download form as the site does: JSON with a status, and
// a server error beside it for a refusal.
func (s *Server) form(w http.ResponseWriter, r *http.Request, id string) {
	_ = r.ParseForm()
	s.posts = append(s.posts, Post{
		ID: id, Hash: r.Header.Get("hash"), XHR: r.Header.Get("X-Requested-With") == "XMLHttpRequest",
		Token: r.PostForm.Get("token"), Answer: r.PostForm.Get("cf-turnstile-response"), Referer: r.Referer(),
	})
	w.Header().Set("Content-Type", "application/json")
	f, ok := s.files[id]
	refuse := func(msg string) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "url": "", "msg": msg})
	}
	switch {
	case !ok:
		refuse("File not found")
	case r.PostForm.Get("token") != "dl-token-"+id || r.Header.Get("hash") != id:
		refuse("Invalid token")
	case r.PostForm.Get("cf-turnstile-response") != Token:
		refuse("captcha not valid")
	case f.Refuse != "":
		refuse(f.Refuse)
	default:
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "url": s.URL + "/force-download/" + id + "?fileHash=" + id, "msg": ""})
	}
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, id string) {
	f, ok := s.files[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
}
