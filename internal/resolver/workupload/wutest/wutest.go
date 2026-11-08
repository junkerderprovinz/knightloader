// Package wutest is a stand-in for Workupload: its robot check with the
// SHA-256 puzzle, the file pages, the start page, the API that names the
// download server, and the download server. The robot check, the file page
// and the API answer are shaped as the live site's are, and the check comes
// back after a few requests as it does there. The password form follows
// JDownloader's plugin.
package wutest

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"
)

//go:embed pages
var pageFiles embed.FS

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Password protects the download. Empty means none.
	Password string
	// Unavailable has the API turn the download down.
	Unavailable bool
}

// Server is a fake Workupload. URL is the site the client takes.
type Server struct {
	URL string
	srv *httptest.Server

	// Pass is how many requests one solved check lets through, 0 for the
	// check never showing.
	Pass int
	// Stubborn keeps the check up whatever the answer.
	Stubborn bool

	pages *template.Template

	mu       sync.Mutex
	files    map[string]File
	puzzles  map[string][]int
	passes   map[string]int
	unlocked map[string]bool
	solves   int
	requests []string
}

func New(t *testing.T) *Server {
	s := &Server{
		Pass: 2, files: map[string]File{}, puzzles: map[string][]int{}, passes: map[string]int{}, unlocked: map[string]bool{},
		pages: template.Must(template.ParseFS(pageFiles, "pages/*.html")),
	}
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
	return "https://workupload.com/file/" + id
}

// Solves is how many answers to the robot check were taken.
func (s *Server) Solves() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.solves
}

// Requests lists every request as "METHOD path", oldest first.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// visitor is the cookie the server tells visitors apart by.
const visitor = "PHPSESSID"

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	_ = r.ParseForm()
	who := ""
	if c, err := r.Cookie(visitor); err == nil {
		who = c.Value
	} else {
		who = rand.Text()
		http.SetCookie(w, &http.Cookie{Name: visitor, Value: who, Path: "/"})
	}
	switch p := r.URL.Path; {
	case p == "/puzzle":
		s.puzzle(w, who)
		return
	case p == "/captcha" && r.Method == http.MethodPost:
		s.answer(w, r, who)
		return
	case strings.HasPrefix(p, "/download/"):
		s.download(w, r, strings.TrimPrefix(p, "/download/"))
		return
	}
	if s.Pass > 0 {
		if s.passes[who] == 0 {
			s.render(w, http.StatusOK, "robot.html", nil)
			return
		}
		s.passes[who]--
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "file":
		s.file(w, r, who, parts[1])
	case len(parts) == 2 && parts[0] == "start":
		s.start(w, r, who, parts[1])
	case len(parts) == 4 && parts[0] == "api" && parts[2] == "getDownloadServer":
		s.server(w, who, parts[3])
	default:
		http.NotFound(w, r)
	}
}

// puzzle hands out three numbers below the range by their SHA-256 hashes.
func (s *Server) puzzle(w http.ResponseWriter, who string) {
	seed := strconv.FormatInt(time.Now().Unix(), 10) + "." + rand.Text()[:12]
	var nums []int
	var find []string
	for len(nums) < 3 {
		n, _ := rand.Int(rand.Reader, big.NewInt(10000))
		if slices.Contains(nums, int(n.Int64())) {
			continue
		}
		nums = append(nums, int(n.Int64()))
		sum := sha256.Sum256([]byte(seed + n.String()))
		find = append(find, hex.EncodeToString(sum[:]))
	}
	s.puzzles[who] = nums
	data := map[string]any{"puzzle": seed, "range": 10000, "find": find}
	cookie, _ := json.Marshal(data)
	http.SetCookie(w, &http.Cookie{Name: "captcha", Value: url.QueryEscape(string(cookie)), Path: "/"})
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, who string) {
	want := s.puzzles[who]
	var got []int
	for _, f := range strings.Fields(r.PostForm.Get("captcha")) {
		n, _ := strconv.Atoi(f)
		got = append(got, n)
	}
	slices.Sort(got)
	slices.Sort(want)
	ok := want != nil && slices.Equal(got, want) && !s.Stubborn
	if ok {
		s.solves++
		s.passes[who] = s.Pass
		delete(s.puzzles, who)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": ok})
}

func (s *Server) file(w http.ResponseWriter, r *http.Request, who, id string) {
	f, ok := s.files[id]
	if !ok {
		s.render(w, http.StatusNotFound, "gone.html", nil)
		return
	}
	key := who + "/" + id
	if r.Method == http.MethodPost && f.Password != "" && r.PostForm.Get("passwordprotected_file[password]") == f.Password && r.PostForm.Get("passwordprotected_file[key]") == id {
		s.unlocked[key] = true
	}
	locked := f.Password != "" && !s.unlocked[key]
	s.render(w, http.StatusOK, "file.html", map[string]any{
		"ID": id, "Name": html.EscapeString(f.Name), "Bytes": len(f.Body), "Size": size(len(f.Body)), "Locked": locked,
	})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request, who, id string) {
	f, ok := s.files[id]
	if !ok {
		s.render(w, http.StatusNotFound, "gone.html", nil)
		return
	}
	if f.Password != "" && !s.unlocked[who+"/"+id] {
		http.Redirect(w, r, "/file/"+id, http.StatusFound)
		return
	}
	s.unlocked[who+"/start/"+id] = true
	s.render(w, http.StatusOK, "start.html", map[string]any{"ID": id, "Name": html.EscapeString(f.Name)})
}

// server answers as the live API does, JSON under an HTML content type.
func (s *Server) server(w http.ResponseWriter, who, id string) {
	f, ok := s.files[id]
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	if !ok || f.Unavailable || !s.unlocked[who+"/start/"+id] {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "data": nil})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]string{"url": s.URL + "/download/" + id}})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, id string) {
	f, ok := s.files[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data map[string]any) {
	var b bytes.Buffer
	if err := s.pages.ExecuteTemplate(&b, page, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}

// size writes a size as the page's heading does.
func size(n int) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
