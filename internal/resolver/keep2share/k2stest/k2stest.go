// Package k2stest is a stand-in for one site of the Keep2Share family: the
// parts of its API v2 a premium download goes through and the download
// server, with the refusals the API hands out on request. The answers keep
// the shapes the live API sends.
package k2stest

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Account is one account the server knows.
type Account struct {
	Email    string
	Password string
	// Expires is when premium ends. Zero is an account that never had
	// premium, which the API answers with false.
	Expires time.Time
	// Traffic is the bytes left for today. Downloading a file takes its size
	// off once.
	Traffic int64
	// Captcha has every sign-in answered with a reCAPTCHA request.
	Captcha bool
}

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Blocked is a file the abuse system took down.
	Blocked bool
}

// Code is the one answer the image captcha takes, and Picture its image.
const Code = "48213"

var Picture = []byte("PNG k2s captcha")

// Server answers for the accounts and files it was given. A client pointed at
// URL reaches it.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	domain   string
	accounts map[string]*Account
	files    map[string]File
	tokens   map[string]*Account
	paid     map[string]bool
	fail     map[string][]failure
	logins   int
	requests []string
	// freeWait is the wait an accepted captcha comes with, in seconds.
	freeWait   int
	challenges map[string]bool
	keys       map[string]string
}

type failure struct {
	status int
	body   map[string]any
}

// New starts a server whose links name domain, such as "k2s.cc". It closes
// when the test ends.
func New(t *testing.T, domain string) *Server {
	s := &Server{
		domain:   domain,
		accounts: map[string]*Account{},
		files:    map[string]File{},
		tokens:   map[string]*Account{},
		paid:     map[string]bool{},
		fail:     map[string][]failure{},

		freeWait:   30,
		challenges: map[string]bool{},
		keys:       map[string]string{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// AddAccount lets a.Email sign in with a.Password.
func (s *Server) AddAccount(a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[a.Email] = &a
}

// Add puts f under id, 13 hex digits, and returns its link as people paste
// it.
func (s *Server) Add(id string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
	return "https://" + s.domain + "/file/" + id + "/" + f.Name
}

// Expire ends every session handed out so far.
func (s *Server) Expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.tokens)
}

// Fail makes the next call of method, such as "getUrl", answer with status
// and an error body carrying errorCode and message. sub, when not 0, goes
// into the errors list the API adds to errorCode 21 and 42.
func (s *Server) Fail(method string, status int, errorCode any, message string, sub int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body := map[string]any{"status": "error", "code": status, "errorCode": errorCode, "message": message}
	if sub != 0 {
		body["errors"] = []map[string]any{{"code": sub}}
	}
	s.fail[method] = append(s.fail[method], failure{status, body})
}

// FreeWait sets the wait an accepted captcha comes with.
func (s *Server) FreeWait(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freeWait = seconds
}

// Logins is how many times login was called.
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

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	switch path := r.URL.Path; {
	case path == "/api/v2/captcha" && r.Method == http.MethodGet:
		if !s.challenges[r.URL.Query().Get("id")] {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(Picture)
	case strings.HasPrefix(path, "/api/v2/"):
		s.api(w, r, strings.TrimPrefix(path, "/api/v2/"))
	case strings.HasPrefix(path, "/download/"):
		f, ok := s.files[strings.TrimPrefix(path, "/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
	default:
		http.NotFound(w, r)
	}
}

// fileID is what the API takes as a file id: the 13 hex digits of a plain
// link or one of the longer ids with dashes and underscores.
var fileID = regexp.MustCompile(`^[a-z0-9_\-]{13,}$`)

func (s *Server) api(w http.ResponseWriter, r *http.Request, method string) {
	var in struct {
		Username  string   `json:"username"`
		Password  string   `json:"password"`
		AuthToken string   `json:"auth_token"`
		IDs       []string `json:"ids"`
		FileID    string   `json:"file_id"`
		Challenge string   `json:"captcha_challenge"`
		Response  string   `json:"captcha_response"`
		FreeKey   string   `json:"free_download_key"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&in) != nil {
		answer(w, http.StatusBadRequest, map[string]any{"status": "error", "code": 400, "errorCode": 2, "message": "Invalid request params"})
		return
	}
	if q := s.fail[method]; len(q) > 0 {
		s.fail[method] = q[1:]
		answer(w, q[0].status, q[0].body)
		return
	}
	notAuthorized := map[string]any{"message": "You are not authorized for this action", "status": "error", "code": 403, "errorCode": 10}
	switch method {
	case "login":
		s.login(w, in.Username, in.Password)
	case "accountInfo":
		acct := s.tokens[in.AuthToken]
		if acct == nil {
			answer(w, http.StatusForbidden, notAuthorized)
			return
		}
		expires := any(false)
		if !acct.Expires.IsZero() {
			expires = acct.Expires.Unix()
		}
		answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200,
			"available_traffic": acct.Traffic, "account_expires": expires})
	case "getFilesInfo":
		s.filesInfo(w, in.IDs)
	case "requestCaptcha":
		challenge := strings.ToLower(rand.Text())[:13]
		s.challenges[challenge] = true
		answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200, "challenge": challenge,
			"captcha_url": s.URL + "/api/v2/captcha?id=" + challenge})
	case "getUrl":
		acct := s.tokens[in.AuthToken]
		if in.AuthToken != "" && acct == nil {
			answer(w, http.StatusForbidden, notAuthorized)
			return
		}
		if in.AuthToken == "" && (in.Challenge != "" || in.FreeKey != "") {
			s.freeURL(w, in.FileID, in.Challenge, in.Response, in.FreeKey)
			return
		}
		s.getURL(w, acct, in.FileID)
	default:
		answer(w, http.StatusNotFound, map[string]any{"status": "error", "code": 404, "message": "Not found"})
	}
}

func (s *Server) login(w http.ResponseWriter, user, password string) {
	s.logins++
	acct := s.accounts[user]
	switch {
	case acct != nil && acct.Captcha:
		answer(w, http.StatusNotAcceptable, map[string]any{"message": "Please verify your request via re captcha challenge",
			"status": "error", "code": 406, "errorCode": 33})
	case acct == nil || acct.Password != password:
		answer(w, http.StatusNotAcceptable, map[string]any{"message": "Invalid login or password",
			"status": "error", "code": 406, "errorCode": 70})
	default:
		token := strings.ToLower(rand.Text())[:26]
		s.tokens[token] = acct
		answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200, "auth_token": token, "message": "success"})
	}
}

// filesInfo answers as the live API does: an id it holds nothing under comes
// back with nulls and is_available false, one that is no id at all is left
// out, and a request with nothing but such ids is refused.
func (s *Server) filesInfo(w http.ResponseWriter, ids []string) {
	out := []map[string]any{}
	for _, id := range ids {
		if !fileID.MatchString(id) {
			continue
		}
		f, ok := s.files[id]
		if !ok {
			out = append(out, map[string]any{"id": id, "name": nil, "size": nil, "is_available": false,
				"access": nil, "is_folder": nil, "md5": nil})
			continue
		}
		out = append(out, map[string]any{
			"id": id, "name": f.Name, "size": len(f.Body), "is_available": !f.Blocked,
			"access": "public", "is_folder": false, "md5": nil, "isAvailableForFree": true, "parent_id": nil,
			"extended_info": map[string]any{"storage_object": "available", "size": len(f.Body),
				"access": "public", "content_type": "application/octet-stream"},
		})
	}
	if len(out) == 0 {
		answer(w, http.StatusBadRequest, map[string]any{"message": "Invalid request params", "status": "error", "code": 400, "errorCode": 2})
		return
	}
	answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200, "files": out})
}

func (s *Server) getURL(w http.ResponseWriter, acct *Account, id string) {
	f, ok := s.files[id]
	switch {
	case !ok:
		answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "File not found", "errorCode": 20})
	case f.Blocked:
		answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "File is blocked", "errorCode": 22})
	case acct == nil || acct.Expires.Before(time.Now()):
		answer(w, http.StatusNotAcceptable, map[string]any{"message": "You need send request for free download with captcha fields",
			"status": "error", "code": 406, "errorCode": 30})
	case !s.paid[id] && acct.Traffic < int64(len(f.Body)):
		answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "Download is not available",
			"errorCode": 21, "errors": []map[string]any{{"code": 2, "message": "Traffic limit exceed"}}})
	default:
		if !s.paid[id] {
			acct.Traffic -= int64(len(f.Body))
			s.paid[id] = true
		}
		answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200,
			"url": s.URL + "/download/" + id + "?temp_url_sig=" + rand.Text() + "&project=k2s"})
	}
}

// freeURL answers a free getUrl as the API documents it: an accepted captcha
// with a key and a wait, the key with the link.
func (s *Server) freeURL(w http.ResponseWriter, id, challenge, response, key string) {
	f, ok := s.files[id]
	switch {
	case !ok:
		answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "File not found", "errorCode": 20})
	case key != "":
		if s.keys[key] != id {
			answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "Wrong free download key", "errorCode": 40})
			return
		}
		delete(s.keys, key)
		answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200,
			"url": s.URL + "/download/" + id + "?temp_url_sig=" + rand.Text() + "&rate_limit=51200"})
	case !s.challenges[challenge] || response != Code:
		delete(s.challenges, challenge)
		answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "Invalid captcha code", "errorCode": 31})
	case f.Blocked:
		answer(w, http.StatusNotAcceptable, map[string]any{"status": "error", "code": 406, "message": "File is blocked", "errorCode": 22})
	default:
		delete(s.challenges, challenge)
		k := strings.ToLower(rand.Text())[:16]
		s.keys[k] = id
		answer(w, http.StatusOK, map[string]any{"status": "success", "code": 200, "message": "Captcha accepted, please wait",
			"free_download_key": k, "time_wait": s.freeWait})
	}
}

func answer(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
