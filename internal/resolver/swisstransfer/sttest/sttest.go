// Package sttest is a stand-in for SwissTransfer: the transfer page with the
// page data its script reads, the API call that hands out a file's signed
// link, and the storage behind it. The page data, the not-found page and the
// API answer are shaped as the live site's are. The password post follows
// JDownloader's plugin, since no locked transfer was at hand.
package sttest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// File is one file of a transfer. Path may carry the folders it was sent in.
type File struct {
	ID   string
	Path string
	Body []byte
}

// Transfer is one transfer the server holds under its link's id.
type Transfer struct {
	ID    string
	Title string
	Files []File
	// ExpiresAt is when the transfer goes, in Unix seconds.
	ExpiresAt int64
	// Password locks the transfer, and Exceeded ones have been downloaded as
	// often as their sender allowed.
	Password string
	Exceeded bool
}

// Server is a fake SwissTransfer. URL is its website and its storage.
type Server struct {
	URL string
	srv *httptest.Server

	mu        sync.Mutex
	transfers map[string]Transfer
	asked     []string
}

func New(t *testing.T) *Server {
	s := &Server{transfers: map[string]Transfer{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// Add stores a transfer under its link's id and returns the link.
func (s *Server) Add(linkID string, tr Transfer) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transfers[linkID] = tr
	return "https://www.swisstransfer.com/dl/" + linkID
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
	s.mu.Unlock()
	if _, err := r.Cookie("ST_SESSION"); err != nil {
		http.SetCookie(w, &http.Cookie{Name: "SWISSTRANSFER-API-XSRF-TOKEN", Value: "invented-xsrf-token%3D", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "ST_SESSION", Value: "guest", Path: "/", HttpOnly: true})
	}
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/dl/"):
		s.page(w, r, strings.TrimPrefix(p, "/dl/"))
	case strings.HasPrefix(p, "/api/1/links/"):
		s.file(w, r)
	case strings.HasPrefix(p, "/storage/"):
		s.storage(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) find(id string) (Transfer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tr, ok := s.transfers[id]
	return tr, ok
}

// unlocked reports whether a request's session has entered the transfer's
// password.
func unlocked(r *http.Request, id string, tr Transfer) bool {
	c, err := r.Cookie("ST_SESSION")
	return tr.Password == "" || (err == nil && c.Value == "unlocked:"+id)
}

// render answers with the page data, inside the page or alone for a request
// the site's script sent.
func render(w http.ResponseWriter, r *http.Request, status int, data map[string]any) {
	b, _ := json.Marshal(data)
	if r.Header.Get("X-Inertia") == "true" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Inertia", "true")
		w.WriteHeader(status)
		w.Write(b)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// The live page writes its data as PHP does, every slash escaped.
	fmt.Fprintf(w, `<!DOCTYPE html><html lang="en"><head><title>SwissTransfer</title></head><body><script data-page="app" type="application/json">%s</script><div id="app"></div></body></html>`, strings.ReplaceAll(string(b), "/", `\/`))
}

func (s *Server) page(w http.ResponseWriter, r *http.Request, id string) {
	tr, ok := s.find(id)
	base := map[string]any{"version": "app:1.0.0", "url": "/dl/" + id}
	if !ok {
		base["component"] = "errors/transfer/not-found"
		base["props"] = map[string]any{"errors": map[string]any{}, "user": nil}
		render(w, r, http.StatusNotFound, base)
		return
	}
	open := unlocked(r, id, tr)
	if r.Method == http.MethodPost {
		r.ParseForm()
		if r.Header.Get("X-Inertia") != "true" || r.Header.Get("X-XSRF-TOKEN") != "invented-xsrf-token=" {
			w.WriteHeader(419)
			return
		}
		if r.PostForm.Get("password") == tr.Password {
			http.SetCookie(w, &http.Cookie{Name: "ST_SESSION", Value: "unlocked:" + id, Path: "/", HttpOnly: true})
			open = true
		}
	}
	props := map[string]any{"errors": map[string]any{}, "user": nil, "link": map[string]any{"id": id}}
	base["component"] = "link/show"
	if open {
		var files []map[string]any
		var total int
		for _, f := range tr.Files {
			files = append(files, map[string]any{"id": f.ID, "path": f.Path, "size": len(f.Body), "mime_type": "application/octet-stream"})
			total += len(f.Body)
		}
		props["transfer"] = map[string]any{"id": tr.ID, "title": tr.Title, "message": nil, "total_size": total, "expires_at": tr.ExpiresAt, "files": files, "type": "link"}
	} else {
		base["component"] = "link/password"
	}
	base["props"] = props
	render(w, r, http.StatusOK, base)
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/1/links/"), "/")
	w.Header().Set("Content-Type", "application/json")
	if len(parts) != 3 || parts[1] != "files" || r.Header.Get("Accept") != "application/json" {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Object not found","errorCode":404}`))
		return
	}
	tr, ok := s.find(parts[0])
	switch {
	case !ok:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Object not found","errorCode":404}`))
	case !unlocked(r, parts[0], tr):
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"result":"error","message":"Unauthorized"}`))
	case tr.Exceeded:
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"result":"error","message":"Download Number Exceeded","errorCode":"500"}`))
	default:
		json.NewEncoder(w).Encode(map[string]any{"result": "success", "data": map[string]any{
			"url": s.URL + "/storage/" + tr.ID + "/" + parts[2] + "?X-Amz-Expires=3600&X-Amz-Signature=63ac27631c3b",
		}})
	}
}

func (s *Server) storage(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/storage/"), "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tr := range s.transfers {
		for _, f := range tr.Files {
			if len(parts) == 2 && tr.ID == parts[0] && f.ID == parts[1] {
				name := f.Path[strings.LastIndex(f.Path, "/")+1:]
				w.Header().Set("Content-Disposition", "attachment; filename="+name)
				http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.Body))
				return
			}
		}
	}
	http.NotFound(w, r)
}
