// Package wttest is a stand-in for WeTransfer: its short links, the two calls
// of its public download API and the storage the download links lead to. The
// answers follow JDownloader's plugin, since both calls are posts the live
// site was not asked for and transfers expire within days.
package wttest

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

// File is one file of a transfer. Name may carry the folders it was sent in.
type File struct {
	ID   string
	Name string
	Body []byte
}

// Transfer is one transfer the server holds.
type Transfer struct {
	Hash  string
	Files []File
	// Password locks the transfer, Expired ones are past their date, and
	// Verify ones want the recipient to confirm their email first.
	Password string
	Expired  bool
	Verify   bool
}

// Server is a fake WeTransfer. URL is its website and its short link host.
type Server struct {
	URL string
	srv *httptest.Server

	mu        sync.Mutex
	transfers map[string]Transfer
	shorts    map[string]string
	asked     []string
}

func New(t *testing.T) *Server {
	s := &Server{transfers: map[string]Transfer{}, shorts: map[string]string{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// Add stores a transfer under id and returns its link and its short link.
func (s *Server) Add(id string, tr Transfer) (link, short string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transfers[id] = tr
	link = "https://wetransfer.com/downloads/" + id + "/" + tr.Hash
	code := "t-" + id[:10]
	s.shorts[code] = link
	return link, "https://we.tl/" + code
}

// Asked lists every request, method and path, in order.
func (s *Server) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.asked = append(s.asked, r.Method+" "+r.URL.Path)
	target, short := s.shorts[strings.TrimPrefix(r.URL.Path, "/")]
	s.mu.Unlock()
	p := r.URL.Path
	switch {
	case short:
		http.Redirect(w, r, s.URL+strings.TrimPrefix(target, "https://wetransfer.com"), http.StatusMovedPermanently)
	case strings.HasPrefix(p, "/api/v4/transfers/") && r.Method == http.MethodPost:
		s.api(w, r)
	case strings.HasPrefix(p, "/storage/"):
		s.storage(w, r)
	default:
		http.NotFound(w, r)
	}
}

type request struct {
	Hash      string   `json:"security_hash"`
	Recipient string   `json:"recipient_id"`
	Password  string   `json:"password"`
	Intent    string   `json:"intent"`
	FileIDs   []string `json:"file_ids"`
}

func answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	id, call, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v4/transfers/"), "/")
	var req request
	if json.NewDecoder(r.Body).Decode(&req) != nil || r.Header.Get("Content-Type") != "application/json" {
		answer(w, http.StatusBadRequest, map[string]any{"message": "bad request"})
		return
	}
	s.mu.Lock()
	tr, ok := s.transfers[id]
	s.mu.Unlock()
	if !ok || req.Hash != tr.Hash {
		answer(w, http.StatusNotFound, map[string]any{"message": "Transfer not found"})
		return
	}
	if tr.Password != "" && req.Password != "" && req.Password != tr.Password {
		answer(w, http.StatusForbidden, map[string]any{"message": "invalid_transfer_password"})
		return
	}
	switch call {
	case "prepare-download":
		s.prepare(w, id, tr, req)
	case "download":
		s.download(w, id, tr, req)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) prepare(w http.ResponseWriter, id string, tr Transfer, req request) {
	state := "downloadable"
	if tr.Expired {
		state = "expired"
	}
	out := map[string]any{
		"id": id, "state": state, "password_protected": tr.Password != "",
		"recommended_filename": "wetransfer_isle-of-dogs_2026-11-08_0712.zip", "display_name": tr.Files[0].Name,
		"expires_at": "2026-11-15T07:12:43.000Z", "shortened_url": "https://we.tl/t-" + id[:10],
	}
	if tr.Password == "" || req.Password == tr.Password {
		var items []map[string]any
		var total int
		for _, f := range tr.Files {
			items = append(items, map[string]any{"id": f.ID, "name": f.Name, "size": len(f.Body), "type": "file"})
			total += len(f.Body)
		}
		out["items"], out["size"] = items, total
	}
	answer(w, http.StatusOK, out)
}

func (s *Server) download(w http.ResponseWriter, id string, tr Transfer, req request) {
	switch {
	case tr.Expired:
		answer(w, http.StatusOK, map[string]any{"error": "invalid_transfer"})
	case tr.Verify:
		answer(w, http.StatusForbidden, map[string]any{"message": "No download access to this Transfer"})
	case tr.Password != "" && req.Password == "":
		answer(w, http.StatusForbidden, map[string]any{"message": "invalid_transfer_password"})
	case req.Intent == "entire_transfer":
		answer(w, http.StatusOK, map[string]any{"direct_link": s.URL + "/storage/" + id + "/zip"})
	case req.Intent == "single_file" && len(req.FileIDs) == 1:
		answer(w, http.StatusOK, map[string]any{"direct_link": s.URL + "/storage/" + id + "/" + req.FileIDs[0] + "?X-Amz-Expires=3600"})
	default:
		answer(w, http.StatusBadRequest, map[string]any{"message": "bad intent"})
	}
}

func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	id, file, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/storage/"), "/")
	s.mu.Lock()
	tr, ok := s.transfers[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if file == "zip" {
		w.Header().Set("Content-Disposition", `attachment; filename="wetransfer_isle-of-dogs_2026-11-08_0712.zip"`)
		w.Write([]byte("PK\x03\x04"))
		return
	}
	for _, f := range tr.Files {
		if f.ID == file {
			name := f.Name[strings.LastIndex(f.Name, "/")+1:]
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.Body))
			return
		}
	}
	http.NotFound(w, r)
}
