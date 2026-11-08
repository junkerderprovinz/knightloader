// Package ddltest is a stand-in for ddownload's website: the sign-in with its
// Turnstile check, the account page, the file pages of a free and a premium
// download, the forms behind them and the download server, with the waits and
// refusals ddownload hands out on request.
package ddltest

import (
	"bytes"
	"crypto/rand"
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

// SiteKey is the Turnstile key the pages carry, as ddownload's do.
const SiteKey = "0x4AAAAAABm53D0OJNkESa1O"

// Token is the one Turnstile answer the server takes.
const Token = "turnstile-answer"

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
	// TrafficMB is the premium traffic left, in ddownload's megabytes.
	TrafficMB int
	// FormDownloads is an account with direct downloads switched off, whose
	// file pages carry a form rather than sending the download on.
	FormDownloads bool
}

// Server answers for the files and accounts it was given. Links name it as
// ddownload.com, and a client pointed at URL reaches it.
type Server struct {
	*httptest.Server

	mu         sync.Mutex
	files      map[string]File
	accounts   map[string]Account
	sessions   map[string]string
	waits      []int
	countdown  int
	trafficOut bool
	signIns    int
	posts      []url.Values
	requests   []string
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server {
	s := &Server{files: map[string]File{}, accounts: map[string]Account{}, sessions: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Add puts f under id, which ddownload writes as twelve letters and digits,
// and returns its link as people paste it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://ddownload.com/" + id + "/" + strings.ReplaceAll(f.Name, " ", "_")
}

// AddAccount lets user sign in with a.
func (s *Server) AddAccount(user string, a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[user] = a
}

// SignOutAll ends every session, as a session that ran out would.
func (s *Server) SignOutAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]string{}
}

// Wait makes the next free file pages say that this address must wait, one
// page for each figure, in seconds.
func (s *Server) Wait(seconds ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waits = append(s.waits, seconds...)
}

// Countdown puts a countdown of seconds on the free file pages.
func (s *Server) Countdown(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countdown = seconds
}

// TrafficOut makes premium downloads say the day's traffic is used up.
func (s *Server) TrafficOut() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficOut = true
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
	case r.URL.Path == "/login":
		s.render(w, "login.html", map[string]any{"Action": s.URL + "/", "SiteKey": SiteKey})
	case r.URL.Path == "/" && r.PostForm.Get("op") == "login":
		s.signIn(w, r)
	case r.URL.Path == "/" && r.URL.Query().Get("op") == "my_account":
		s.account(w, r)
	default:
		s.file(w, r)
	}
}

func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	s.signIns++
	data := map[string]any{"Action": s.URL + "/", "SiteKey": SiteKey}
	if r.PostForm.Get("cf-turnstile-response") != Token {
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
		http.Redirect(w, r, "/login?redirect="+url.QueryEscape(s.URL+"/?op=my_account"), http.StatusFound)
		return
	}
	s.render(w, "account.html", map[string]any{
		"Premium":   a.Premium,
		"Until":     a.Until.Format("02 January 2006"),
		"TrafficMB": a.TrafficMB,
	})
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	id, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	f, found := s.files[id]
	if !found {
		s.render(w, "gone.html", nil)
		return
	}
	link := s.URL + "/d/" + id + "/" + url.PathEscape(f.Name)
	data := map[string]any{
		"ID":       id,
		"Name":     html.EscapeString(f.Name),
		"Size":     fmt.Sprintf("%.2f KB", float64(len(f.Body))/1024),
		"Password": f.Password != "",
		"SiteKey":  SiteKey,
		"Link":     link,
	}
	user, a := s.user(r)
	premium := user != "" && a.Premium
	data["SignedIn"] = user != ""
	if r.Method == http.MethodPost {
		s.posts = append(s.posts, r.PostForm)
	}
	switch {
	case premium && s.trafficOut:
		data["Notice"] = "You have reached the download-limit: 200000 Mb for last 1 days"
		s.render(w, "notice.html", data)
	case premium && r.Method == http.MethodGet && !a.FormDownloads && f.Password == "":
		http.Redirect(w, r, link, http.StatusFound)
	case premium && r.Method == http.MethodGet:
		s.render(w, "file.html", data)
	case premium:
		s.post(w, r, f, link, data, a.FormDownloads)
	case f.PremiumOnly:
		s.render(w, "premium.html", data)
	case len(s.waits) > 0:
		data["WaitSeconds"] = s.waits[0]
		data["WaitText"] = fmt.Sprintf("%d minutes, %d seconds", s.waits[0]/60, s.waits[0]%60)
		s.waits = s.waits[1:]
		s.render(w, "file.html", data)
	case r.Method == http.MethodGet:
		data["Free"] = true
		data["Countdown"] = s.countdown
		s.render(w, "file.html", data)
	case r.PostForm.Get("cf-turnstile-response") != Token:
		data["Notice"] = "Wrong captcha"
		s.render(w, "notice.html", data)
	default:
		s.post(w, r, f, link, data, false)
	}
}

// post answers a download form: with the download link, on a page of its own
// when inPage is set, or with a refusal of its password.
func (s *Server) post(w http.ResponseWriter, r *http.Request, f File, link string, data map[string]any, inPage bool) {
	if f.Password != "" && r.PostForm.Get("password") != f.Password {
		data["Notice"] = "Wrong password"
		s.render(w, "notice.html", data)
		return
	}
	if inPage {
		s.render(w, "link.html", data)
		return
	}
	http.Redirect(w, r, link, http.StatusFound)
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ExecuteTemplate(w, page, data)
}
