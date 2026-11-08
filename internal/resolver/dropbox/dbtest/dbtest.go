// Package dbtest is a stand-in for Dropbox: its shared link pages, the
// redirect of a dl=1 link to its content server, folder zips, the password
// prompt and the folder listing the website's own script asks for. The pages
// for a gone link and the dl=1 redirect are shaped as the live site's are.
// The password exchange and the folder listing follow JDownloader's plugin,
// since both are posts the live site was not asked for.
package dbtest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Token is the value of the t cookie every page sets, which the website's
// posts carry back.
const Token = "invented-t-cookie"

// File is one shared file or a file inside a shared folder.
type File struct {
	Name string
	Body []byte
}

// Folder is a shared folder or one of its subfolders.
type Folder struct {
	Name    string
	Files   []File
	Folders []Folder
}

// Share is one shared link: a file or a folder, with what guards it.
type Share struct {
	File   *File
	Folder *Folder
	// Password locks the link behind Dropbox's password prompt.
	Password string
	// Disabled links have had too much traffic for the moment.
	Disabled bool
}

// Server is a fake Dropbox. URL is both its website and its content server.
type Server struct {
	URL string
	srv *httptest.Server

	mu     sync.Mutex
	shares map[string]Share
	// page is how many entries one answer of the folder listing holds.
	page  int
	asked []string
}

func New(t *testing.T) *Server {
	s := &Server{shares: map[string]Share{}, page: 30}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// AddFile shares a file under id and returns its link as Dropbox writes it.
func (s *Server) AddFile(id, rlkey string, sh Share) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares["fi/"+id] = sh
	return "https://www.dropbox.com/scl/fi/" + id + "/" + url.PathEscape(sh.File.Name) + "?rlkey=" + rlkey + "&dl=0"
}

// AddFolder shares a folder under id and returns its link.
func (s *Server) AddFolder(id, rlkey string, sh Share) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares["fo/"+id] = sh
	return "https://www.dropbox.com/scl/fo/" + id + "/h?rlkey=" + rlkey + "&dl=0"
}

// Page sets how many entries an answer of the folder listing holds.
func (s *Server) Page(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.page = n
}

// Asked lists every request, method, path and query, in order.
func (s *Server) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.asked = append(s.asked, r.Method+" "+r.URL.RequestURI())
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "t", Value: Token, Path: "/"})
	switch p := r.URL.Path; {
	case strings.HasPrefix(p, "/scl/fi/") || strings.HasPrefix(p, "/scl/fo/"):
		s.link(w, r)
	case strings.HasPrefix(p, "/content/"):
		s.content(w, r)
	case p == "/sm/password":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><title>Dropbox - Enter password</title></html>"))
	case p == "/sm/auth" && r.Method == http.MethodPost:
		s.auth(w, r)
	case p == "/list_shared_link_folder_entries" && r.Method == http.MethodPost:
		s.list(w, r)
	default:
		http.NotFound(w, r)
	}
}

// find is the share a path names and what in it: the file itself, or the
// file or folder at the path's rest inside a folder.
func (s *Server) find(p string) (key string, sh Share, f *File, dir *Folder, ok bool) {
	parts := strings.Split(strings.TrimPrefix(p, "/scl/"), "/")
	if len(parts) < 2 {
		return "", Share{}, nil, nil, false
	}
	key = parts[0] + "/" + parts[1]
	s.mu.Lock()
	sh, ok = s.shares[key]
	s.mu.Unlock()
	switch {
	case !ok:
		return "", Share{}, nil, nil, false
	case sh.File != nil:
		return key, sh, sh.File, nil, true
	}
	dir = sh.Folder
	var rest []string
	if len(parts) > 3 {
		rest = parts[3:]
	}
	for i, name := range rest {
		var next *Folder
		for j := range dir.Folders {
			if dir.Folders[j].Name == name {
				next = &dir.Folders[j]
			}
		}
		if next == nil && i == len(rest)-1 {
			for j := range dir.Files {
				if dir.Files[j].Name == name {
					return key, sh, &dir.Files[j], nil, true
				}
			}
		}
		if next == nil {
			return "", Share{}, nil, nil, false
		}
		dir = next
	}
	return key, sh, nil, dir, true
}

// authed reports whether a request carries the cookie the password prompt
// hands out.
func authed(r *http.Request, sh Share) bool {
	c, err := r.Cookie("sm_auth")
	return sh.Password == "" || (err == nil && c.Value == "ok:"+sh.Password)
}

const invalidPage = `<!DOCTYPE html><html><head><title>Dropbox - Invalid Link - Simplify your life</title></head><body><div id="embedded-app"></div></body></html>`

func (s *Server) link(w http.ResponseWriter, r *http.Request) {
	key, sh, f, dir, ok := s.find(r.URL.Path)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	switch {
	case !ok:
		w.Write([]byte(invalidPage))
	case !authed(r, sh):
		http.Redirect(w, r, "/sm/password?content_id="+url.QueryEscape("AQC"+key)+"&cont="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
	case sh.Disabled:
		w.Write([]byte(`<html><title>Dropbox - Error</title><body><h1>Link Temporarily Disabled</h1></body></html>`))
	case r.URL.Query().Get("dl") != "1":
		w.Write([]byte(`<html><title>Dropbox - ` + map[bool]string{true: "shared", false: "file"}[dir != nil] + ` - Simplify your life</title></html>`))
	case f != nil:
		http.Redirect(w, r, s.URL+"/content/cd/0/get/"+url.PathEscape(strings.TrimPrefix(r.URL.Path, "/scl/"))+"/file?dl=1", http.StatusFound)
	default:
		http.Redirect(w, r, s.URL+"/content/zip/"+url.PathEscape(strings.TrimPrefix(r.URL.Path, "/scl/"))+"/file?dl=1", http.StatusFound)
	}
}

func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/content/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	inner := strings.TrimSuffix(parts[1], "/file")
	if parts[0] == "cd" {
		inner = strings.TrimPrefix(inner, "0/get/")
	}
	_, _, f, dir, ok := s.find("/scl/" + inner)
	switch {
	case !ok:
		http.NotFound(w, r)
	case f != nil:
		w.Header().Set("Content-Type", "application/binary")
		w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"; filename*=UTF-8''`+url.PathEscape(f.Name))
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.Body))
	default:
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+dir.Name+`.zip"`)
		w.Write([]byte("PK\x03\x04" + dir.Name))
	}
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	w.Header().Set("Content-Type", "application/json")
	key := strings.TrimPrefix(r.PostForm.Get("content_id"), "AQC")
	s.mu.Lock()
	sh, ok := s.shares[key]
	s.mu.Unlock()
	if !ok || r.PostForm.Get("t") != Token || r.PostForm.Get("is_xhr") != "true" || r.PostForm.Get("password") != sh.Password {
		w.Write([]byte(`{"status": "error"}`))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "sm_auth", Value: "ok:" + sh.Password, Path: "/"})
	w.Write([]byte(`{"status": "authed"}`))
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	f := r.PostForm
	w.Header().Set("Content-Type", "application/json")
	if f.Get("t") != Token || f.Get("link_type") != "c" || f.Get("is_xhr") != "true" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	p := "/scl/fo/" + f.Get("link_key") + "/" + f.Get("secure_hash") + f.Get("sub_path")
	key, sh, _, dir, ok := s.find(p)
	switch {
	case !ok || dir == nil:
		w.WriteHeader(http.StatusNotFound)
		return
	case !authed(r, sh):
		w.WriteHeader(http.StatusForbidden)
		return
	}
	type entry struct {
		Name  string `json:"filename"`
		Bytes int    `json:"bytes"`
		Dir   bool   `json:"is_dir"`
		Href  string `json:"href"`
	}
	base := "https://www.dropbox.com" + path.Clean(p)
	rlkey := f.Get("rlkey")
	var all []entry
	for _, d := range dir.Folders {
		all = append(all, entry{Name: d.Name, Dir: true, Href: base + "/" + url.PathEscape(d.Name) + "?rlkey=" + rlkey + "&dl=0"})
	}
	for _, file := range dir.Files {
		all = append(all, entry{Name: file.Name, Bytes: len(file.Body), Href: base + "/" + url.PathEscape(file.Name) + "?rlkey=" + rlkey + "&dl=0"})
	}
	s.mu.Lock()
	page := s.page
	s.mu.Unlock()
	from, _ := strconv.Atoi(f.Get("voucher"))
	to := min(from+page, len(all))
	out := map[string]any{
		"entries": all[from:to], "has_more_entries": to < len(all), "total_num_entries": len(all),
		"folder": map[string]any{"filename": dir.Name, "is_dir": true}, "share_token": map[string]any{"linkKey": key},
	}
	if to < len(all) {
		out["next_request_voucher"] = strconv.Itoa(to)
	}
	json.NewEncoder(w).Encode(out)
}
