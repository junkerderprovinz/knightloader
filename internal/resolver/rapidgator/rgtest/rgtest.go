// Package rgtest is a stand-in for Rapidgator: the parts of its API v2 a
// premium download goes through, the public file pages and the download
// server, with the refusals Rapidgator hands out on request.
package rgtest

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"
)

//go:embed pages/*.html
var pageFiles embed.FS

var pages = template.Must(template.ParseFS(pageFiles, "pages/*.html"))

// Account is one Rapidgator account the server knows.
type Account struct {
	Email    string
	Password string
	Premium  bool
	// TrafficTotal is the plan's traffic in bytes. Zero reports none, as
	// Rapidgator does for a free account.
	TrafficTotal int64
	TrafficLeft  int64
	PremiumEnd   time.Time
	// TwoFactor asks for a code at every login.
	TwoFactor bool
}

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
}

// Server answers for the accounts and files it was given. Links name it as
// rapidgator.net, and a client pointed at URL reaches it.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	accounts map[string]*Account
	files    map[string]File
	tokens   map[string]*Account
	fail     map[string][]failure
	delay    int
	logins   int
	requests []string
	queries  []string
	// secs is the free page's countdown, pauses the free downloads still
	// turned away with a delay, and sessions what each free download's
	// cookie has got to.
	secs     int
	pauses   int
	sessions map[string]*freeRun
}

// freeRun is one free download on its way through the website.
type freeRun struct {
	id      string
	sid     string
	counted bool
}

// SiteKey is the key of the Turnstile on the captcha page, and Token the one
// answer it takes.
const (
	SiteKey = "0x4AAAAAACGwIXkmZ2lsGdCV"
	Token   = "0.turnstile-answer"
)

type failure struct {
	status  int
	details string
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server {
	s := &Server{
		accounts: map[string]*Account{},
		files:    map[string]File{},
		tokens:   map[string]*Account{},
		fail:     map[string][]failure{},
		secs:     180,
		sessions: map[string]*freeRun{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Countdown sets the seconds the free page counts down.
func (s *Server) Countdown(secs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secs = secs
}

// Pause turns the next n free downloads away with a delay of 15 minutes.
func (s *Server) Pause(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pauses = n
}

// free answers the steps of a free download after the file page, as
// JDownloader's plugin finds them: the countdown's two calls, the captcha
// page and its form. Each step needs the cookie the file page set.
func (s *Server) free(w http.ResponseWriter, r *http.Request, step string) bool {
	cookie, _ := r.Cookie("PHPSESSID")
	var run *freeRun
	if cookie != nil {
		run = s.sessions[cookie.Value]
	}
	ajax := func(v map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch step {
	case "AjaxStartTimer":
		if run == nil || r.Header.Get("X-Requested-With") != "XMLHttpRequest" || r.URL.Query().Get("fid") == "" {
			ajax(map[string]any{"state": "error"})
			return true
		}
		run.sid = "s" + run.id[:12]
		ajax(map[string]any{"state": "started", "sid": run.sid})
	case "AjaxGetDownloadLink":
		if run == nil || r.URL.Query().Get("sid") != run.sid {
			ajax(map[string]any{"state": "error", "code": 1})
			return true
		}
		run.counted = true
		ajax(map[string]any{"state": "done", "code": 200})
	case "captcha":
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if run == nil || !run.counted {
			fmt.Fprint(w, "<html><body><div class=\"error\">An unexpected error occurred.</div></body></html>")
			return true
		}
		_ = r.ParseForm()
		if r.Method == http.MethodPost {
			if r.PostForm.Get("DownloadCaptchaForm[verifyCode]") == Token && r.PostForm.Get("DownloadCaptchaForm[captcha]") == "" {
				link := "http://" + r.Host + "//?r=download/index&session_id=" + run.sid
				fmt.Fprintf(w, "<html><body><script>var download_link = '%s'; location.href = '%s';</script></body></html>", link, link)
				return true
			}
			fmt.Fprint(w, "<html><body><div class=\"errorSummary\"><p>Please fix the following input errors:</p>"+
				"<ul><li>The verification code is incorrect.</li></ul></div>")
		} else {
			fmt.Fprint(w, "<html><body>")
		}
		fmt.Fprintf(w, "<form id=\"captchaform\" action=\"/download/captcha\" method=\"post\">"+
			"<input type=\"hidden\" name=\"DownloadCaptchaForm[captcha]\" value=\"\" />"+
			"<div class=\"cf-turnstile\" data-sitekey=\"%s\"></div>"+
			"<input type=\"submit\" value=\"Download\" /></form></body></html>", SiteKey)
	default:
		return false
	}
	return true
}

// serveFree hands out a file through the link a free download's captcha led
// to.
func (s *Server) serveFree(w http.ResponseWriter, r *http.Request) bool {
	sid := r.URL.Query().Get("session_id")
	if strings.Trim(r.URL.Path, "/") != "" || r.URL.Query().Get("r") != "download/index" || sid == "" {
		return false
	}
	for _, run := range s.sessions {
		if run.sid == sid && run.counted {
			f := s.files[run.id]
			http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
			return true
		}
	}
	http.NotFound(w, r)
	return true
}

// AddAccount lets a.Email sign in with a.Password.
func (s *Server) AddAccount(a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[a.Email] = &a
}

// Add puts f under id, 32 hex digits, and returns its link as people paste
// it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://rapidgator.net/file/" + id + "/" + f.Name + ".html"
}

// Expire ends every session handed out so far.
func (s *Server) Expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.tokens)
}

// Fail makes the next call of method, such as "file/download", answer with
// status and details instead.
func (s *Server) Fail(method string, status int, details string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[method] = append(s.fail[method], failure{status, details})
}

// Delay puts seconds into the delay of every download link.
func (s *Server) Delay(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = seconds
}

// Logins is how many times user/login was called.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// Requests is every request as "METHOD path", oldest first.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// Queries is the query string of every API call, oldest first.
func (s *Server) Queries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

// TrafficLeft is what the account named by email has left.
func (s *Server) TrafficLeft(email string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accounts[email].TrafficLeft
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)

	switch path := r.URL.Path; {
	case strings.HasPrefix(path, "/api/v2/"):
		s.queries = append(s.queries, r.URL.RawQuery)
		s.api(w, r, strings.TrimPrefix(path, "/api/v2/"))
	case s.serveFree(w, r):
	case strings.HasPrefix(path, "/download/") && s.free(w, r, strings.TrimPrefix(path, "/download/")):
	case strings.HasPrefix(path, "/download/"):
		f, ok := s.files[strings.TrimPrefix(path, "/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
	case strings.HasPrefix(path, "/file/"):
		s.page(w, r, strings.TrimPrefix(path, "/file/"))
	default:
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, "<html><head><title>Rapidgator: Premium</title></head><body></body></html>")
	}
}

// page answers a file page as the site does: a missing file sends the
// browser to the premium page with a short-lived fnf cookie.
func (s *Server) page(w http.ResponseWriter, r *http.Request, rest string) {
	id, _, _ := strings.Cut(rest, "/")
	f, ok := s.files[id]
	if !ok {
		http.SetCookie(w, &http.Cookie{Name: "fnf", Value: "1", Path: "/article/premium", MaxAge: 30})
		http.Redirect(w, r, "/article/premium", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	if s.pauses > 0 {
		s.pauses--
		fmt.Fprint(w, "<html><body><div class=\"error\">Delay between downloads must be not less than 15 min.</div></body></html>")
		return
	}
	session := fmt.Sprintf("sess%d", len(s.sessions)+1)
	s.sessions[session] = &freeRun{id: id}
	http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: session, Path: "/"})
	_ = pages.ExecuteTemplate(w, "file.html", map[string]any{
		"Name": f.Name,
		"Size": fmt.Sprintf("%.2f KB", float64(len(f.Body))/1024),
		"Secs": s.secs,
		"Fid":  894317393,
	})
}

func (s *Server) api(w http.ResponseWriter, r *http.Request, method string) {
	_ = r.ParseForm()
	if q := s.fail[method]; len(q) > 0 {
		s.fail[method] = q[1:]
		answer(w, q[0].status, q[0].details, nil)
		return
	}
	if method == "user/login" {
		s.login(w, r)
		return
	}
	acct := s.tokens[r.Form.Get("token")]
	if acct == nil {
		answer(w, http.StatusUnauthorized, "Error. Session doesn't exist", nil)
		return
	}
	switch method {
	case "user/info":
		answer(w, http.StatusOK, "", map[string]any{"user": user(acct)})
	case "file/info":
		id := r.Form.Get("file_id")
		f, ok := s.files[id]
		if !ok {
			answer(w, http.StatusNotFound, "Error: File not found", nil)
			return
		}
		answer(w, http.StatusOK, "", map[string]any{"file": map[string]any{
			"file_id": id, "mode": 0, "mode_label": "Public", "folder_id": "343",
			"name": f.Name, "hash": "097aa1113bf9c60994c2425c7547b760", "size": len(f.Body),
			"created": 1465909983, "url": "http://rapidgator.net/file/" + id + "/" + f.Name,
		}})
	case "file/download":
		id := r.Form.Get("file_id")
		f, ok := s.files[id]
		switch {
		case !ok:
			answer(w, http.StatusNotFound, "Error: File not found", nil)
		case !acct.Premium:
			answer(w, http.StatusForbidden, "Error: This file can be downloaded by premium only", nil)
		case acct.TrafficLeft < int64(len(f.Body)):
			answer(w, http.StatusLocked, "Error: Exceeded traffic", nil)
		default:
			acct.TrafficLeft -= int64(len(f.Body))
			answer(w, http.StatusOK, "", map[string]any{
				"download_url": s.URL + "/download/" + id + "?r=download/index&session_id=" + rand.Text(),
				"delay":        s.delay,
			})
		}
	default:
		answer(w, http.StatusNotFound, "Error: Method not found", nil)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.logins++
	acct := s.accounts[r.Form.Get("login")]
	switch {
	case acct == nil || acct.Password != r.Form.Get("password"):
		answer(w, http.StatusUnauthorized, "Error: Login or password is wrong", nil)
	case acct.TwoFactor && r.Form.Get("code") == "":
		answer(w, http.StatusUnauthorized, "Error: Invalid auth code", nil)
	default:
		token := strings.ToLower(rand.Text())[:26]
		s.tokens[token] = acct
		answer(w, http.StatusOK, "", map[string]any{"token": token, "user": user(acct)})
	}
}

// user is an account as user/login and user/info describe it, the storage
// total a string as in Rapidgator's own example.
func user(a *Account) map[string]any {
	u := map[string]any{
		"email": a.Email, "is_premium": a.Premium, "premium_end_time": nil,
		"state": 1, "state_label": "Activated",
		"traffic": map[string]any{"total": nil, "left": nil},
		"storage": map[string]any{"total": "4398046511104", "left": 4398035213138},
	}
	if !a.PremiumEnd.IsZero() {
		u["premium_end_time"] = a.PremiumEnd.Unix()
	}
	if a.TrafficTotal > 0 {
		u["traffic"] = map[string]any{"total": a.TrafficTotal, "left": a.TrafficLeft}
	}
	return u
}

func answer(w http.ResponseWriter, status int, details string, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"response": response, "status": status, "details": nil}
	if details != "" {
		body["details"] = details
	}
	_ = json.NewEncoder(w).Encode(body)
}
