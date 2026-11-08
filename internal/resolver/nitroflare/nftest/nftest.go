// Package nftest is a stand-in for Nitroflare: the parts of its API v2 a
// premium download goes through and the download server, with the refusals
// the API hands out on request. The answers keep the shapes the live API
// sends.
package nftest

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Account is one account the server knows.
type Account struct {
	Email    string
	Password string
	// Expires is when premium ends. Zero is an account without premium.
	Expires    time.Time
	TrafficMax int64
	// TrafficLeft is what is left of TrafficMax. Downloading a file takes its
	// size off.
	TrafficLeft int64
	// Captcha has every call with the login answered with a captcha request.
	Captcha bool
}

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Offline is a file the server still names but does not serve.
	Offline bool
}

// Server answers for the accounts and files it was given. A client pointed at
// URL reaches it.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	accounts map[string]*Account
	files    map[string]File
	fail     map[string][]failure
	requests []string
}

type failure struct {
	status  int
	code    int
	message string
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server {
	s := &Server{accounts: map[string]*Account{}, files: map[string]File{}, fail: map[string][]failure{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// AddAccount lets a.Email in with a.Password.
func (s *Server) AddAccount(a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[a.Email] = &a
}

// Add puts f under id, 15 upper-case hex digits, and returns its link as
// people paste it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://nitroflare.com/view/" + id + "/" + f.Name
}

// Fail makes the next call of method, such as "getDownloadLink", answer with
// an error of code and message, at HTTP status.
func (s *Server) Fail(method string, status, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[method] = append(s.fail[method], failure{status, code, message})
}

// Requests is every request as "METHOD path", oldest first.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	switch path := r.URL.Path; {
	case strings.HasPrefix(path, "/api/v2/"):
		s.api(w, r, strings.TrimPrefix(path, "/api/v2/"))
	case strings.HasPrefix(path, "/d/"):
		f, ok := s.files[strings.TrimPrefix(path, "/d/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
	default:
		http.NotFound(w, r)
	}
}

// api reads its parameters from the query string only: the live API answers
// a POSTed login with "Bad input".
func (s *Server) api(w http.ResponseWriter, r *http.Request, method string) {
	q := r.URL.Query()
	if q := s.fail[method]; len(q) > 0 {
		s.fail[method] = q[1:]
		answer(w, q[0].status, map[string]any{"type": "error", "message": q[0].message, "code": q[0].code})
		return
	}
	switch method {
	case "getFileInfo":
		s.fileInfo(w, strings.Split(q.Get("files"), ","))
	case "getKeyInfo":
		acct, ok := s.account(w, q)
		if !ok {
			return
		}
		status, expiry := "inactive", any(0)
		if !acct.Expires.IsZero() {
			expiry = acct.Expires.UTC().Format(time.DateTime)
			if acct.Expires.After(time.Now()) {
				status = "active"
			}
		}
		answer(w, http.StatusOK, map[string]any{"type": "success", "result": map[string]any{
			"status": status, "expiryDate": expiry, "storageUsed": "0", "trafficLeft": acct.TrafficLeft, "trafficMax": acct.TrafficMax,
		}})
	case "getDownloadLink":
		acct, ok := s.account(w, q)
		if !ok {
			return
		}
		id := q.Get("file")
		f, ok := s.files[id]
		switch {
		case !ok || f.Offline:
			answer(w, http.StatusOK, map[string]any{"type": "error", "message": "File doesn't exist", "code": 4})
		case !acct.Expires.After(time.Now()):
			answer(w, http.StatusOK, map[string]any{"type": "error", "message": "Access denied", "code": 1})
		case acct.TrafficLeft < int64(len(f.Body)):
			// The words are the site's. Which code the API sends with them
			// is not known, so this one is made up.
			answer(w, http.StatusOK, map[string]any{"type": "error", "message": "Your premium has reached the maximum volume for today", "code": 9})
		default:
			acct.TrafficLeft -= int64(len(f.Body))
			answer(w, http.StatusOK, map[string]any{"type": "success", "result": map[string]any{
				"name": f.Name, "size": strconv.Itoa(len(f.Body)), "url": s.URL + "/d/" + id + "?k=" + rand.Text(),
			}})
		}
	default:
		answer(w, http.StatusOK, map[string]any{"type": "error", "message": "Bad input", "code": 3})
	}
}

// account is the one the query names, or answers with the refusal.
func (s *Server) account(w http.ResponseWriter, q url.Values) (*Account, bool) {
	if q.Get("user") == "" {
		answer(w, http.StatusOK, map[string]any{"type": "error", "message": "Bad input", "code": 3})
		return nil, false
	}
	acct := s.accounts[q.Get("user")]
	switch {
	case acct == nil || acct.Password != q.Get("premiumKey"):
		answer(w, http.StatusOK, map[string]any{"type": "error", "message": "Wrong login", "code": 8})
		return nil, false
	case acct.Captcha:
		answer(w, http.StatusOK, map[string]any{"type": "error", "message": "Captcha required", "code": 12})
		return nil, false
	}
	return acct, true
}

// fileInfo answers as the live API does: ids it holds nothing under are left
// out, and with none left the files come as an empty list, not a map.
func (s *Server) fileInfo(w http.ResponseWriter, ids []string) {
	files := map[string]any{}
	for _, id := range ids {
		f, ok := s.files[id]
		if !ok {
			continue
		}
		status := "online"
		if f.Offline {
			status = "offline"
		}
		files[id] = map[string]any{
			"status": status, "name": f.Name, "url": "https://nitroflare.com/view/" + id + "/" + f.Name,
			"size": strconv.Itoa(len(f.Body)), "md5": "", "uploadDate": "2026-08-01 10:00:00", "premiumOnly": false, "password": false,
		}
	}
	var out any = files
	if len(files) == 0 {
		out = []any{}
	}
	answer(w, http.StatusOK, map[string]any{"type": "success", "result": map[string]any{"files": out}})
}

func answer(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
