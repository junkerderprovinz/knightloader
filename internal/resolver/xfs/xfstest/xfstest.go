// Package xfstest is a stand-in for a hoster on XFileSharing's own
// templates: the file page with the first download form, the second form
// with its countdown and captcha, the page with the link and the download
// server, the sign-in and the account page, with the waits and refusals the
// script hands out on request. A hoster's test can put pages of its own in
// place of the script's, as long as they take the same data.
package xfstest

import (
	"bytes"
	"crypto/rand"
	"embed"
	"fmt"
	"html"
	"io/fs"
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

// SiteKey is the key a widget captcha carries.
const SiteKey = "6LcXfsTestKeyForEveryWidget"

// Token is the one widget answer the server takes, and Code the one typed
// answer.
const (
	Token = "widget-answer"
	Code  = "4711"
)

// Picture is the image an image captcha shows.
var Picture = []byte("\x89PNG\r\n\x1a\n captcha")

// The captchas the second form can ask for.
const (
	NoCaptcha = ""
	Turnstile = "cf-turnstile"
	Recaptcha = "g-recaptcha"
	HCaptcha  = "h-captcha"
	Image     = "image"
	// Plain is the script's own captcha, digits placed by their padding.
	Plain = "plain"
)

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Password protects the download. Empty means none.
	Password string
	// PremiumOnly keeps the file from free downloads.
	PremiumOnly bool
}

// Account is one login the server knows.
type Account struct {
	Password string
	// Premium is a paid plan that runs until Until.
	Premium bool
	Until   time.Time
	// TrafficMB is the premium traffic left.
	TrafficMB int
	// Direct sends a premium download from the file page straight on.
	Direct bool
}

// Server answers for the files and accounts it was given. Links name it as
// Host, and a client pointed at URL reaches it.
type Server struct {
	*httptest.Server
	// Host is the domain the links are written with.
	Host string
	// GoneStatus is the status a deleted file's page answers with, 200 as
	// most hosters do when it is 0.
	GoneStatus int

	pages *template.Template

	mu           sync.Mutex
	files        map[string]File
	accounts     map[string]Account
	sessions     map[string]string
	waits        []int
	countdown    int
	captcha      string
	loginCaptcha bool
	premiumCode  bool
	signIns      int
	posts        []url.Values
	requests     []string
}

// New starts a server for host that closes when the test ends. own holds
// pages that take the place of the script's of the same name, nil for none.
func New(t *testing.T, host string, own fs.FS) *Server {
	pages := template.Must(template.ParseFS(pageFiles, "pages/*.html"))
	if own != nil {
		pages = template.Must(pages.ParseFS(own, "*.html"))
	}
	s := &Server{Host: host, pages: pages, files: map[string]File{}, accounts: map[string]Account{}, sessions: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Add puts f under id, twelve letters and digits, and returns its link as
// people paste it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://" + s.Host + "/" + id + "/" + strings.ReplaceAll(f.Name, " ", "_") + ".html"
}

// AddAccount lets user sign in with a.
func (s *Server) AddAccount(user string, a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[user] = a
}

// Wait makes the next free file pages say that this address must wait, one
// page for each figure, in seconds.
func (s *Server) Wait(seconds ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waits = append(s.waits, seconds...)
}

// Countdown puts a countdown of seconds on the second form.
func (s *Server) Countdown(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countdown = seconds
}

// Captcha makes the second form of a free download ask for kind.
func (s *Server) Captcha(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captcha = kind
}

// LoginCaptcha puts a Turnstile check on the sign-in form.
func (s *Server) LoginCaptcha() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loginCaptcha = true
}

// PremiumCaptcha makes the second form of a premium download ask for the
// same captcha as a free one.
func (s *Server) PremiumCaptcha() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.premiumCode = true
}

// SignIns is how many sign-in forms came in.
func (s *Server) SignIns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.signIns
}

// Posts is every download form that came in, oldest first.
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

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
	_ = r.ParseForm()

	switch {
	case strings.HasPrefix(r.URL.Path, "/d/"):
		s.download(w, r)
	case r.URL.Path == "/captchas/code.png":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(Picture)
	case r.URL.Path == "/login.html":
		s.render(w, "login.html", s.loginData())
	case r.URL.Path == "/" && r.PostForm.Get("op") == "login":
		s.signIn(w, r)
	case r.URL.Path == "/" && r.URL.Query().Get("op") == "my_account":
		s.account(w, r)
	default:
		s.file(w, r)
	}
}

func (s *Server) loginData() map[string]any {
	data := map[string]any{"Action": s.URL + "/"}
	if s.loginCaptcha {
		data["SiteKey"] = SiteKey
	}
	return data
}

func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	s.signIns++
	data := s.loginData()
	if s.loginCaptcha && r.PostForm.Get("cf-turnstile-response") != Token {
		data["WrongCaptcha"] = true
		s.render(w, "login.html", data)
		return
	}
	user := r.PostForm.Get("login")
	a, ok := s.accounts[user]
	if !ok || a.Password != r.PostForm.Get("password") {
		data["Refused"] = true
		s.render(w, "login.html", data)
		return
	}
	session := rand.Text()
	s.sessions[session] = user
	http.SetCookie(w, &http.Cookie{Name: "login", Value: user, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "xfss", Value: session, Path: "/", HttpOnly: true})
	http.Redirect(w, r, "/?op=my_account", http.StatusFound)
}

// user is who the request's session belongs to, "" for nobody.
func (s *Server) user(r *http.Request) (string, Account) {
	c, err := r.Cookie("xfss")
	if err != nil {
		return "", Account{}
	}
	user := s.sessions[c.Value]
	return user, s.accounts[user]
}

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	user, a := s.user(r)
	if user == "" {
		http.Redirect(w, r, "/login.html", http.StatusFound)
		return
	}
	s.render(w, "account.html", map[string]any{
		"Premium":   a.Premium,
		"Until":     a.Until.Format("2 January 2006"),
		"TrafficMB": a.TrafficMB,
	})
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	id, _, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".html"), "/")
	if r.Method == http.MethodPost {
		id = r.PostForm.Get("id")
	}
	f, found := s.files[id]
	if !found {
		s.renderStatus(w, max(s.GoneStatus, http.StatusOK), "gone.html", nil)
		return
	}
	link := s.URL + "/d/" + id + "/" + url.PathEscape(f.Name)
	user, a := s.user(r)
	premium := user != "" && a.Premium
	data := map[string]any{
		"ID":       id,
		"Name":     html.EscapeString(f.Name),
		"Size":     size(len(f.Body)),
		"Bytes":    len(f.Body),
		"Password": f.Password != "",
		"SiteKey":  SiteKey,
		"Link":     link,
		"SignedIn": user != "",
		"Premium":  premium,
	}
	if r.Method == http.MethodPost {
		s.posts = append(s.posts, r.PostForm)
	}
	op := r.PostForm.Get("op")
	switch {
	case premium && r.Method == http.MethodGet && a.Direct && f.Password == "":
		http.Redirect(w, r, link, http.StatusFound)
	case premium && r.Method == http.MethodGet:
		// A premium page skips the first form.
		s.second(w, data, s.premiumCode)
	case premium:
		s.post(w, r, f, data, s.premiumCode)
	case f.PremiumOnly:
		s.render(w, "premium.html", data)
	case r.Method == http.MethodGet && len(s.waits) > 0:
		n := s.waits[0]
		s.waits = s.waits[1:]
		data["Notice"] = fmt.Sprintf("You have to wait %d minutes, %d seconds till next download", n/60, n%60)
		s.render(w, "notice.html", data)
	case r.Method == http.MethodGet:
		// A site whose file page holds the second form shows its countdown
		// and captcha there.
		s.free(data)
		s.render(w, "file.html", data)
	case op == "download1" && (r.PostForm.Get("method_free") == "" || r.PostForm.Has("method_premium")):
		data["Notice"] = "Choose a free or a premium download"
		s.render(w, "notice.html", data)
	case op == "download1":
		s.second(w, data, true)
	default:
		s.post(w, r, f, data, true)
	}
}

// second renders the second form, with the countdown and the captcha of a
// free download when free is set.
func (s *Server) second(w http.ResponseWriter, data map[string]any, free bool) {
	data["Captcha"] = NoCaptcha
	if free {
		s.free(data)
	}
	data["Rand"] = rand.Text()
	s.render(w, "download2.html", data)
}

// free puts the countdown and the captcha of a free download's second form
// into data.
func (s *Server) free(data map[string]any) {
	data["Countdown"] = s.countdown
	data["Captcha"] = s.captcha
	if s.captcha == Plain {
		data["Digits"] = digits(Code)
	}
	data["Rand"] = rand.Text()
}

// post answers the second form: with the page that holds the download link,
// or with a refusal of its captcha or its password.
func (s *Server) post(w http.ResponseWriter, r *http.Request, f File, data map[string]any, free bool) {
	if free && !s.answered(r.PostForm) {
		data["Notice"] = "Wrong captcha"
		s.render(w, "notice.html", data)
		return
	}
	if f.Password != "" && r.PostForm.Get("password") != f.Password {
		data["Notice"] = "Wrong password"
		s.render(w, "notice.html", data)
		return
	}
	s.render(w, "link.html", data)
}

func (s *Server) answered(form url.Values) bool {
	switch s.captcha {
	case Turnstile:
		return form.Get("cf-turnstile-response") == Token
	case Recaptcha:
		return form.Get("g-recaptcha-response") == Token
	case HCaptcha:
		return form.Get("h-captcha-response") == Token
	case Image, Plain:
		return form.Get("code") == Code
	}
	return true
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	id, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/d/"), "/")
	f, found := s.files[id]
	if !found {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
}

func (s *Server) render(w http.ResponseWriter, page string, data map[string]any) {
	s.renderStatus(w, http.StatusOK, page, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, status int, page string, data map[string]any) {
	var b bytes.Buffer
	if err := s.pages.ExecuteTemplate(&b, page, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}

// size writes a size as the script does, in binary units.
func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// digit is one character of the script's own captcha.
type digit struct {
	Left int
	Char string
}

// digits places code's characters as the script does: out of order in the
// page, each moved to its place by its left padding.
func digits(code string) []digit {
	var out []digit
	for i := len(code) - 1; i >= 0; i-- {
		out = append(out, digit{Left: 6 + 14*i, Char: fmt.Sprintf("&#%d;", code[i])})
	}
	return out
}
