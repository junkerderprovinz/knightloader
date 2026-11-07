// Package fichiertest is a stand-in for 1fichier's website: the file pages a
// free download goes through, the form behind them and the download server,
// with the waits and refusals 1fichier hands out on request.
package fichiertest

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"
)

//go:embed pages/*.html
var pageFiles embed.FS

var pages = template.Must(template.ParseFS(pageFiles, "pages/*.html"))

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Password protects the download. Empty means none.
	Password string
	// PremiumOnly reserves the file for subscribers.
	PremiumOnly bool
}

// Server answers for the files it was given. Links name it as 1fichier.com,
// and a client pointed at URL reaches it.
type Server struct {
	*httptest.Server

	mu        sync.Mutex
	files     map[string]File
	coolDown  []int
	afterForm int
	busy      int
	countdown int
	posts     []url.Values
	requests  []string
	noCookie  []string
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server {
	s := &Server{files: map[string]File{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Add puts f under id and returns its link as people paste it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://1fichier.com/?" + id
}

// CoolDown makes the next pages say that this address must wait, one page
// for each figure, in minutes. A posted form gets such a page as well.
func (s *Server) CoolDown(minutes ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coolDown = append(s.coolDown, minutes...)
}

// CoolDownAfterForm makes the answer to the next form ask for a wait of
// minutes, while the page before it offers the download.
func (s *Server) CoolDownAfterForm(minutes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterForm = minutes
}

// Busy makes the next n file pages say that free downloads are limited.
func (s *Server) Busy(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = n
}

// Countdown puts a countdown of seconds on the file pages.
func (s *Server) Countdown(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countdown = seconds
}

// Posts is every form that was sent in, oldest first.
func (s *Server) Posts() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.posts...)
}

// Requests is every request as "METHOD path?query", oldest first.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// WithoutEnglish is every request to a file page that came without the LG=en
// cookie, which makes 1fichier answer in French.
func (s *Server) WithoutEnglish() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.noCookie...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())

	if id, ok := strings.CutPrefix(r.URL.Path, "/dl/"); ok {
		f, found := s.files[id]
		if !found {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
		return
	}
	if c, err := r.Cookie("LG"); err != nil || c.Value != "en" {
		s.noCookie = append(s.noCookie, r.Method+" "+r.URL.RequestURI())
	}
	id, _, _ := strings.Cut(r.URL.RawQuery, "&")
	f, found := s.files[id]
	if r.URL.Path != "/" || !found {
		w.WriteHeader(http.StatusNotFound)
		s.render(w, "gone.html", nil)
		return
	}
	data := map[string]any{
		"Name":     html.EscapeString(f.Name),
		"Size":     fmt.Sprintf("%.2f KB", float64(len(f.Body))/1024),
		"Action":   s.URL + "/?" + id,
		"Password": f.Password != "",
		"Link":     s.URL + "/dl/" + id,
	}
	switch {
	case f.PremiumOnly:
		s.render(w, "premium.html", data)
	case len(s.coolDown) > 0:
		data["Minutes"] = s.coolDown[0]
		s.coolDown = s.coolDown[1:]
		s.render(w, "cooldown.html", data)
	case s.busy > 0:
		s.busy--
		s.render(w, "busy.html", data)
	case r.Method == http.MethodPost && s.afterForm > 0:
		data["Minutes"] = s.afterForm
		s.afterForm = 0
		s.render(w, "cooldown.html", data)
	case r.Method == http.MethodPost:
		s.post(w, r, f, data)
	default:
		data["Countdown"] = s.countdown
		s.render(w, "file.html", data)
	}
}

// post answers the form: with the download link, or with the form again for
// a wrong password.
func (s *Server) post(w http.ResponseWriter, r *http.Request, f File, data map[string]any) {
	_ = r.ParseForm()
	s.posts = append(s.posts, r.PostForm)
	if f.Password != "" && r.PostForm.Get("pass") != f.Password {
		s.render(w, "file.html", data)
		return
	}
	s.render(w, "link.html", data)
}

func (s *Server) render(w http.ResponseWriter, page string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ExecuteTemplate(w, page, data)
}
