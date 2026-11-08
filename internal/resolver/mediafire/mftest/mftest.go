// Package mftest is a stand-in for Mediafire: its public API, its file pages
// and its download servers. The API answers and the file page are shaped as
// the live site's are. The pages for a limit, a password, a captcha and an
// outage follow what JDownloader's plugin looks for, since the live site
// shows them only when it decides to.
package mftest

import (
	"bytes"
	"embed"
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

//go:embed pages
var pages embed.FS

// Page is what a file's page shows instead of its download button.
type Page int

const (
	PageFile Page = iota
	// PageLimit is the address having downloaded too much for the moment.
	PageLimit
	PagePassword
	PageCaptcha
	PageUnavailable
	PageMalware
	// PageRemoved redirects to the error page for a file its owner removed.
	PageRemoved
)

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	Page Page
	// Deleted is a file the API still describes but marks with a delete
	// date.
	Deleted bool
}

// Folder is one folder the server holds, its files and subfolders by key.
type Folder struct {
	Name    string
	Files   []string
	Folders []string
}

// Server is a fake Mediafire. URL is the site and API base the client takes.
type Server struct {
	URL string
	srv *httptest.Server

	mu      sync.Mutex
	files   map[string]File
	folders map[string]Folder
	// chunk is how many entries one page of a folder's content holds.
	chunk int
	asked []string
}

func New(t *testing.T) *Server {
	s := &Server{files: map[string]File{}, folders: map[string]Folder{}, chunk: 100}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// AddFile stores a file under its quick key and returns its link.
func (s *Server) AddFile(key string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = f
	return "https://www.mediafire.com/file/" + key + "/" + f.Name + "/file"
}

// AddFolder stores a folder under its key and returns its link.
func (s *Server) AddFolder(key string, f Folder) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.folders[key] = f
	return "https://www.mediafire.com/folder/" + key + "/" + f.Name
}

// Chunk sets how many entries a page of a folder's content holds.
func (s *Server) Chunk(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunk = n
}

// Asked lists every request, path and query, in order.
func (s *Server) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.asked = append(s.asked, r.URL.RequestURI())
	s.mu.Unlock()
	switch p := r.URL.Path; {
	case p == "/api/1.5/file/get_info.php":
		s.fileInfo(w, r)
	case p == "/api/1.5/folder/get_info.php":
		s.folderInfo(w, r)
	case p == "/api/1.5/folder/get_content.php":
		s.folderContent(w, r)
	case strings.HasPrefix(p, "/file/"):
		s.page(w, r, strings.Split(strings.TrimPrefix(p, "/file/"), "/")[0])
	case strings.HasPrefix(p, "/download/"):
		s.download(w, r)
	case p == "/error.php":
		w.Write([]byte("<html><body>The file was removed.</body></html>"))
	default:
		http.NotFound(w, r)
	}
}

func reply(w http.ResponseWriter, action string, fields map[string]any) {
	resp := map[string]any{"action": action, "result": "Success", "current_api_version": "1.5"}
	for k, v := range fields {
		resp[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"response": resp})
}

// apiError is how the live API answers a key it does not know.
func apiError(w http.ResponseWriter, action string, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{
		"action": action, "message": message, "error": code, "result": "Error", "current_api_version": "1.5",
	}})
}

func (s *Server) info(key string, f File) map[string]any {
	m := map[string]any{
		"quickkey": key, "filename": f.Name, "ready": "yes", "created": "2019-03-13 12:53:54", "description": "",
		"size": strconv.Itoa(len(f.Body)), "privacy": "public", "password_protected": "no",
		"hash": "e3dca4b1ca1a94f80d4f58f95c026a2ab00ff8742cf60f0e97b86ca8bae6acf0", "filetype": "archive",
		"mimetype": "application/x-gzip", "owner_name": "oochamaloo", "flag": "0",
		"permissions": map[string]any{"value": "1", "explicit": "0", "read": "1", "write": "0"},
		"revision":    "2", "view": "0", "edit": "0",
		"links":       map[string]any{"normal_download": "https://www.mediafire.com/file/" + key + "/" + f.Name + "/file"},
		"created_utc": "2019-03-13T17:53:54Z",
	}
	if f.Page == PagePassword {
		m["password_protected"] = "yes"
	}
	if f.Deleted {
		m["delete_date"] = "2024-01-02 03:04:05"
	}
	return m
}

// fileInfo answers one key with file_info and several with file_infos and
// the keys it skipped, failing only when it knows none.
func (s *Server) fileInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := strings.Split(r.URL.Query().Get("quick_key"), ",")
	var infos []map[string]any
	var skipped []string
	for _, k := range keys {
		if f, ok := s.files[k]; ok {
			infos = append(infos, s.info(k, f))
		} else {
			skipped = append(skipped, k)
		}
	}
	switch {
	case len(infos) == 0:
		apiError(w, "file/get_info", 110, "Unknown or Invalid QuickKey")
	case len(keys) == 1:
		reply(w, "file/get_info", map[string]any{"file_info": infos[0]})
	default:
		fields := map[string]any{"file_infos": infos}
		if len(skipped) > 0 {
			fields["skipped"] = strings.Join(skipped, ",")
		}
		reply(w, "file/get_info", fields)
	}
}

func (s *Server) folderInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.URL.Query().Get("folder_key")
	f, ok := s.folders[key]
	if !ok {
		apiError(w, "folder/get_info", 112, "Unknown or invalid FolderKey")
		return
	}
	reply(w, "folder/get_info", map[string]any{"folder_info": map[string]any{
		"folderkey": key, "name": f.Name, "description": "", "created": "2016-11-25 16:31:11", "privacy": "public",
		"file_count": strconv.Itoa(len(f.Files)), "folder_count": strconv.Itoa(len(f.Folders)), "revision": "12797",
		"owner_name": "Bjoern Hempel", "flag": "0", "created_utc": "2016-11-25T22:31:11Z",
	}})
}

func (s *Server) folderContent(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	key, kind := q.Get("folder_key"), q.Get("content_type")
	f, ok := s.folders[key]
	if !ok {
		apiError(w, "folder/get_content", 112, "Unknown or invalid FolderKey")
		return
	}
	n, _ := strconv.Atoi(q.Get("chunk"))
	n = max(n, 1)
	keys := f.Files
	if kind == "folders" {
		keys = f.Folders
	}
	from, to := min((n-1)*s.chunk, len(keys)), min(n*s.chunk, len(keys))
	content := map[string]any{
		"chunk_size": strconv.Itoa(s.chunk), "content_type": kind, "chunk_number": strconv.Itoa(n),
		"folderkey": key, "more_chunks": map[bool]string{true: "yes", false: "no"}[to < len(keys)], "revision": "12797",
	}
	var entries []map[string]any
	for _, k := range keys[from:to] {
		if kind == "folders" {
			sub := s.folders[k]
			entries = append(entries, map[string]any{
				"folderkey": k, "name": sub.Name, "description": "", "tags": "", "privacy": "public",
				"file_count": strconv.Itoa(len(sub.Files)), "folder_count": strconv.Itoa(len(sub.Folders)),
			})
			continue
		}
		info := s.info(k, s.files[k])
		info["downloads"], info["views"] = "1121", "0"
		entries = append(entries, info)
	}
	// The live API leaves the list out of a folder without entries of the
	// kind asked for.
	if len(entries) > 0 {
		content[kind] = entries
	}
	reply(w, "folder/get_content", map[string]any{"folder_content": content})
}

func (s *Server) page(w http.ResponseWriter, r *http.Request, key string) {
	s.mu.Lock()
	f, ok := s.files[key]
	s.mu.Unlock()
	if !ok || f.Page == PageRemoved {
		http.Redirect(w, r, "/error.php?errno=320&origin=download", http.StatusFound)
		return
	}
	name := map[Page]string{
		PageFile: "file.html", PageLimit: "limit.html", PagePassword: "password.html", PageCaptcha: "captcha.html",
		PageUnavailable: "unavailable.html", PageMalware: "malware.html",
	}[f.Page]
	b, err := pages.ReadFile("pages/" + name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	link := fmt.Sprintf("%s/download/1530/6wkrnlu3vceg/%s/%s", s.srv.URL, key, f.Name)
	page := strings.NewReplacer("{{LINK}}", link, "{{NAME}}", f.Name, "{{KEY}}", key).Replace(string(b))
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Write([]byte(page))
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/download/"), "/")
	if len(parts) != 4 {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	f, ok := s.files[parts[2]]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
}
