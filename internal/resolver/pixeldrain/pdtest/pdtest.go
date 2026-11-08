// Package pdtest is a stand-in for Pixeldrain's API, answering in the shapes
// the live service answers in.
package pdtest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// File is one file the server holds.
type File struct {
	Name string
	Body []byte
	// Availability is the restriction the file info reports and a download
	// is refused with, such as "file_rate_limited_captcha_required".
	Availability string
	// AbuseType is set on a file taken down after a report.
	AbuseType string
}

// List is one list of files the server holds.
type List struct {
	Title string
	Files []string
}

// Server is a fake Pixeldrain. URL is its API base, as the client takes it.
type Server struct {
	URL string
	srv *httptest.Server

	mu    sync.Mutex
	files map[string]File
	lists map[string]List
	// refuse is the error every download gets, "" for none.
	refuse string
	// plan is the subscription type of the one valid key, Key.
	plan  string
	auths []string
	paths []string
}

// Key is the API key the server accepts.
const Key = "test-key"

func New(t *testing.T) *Server {
	s := &Server{files: map[string]File{}, lists: map[string]List{}, plan: "free"}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL + "/api"
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// AddFile stores a file under id.
func (s *Server) AddFile(id string, f File) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = f
}

// AddList stores a list under id.
func (s *Server) AddList(id string, l List) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists[id] = l
}

// Refuse makes every download fail with value, as a limit does.
func (s *Server) Refuse(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refuse = value
}

// Plan sets the subscription type of the account behind Key.
func (s *Server) Plan(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = kind
}

// Auths lists the Authorization header of every request, in order.
func (s *Server) Auths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auths...)
}

// Paths lists the path of every request, in order.
func (s *Server) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// The messages are the live service's.
var messages = map[string]string{
	"not_found":                          "The entity you requested could not be found",
	"authentication_failed":              "The provided API key is invalid, has been revoked or has expired",
	"file_rate_limited_captcha_required": "We have detected the use of hotlinking for this file. Hotlinking is only supported when either the uploader or the downloader has a paid subscription",
	"hotlink_detected":                   "Hotlinking was detected, hotlinking is only allowed with a premium subscription",
	"transfer_limit_exceeded":            "You have exceeded the free transfer limit, upgrade to premium to continue downloading",
	"max_concurrent_downloads":           "Too many simultaneous download connections from your IP address",
	"unavailable_for_legal_reasons":      "The file received a takedown report and cannot be downloaded",
}

var statuses = map[string]int{
	"not_found":                     http.StatusNotFound,
	"authentication_failed":         http.StatusUnauthorized,
	"unavailable_for_legal_reasons": http.StatusUnavailableForLegalReasons,
}

func fail(w http.ResponseWriter, value string) {
	code, ok := statuses[value]
	if !ok {
		code = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"success": false, "value": value, "message": messages[value]})
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.auths = append(s.auths, r.Header.Get("Authorization"))
	s.paths = append(s.paths, r.URL.Path)
	s.mu.Unlock()

	if auth := r.Header.Get("Authorization"); auth != "" && auth != "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+Key)) {
		fail(w, "authentication_failed")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api")
	switch {
	case path == "/user":
		s.user(w, r)
	case strings.HasPrefix(path, "/list/"):
		s.list(w, strings.TrimPrefix(path, "/list/"))
	case strings.HasPrefix(path, "/file/") && strings.HasSuffix(path, "/info"):
		s.info(w, strings.TrimSuffix(strings.TrimPrefix(path, "/file/"), "/info"))
	case strings.HasPrefix(path, "/file/"):
		s.download(w, r, strings.TrimPrefix(path, "/file/"))
	default:
		fail(w, "not_found")
	}
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "value": "authentication_required"})
		return
	}
	s.mu.Lock()
	plan := s.plan
	s.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]any{
		"username":     "reader",
		"subscription": map[string]any{"id": plan, "name": plan, "type": plan},
	})
}

func (s *Server) fileInfo(id string, f File) map[string]any {
	return map[string]any{
		"id": id, "name": f.Name, "size": len(f.Body), "views": 1, "downloads": 9,
		"date_upload": "2026-10-24T11:47:40.718Z", "mime_type": "application/zip",
		"thumbnail_href": "/file/" + id + "/thumbnail", "hash_sha256": "24c3f6f349065104c3f7db4d89a982e7287ed4ae3218de11e2aac6a26798470e",
		"delete_after_date": "0001-01-01T00:00:00Z", "delete_after_downloads": 0,
		"availability": f.Availability, "availability_message": messages[f.Availability],
		"abuse_type": f.AbuseType, "abuse_reporter_name": "",
		"can_edit": false, "can_download": true, "show_ads": true, "allow_video_player": true,
		"download_speed_limit": 0,
	}
}

// info answers one id with an object and several with an array that leaves
// out the ones that do not exist, failing only when none does.
func (s *Server) info(w http.ResponseWriter, ids string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []map[string]any
	for _, id := range strings.Split(ids, ",") {
		if f, ok := s.files[id]; ok {
			found = append(found, s.fileInfo(id, f))
		}
	}
	if len(found) == 0 {
		fail(w, "not_found")
		return
	}
	if !strings.Contains(ids, ",") {
		found[0]["success"] = true
		json.NewEncoder(w).Encode(found[0])
		return
	}
	json.NewEncoder(w).Encode(found)
}

func (s *Server) list(w http.ResponseWriter, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[id]
	if !ok {
		fail(w, "not_found")
		return
	}
	files := []map[string]any{}
	for _, fid := range l.Files {
		info := s.fileInfo(fid, s.files[fid])
		info["detail_href"] = "/file/" + fid + "/info"
		info["description"] = ""
		files = append(files, info)
	}
	json.NewEncoder(w).Encode(map[string]any{
		"success": true, "id": id, "title": l.Title, "date_created": "2026-10-24T12:21:43.02Z",
		"file_count": len(files), "files": files, "can_edit": false,
	})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	f, ok := s.files[id]
	refuse := s.refuse
	s.mu.Unlock()
	switch {
	case !ok:
		fail(w, "not_found")
	case f.AbuseType != "":
		fail(w, "unavailable_for_legal_reasons")
	case f.Availability != "":
		fail(w, f.Availability)
	case refuse != "":
		fail(w, refuse)
	default:
		if _, attach := r.URL.Query()["download"]; attach {
			w.Header().Set("Content-Disposition", "attachment; filename="+f.Name)
		}
		http.ServeContent(w, r, f.Name, time.Time{}, bytes.NewReader(f.Body))
	}
}
