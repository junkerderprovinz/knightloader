// Package filertest is a stand-in for filer.net: the parts of its API a
// premium download goes through and the download server, with the refusals
// the API hands out on request. The answers keep the shapes filer.net's API
// documentation gives and the live API sends.
package filertest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Account is one account the server knows.
type Account struct {
	Email    string
	Password string
	// Premium is a plan that runs until Until.
	Premium bool
	Until   time.Time
	// Traffic is the total and TrafficLeft what is left of it. Downloading a
	// file takes its size off.
	Traffic, TrafficLeft int64
}

// File is one file the server holds.
type File struct {
	Name        string
	Body        []byte
	PremiumOnly bool
}

// Token is the one hCaptcha answer the free download takes.
const Token = "hcaptcha-answer"

// Server answers for the accounts and files it was given. A client pointed at
// URL reaches it.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	accounts map[string]*Account
	files    map[string]File
	fail     map[string][]failure
	requests []string
	// freeWait is the wait a ticket comes with, in seconds, and limits the
	// free downloads still turned away for the hour.
	freeWait int
	limits   int
	tickets  map[string]string
}

type failure struct {
	http, code int
	status     string
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server {
	s := &Server{accounts: map[string]*Account{}, files: map[string]File{}, fail: map[string][]failure{},
		freeWait: 30, tickets: map[string]string{}}
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

// Add puts f under id, at least 16 letters and digits, and returns its link
// as people paste it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://filer.net/get/" + id
}

// Fail makes the next call of the API path that starts with prefix, such as
// "dl" or "profile", answer with httpStatus and an envelope of code and
// status.
func (s *Server) Fail(prefix string, httpStatus, code int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[prefix] = append(s.fail[prefix], failure{httpStatus, code, status})
}

// FreeWait sets the wait a ticket comes with.
func (s *Server) FreeWait(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freeWait = seconds
}

// HourlyLimit turns the next n free downloads away for the hour.
func (s *Server) HourlyLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = n
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
	path := r.URL.Path
	if id, ok := strings.CutPrefix(path, "/download/"); ok {
		f, found := s.files[id]
		// A download link opened with the login again goes to filer.net's
		// error page, as JDownloader's plugin found.
		if !found || r.Header.Get("Authorization") != "" {
			http.Redirect(w, r, "/error/410", http.StatusFound)
			return
		}
		http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
		return
	}
	call, ok := strings.CutPrefix(path, "/api/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	for prefix, q := range s.fail {
		if strings.HasPrefix(call, prefix) && len(q) > 0 {
			s.fail[prefix] = q[1:]
			envelope(w, q[0].http, q[0].code, q[0].status, map[string]any{})
			return
		}
	}
	if strings.HasPrefix(call, "file/") {
		s.website(w, r, strings.TrimPrefix(call, "file/"))
		return
	}
	switch {
	case call == "profile.json" && r.Method == http.MethodGet:
		acct := s.signedIn(w, r)
		if acct == nil {
			return
		}
		until := any(false)
		if acct.Premium {
			until = acct.Until.Unix()
		}
		envelope(w, http.StatusOK, 200, "success", map[string]any{"email": acct.Email, "premium": acct.Premium,
			"until": until, "traffic": acct.Traffic, "traffic_left": acct.TrafficLeft, "register_date": 1600000000})
	case strings.HasPrefix(call, "status/") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(call, "status/"), ".json")
		f, found := s.files[id]
		if !found {
			envelope(w, http.StatusInternalServerError, 505, "file not found", map[string]any{})
			return
		}
		envelope(w, http.StatusOK, 200, "success", info(id, f))
	case strings.HasPrefix(call, "multi_status/") && r.Method == http.MethodGet:
		s.multi(w, strings.Split(strings.TrimSuffix(strings.TrimPrefix(call, "multi_status/"), ".json"), "|"))
	case call == "multi_status.json" && r.Method == http.MethodPost:
		var in struct {
			Hashes []string `json:"hashes"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Hashes) == 0 {
			envelope(w, http.StatusBadRequest, 400, "invalid hashes format", map[string]any{})
			return
		}
		s.multi(w, in.Hashes)
	case strings.HasPrefix(call, "dl/") && r.Method == http.MethodGet:
		acct := s.signedIn(w, r)
		if acct == nil {
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(call, "dl/"), ".json")
		f, found := s.files[id]
		switch {
		case !found:
			envelope(w, http.StatusInternalServerError, 505, "file not found", map[string]any{})
		case !acct.Premium:
			envelope(w, http.StatusForbidden, 403, "premium required", map[string]any{})
		case acct.TrafficLeft < int64(len(f.Body)):
			// The code JDownloader's plugin reads as spent traffic.
			envelope(w, http.StatusForbidden, 504, "traffic limit reached", map[string]any{})
		default:
			acct.TrafficLeft -= int64(len(f.Body))
			link := "http://" + r.Host + "/download/" + id
			w.Header().Set("Location", link)
			d := info(id, f)
			d["download_url"] = link
			envelope(w, http.StatusFound, 302, "redirect", d)
		}
	default:
		envelope(w, http.StatusNotFound, 404, "not found", map[string]any{})
	}
}

// website answers the API filer.net's own download page uses, in the
// shapes its script reads.
func (s *Server) website(w http.ResponseWriter, r *http.Request, call string) {
	refuse := func(status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	switch {
	case strings.HasPrefix(call, "request/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(call, "request/")
		if _, ok := s.files[id]; !ok {
			refuse(http.StatusNotFound, map[string]any{"message": "File not found"})
			return
		}
		if r.URL.Query().Get("hCaptchaToken") != Token {
			refuse(http.StatusBadRequest, map[string]any{"error": "Invalid captcha"})
			return
		}
		if s.limits > 0 {
			s.limits--
			refuse(http.StatusTooManyRequests, map[string]any{"error": "HOURLY_DOWNLOAD_LIMIT", "message": "Hourly download limit reached."})
			return
		}
		ticket := "tk" + id
		s.tickets[ticket] = id
		http.SetCookie(w, &http.Cookie{Name: "filer_session", Value: ticket, Path: "/"})
		refuse(http.StatusOK, map[string]any{"t": ticket, "wt": s.freeWait})
	case call == "download" && r.Method == http.MethodPost:
		var in struct {
			Ticket string `json:"ticket"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		cookie, err := r.Cookie("filer_session")
		id, ok := s.tickets[in.Ticket]
		if !ok || err != nil || cookie.Value != in.Ticket {
			refuse(http.StatusBadRequest, map[string]any{"error": "Invalid ticket"})
			return
		}
		refuse(http.StatusOK, map[string]any{"downloadUrl": "http://" + r.Host + "/download/" + id})
	case r.Method == http.MethodGet && !strings.Contains(call, "/"):
		f, ok := s.files[call]
		if !ok {
			refuse(http.StatusNotFound, map[string]any{"message": "File not found"})
			return
		}
		refuse(http.StatusOK, map[string]any{"hash": call, "name": f.Name, "size": len(f.Body), "premiumOnly": f.PremiumOnly, "wt": s.freeWait})
	default:
		refuse(http.StatusNotFound, map[string]any{"message": "Not found"})
	}
}

func (s *Server) signedIn(w http.ResponseWriter, r *http.Request) *Account {
	user, pass, ok := r.BasicAuth()
	if !ok {
		envelope(w, http.StatusUnauthorized, 401, "authentication required", map[string]any{})
		return nil
	}
	acct := s.accounts[user]
	if acct == nil || acct.Password != pass {
		envelope(w, http.StatusUnauthorized, 401, "invalid credentials", map[string]any{})
		return nil
	}
	return acct
}

func (s *Server) multi(w http.ResponseWriter, ids []string) {
	data := map[string]any{}
	for _, id := range ids {
		if f, ok := s.files[id]; ok {
			data[id] = info(id, f)
		}
	}
	envelope(w, http.StatusOK, 200, "success", data)
}

func info(id string, f File) map[string]any {
	return map[string]any{"file_hash": id, "file_name": f.Name, "file_size": len(f.Body),
		"premium_only": f.PremiumOnly, "view_count": 3, "md5": nil}
}

func envelope(w http.ResponseWriter, httpStatus, code int, status string, data map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "status": status, "data": data})
}
