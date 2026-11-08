// Package gftest is a stand-in for Gofile: its API, the script its website
// signs requests with, and its download servers, answering in the shapes
// Gofile documents.
package gftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// PremiumToken is the account token of a premium login the server knows.
const PremiumToken = "PREMIUMxTOKENxxxxxxxxxxxxxxxxxxx"

// Item is a file or a folder the server holds.
type Item struct {
	Name string
	// Body makes the item a file. A folder has Children instead, by id.
	Body     []byte
	Children []string
	// Password locks a folder and everything in it.
	Password string
	// Private is a folder only its owner may open.
	Private bool
	// PremiumOnly is content only premium accounts may open.
	PremiumOnly bool
}

// Server is a fake Gofile. API and Site are the bases the client takes.
type Server struct {
	API, Site string
	srv       *httptest.Server

	mu     sync.Mutex
	items  map[string]Item
	parent map[string]string
	codes  map[string]string // share code to id
	guests []string
	secret string
	// rateLimited answers the next contents requests with 429.
	rateLimited int
	scripts     int
	contents    []string
	downloads   []string
}

func New(t *testing.T) *Server {
	s := &Server{items: map[string]Item{}, parent: map[string]string{}, codes: map[string]string{}, secret: "first"}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.API, s.Site = s.srv.URL+"/api", s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// Add stores an item under id, reachable by code as well when code is not
// empty, and returns its share link.
func (s *Server) Add(id, code string, it Item) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = it
	for _, c := range it.Children {
		s.parent[c] = id
	}
	if code != "" {
		s.codes[code] = id
		return "https://gofile.io/d/" + code
	}
	return "https://gofile.io/d/" + id
}

// Rotate changes the secret the website script signs with, as Gofile does
// from time to time. A request signed with the old one is refused.
func (s *Server) Rotate(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secret = secret
}

// RateLimit answers the next n contents requests with Gofile's 429.
func (s *Server) RateLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rateLimited = n
}

// Guests lists the guest tokens handed out.
func (s *Server) Guests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.guests...)
}

// Scripts is how often the website script was fetched.
func (s *Server) Scripts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scripts
}

// Contents lists the query of every contents request, in order.
func (s *Server) Contents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.contents...)
}

// Script is the stand-in for /js/wt.obf.js. Gofile's own is obfuscated, but
// it too defines generateWT(token) and reads the browser's user agent and
// language.
func script(secret string) string {
	return fmt.Sprintf(`function generateWT(token) {
  return navigator.userAgent + "::" + navigator.language + "::" + token + "::%s";
}`, secret)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/js/wt.obf.js":
		s.mu.Lock()
		s.scripts++
		secret := s.secret
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, script(secret))
	case r.URL.Path == "/api/accounts" && r.Method == http.MethodPost:
		s.mu.Lock()
		tok := fmt.Sprintf("GUEST%027d", len(s.guests)+1)
		s.guests = append(s.guests, tok)
		s.mu.Unlock()
		answer(w, http.StatusOK, "ok", map[string]any{"id": "acc-" + tok, "token": tok}, nil)
	case strings.HasPrefix(r.URL.Path, "/api/contents/"):
		s.serveContents(w, r, strings.TrimPrefix(r.URL.Path, "/api/contents/"))
	case strings.HasPrefix(r.URL.Path, "/download/web/"):
		s.serveDownload(w, r)
	default:
		answer(w, http.StatusNotFound, "error-notFound", map[string]any{}, nil)
	}
}

func answer(w http.ResponseWriter, code int, status string, data any, metadata any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	body := map[string]any{"status": status, "data": data}
	if metadata != nil {
		body["metadata"] = metadata
	}
	json.NewEncoder(w).Encode(body)
}

func (s *Server) known(token string) bool {
	return token == PremiumToken || containsString(s.guests, token)
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) serveContents(w http.ResponseWriter, r *http.Request, ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contents = append(s.contents, r.URL.RawQuery)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !s.known(token) {
		answer(w, http.StatusUnauthorized, "error-token", map[string]any{}, nil)
		return
	}
	want := r.Header.Get("User-Agent") + "::" + r.Header.Get("X-BL") + "::" + token + "::" + s.secret
	// Gofile does not document its answer to a stale signature, and its
	// website says it bans the address instead, so this status is the
	// fake's own.
	if r.Header.Get("X-Website-Token") != want {
		answer(w, http.StatusUnauthorized, "error-wrongWebsiteToken", map[string]any{}, nil)
		return
	}
	if s.rateLimited > 0 {
		s.rateLimited--
		answer(w, http.StatusTooManyRequests, "error-rateLimit", map[string]any{}, nil)
		return
	}
	id := ref
	if c, ok := s.codes[ref]; ok {
		id = c
	}
	it, ok := s.items[id]
	if !ok {
		answer(w, http.StatusNotFound, "error-notFound", map[string]any{}, nil)
		return
	}
	if s.lockedFor(id).PremiumOnly && token != PremiumToken {
		answer(w, http.StatusForbidden, "error-notPremium", map[string]any{}, nil)
		return
	}
	if lock := s.lockedFor(id); lock.Private {
		answer(w, http.StatusOK, "ok", map[string]any{"id": id, "canAccess": false, "public": false}, nil)
		return
	}
	if pass := s.passwordFor(id); pass != "" {
		sum := sha256.Sum256([]byte(pass))
		switch got := r.URL.Query().Get("password"); got {
		case hex.EncodeToString(sum[:]):
		case "":
			answer(w, http.StatusOK, "ok", map[string]any{"id": id, "canAccess": false, "password": true, "passwordStatus": "passwordRequired"}, nil)
			return
		default:
			answer(w, http.StatusOK, "ok", map[string]any{"id": id, "canAccess": false, "password": true, "passwordStatus": "passwordWrong"}, nil)
			return
		}
	}

	data := s.payload(id, it)
	if it.Body != nil {
		answer(w, http.StatusOK, "ok", data, nil)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	page, size = max(page, 1), max(size, 1)
	from, to := min((page-1)*size, len(it.Children)), min(page*size, len(it.Children))
	children := map[string]any{}
	for _, c := range it.Children[from:to] {
		children[c] = s.payload(c, s.items[c])
	}
	data["children"] = children
	pages := (len(it.Children) + size - 1) / size
	answer(w, http.StatusOK, "ok", data, map[string]any{
		"page": page, "pageSize": size, "totalCount": len(it.Children), "totalPages": pages, "hasNextPage": page < pages,
	})
}

// lockedFor is the nearest item from id up that carries a restriction.
func (s *Server) lockedFor(id string) Item {
	for range 32 {
		it := s.items[id]
		if it.Private || it.PremiumOnly {
			return it
		}
		p, ok := s.parent[id]
		if !ok {
			return Item{}
		}
		id = p
	}
	return Item{}
}

func (s *Server) passwordFor(id string) string {
	for range 32 {
		if p := s.items[id].Password; p != "" {
			return p
		}
		p, ok := s.parent[id]
		if !ok {
			return ""
		}
		id = p
	}
	return ""
}

func (s *Server) payload(id string, it Item) map[string]any {
	if it.Body == nil {
		return map[string]any{"id": id, "type": "folder", "name": it.Name, "code": codeOf(s.codes, id), "public": true, "childrenCount": len(it.Children)}
	}
	return map[string]any{
		"id": id, "type": "file", "name": it.Name, "size": len(it.Body), "md5": "0f8adc1149b1a2c3d4e5f60718293a4b",
		"mimetype": "application/octet-stream", "createTime": 1754512800, "parentFolder": s.parent[id],
		"servers": []string{"store-1"}, "serverSelected": "store-1",
		"link": s.srv.URL + "/download/web/" + id + "/" + it.Name,
	}
}

func codeOf(codes map[string]string, id string) string {
	for c, v := range codes {
		if v == id {
			return c
		}
	}
	return ""
}

// serveDownload hands out a file to a request that carries a known account
// token in its cookie, as Gofile's download servers want.
func (s *Server) serveDownload(w http.ResponseWriter, r *http.Request) {
	id, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/download/web/"), "/")
	c, err := r.Cookie("accountToken")
	s.mu.Lock()
	it, ok := s.items[id]
	known := err == nil && s.known(c.Value)
	s.downloads = append(s.downloads, id)
	s.mu.Unlock()
	if !ok || it.Body == nil {
		http.NotFound(w, r)
		return
	}
	if !known {
		http.Redirect(w, r, "https://gofile.io/d/"+id, http.StatusFound)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(it.Body))
}
