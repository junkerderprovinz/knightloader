// Package gdtest is a stand-in for Google Drive: its download server, its
// folder pages, the files API the folder pages call and the export of Google
// documents. The download answers, the virus scan warning and the folder
// page's key are shaped as the live site's are. The quota page follows what
// JDownloader's plugin and gdown look for, since the live site shows it only
// for a file downloaded too often.
package gdtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Key is the API key the folder pages carry.
const Key = "AIzaInventedFilesKeyOfTheGdtestFake0000"

// The MIME types Drive gives a folder and the Google documents it exports.
const (
	Folder       = "application/vnd.google-apps.folder"
	Document     = "application/vnd.google-apps.document"
	Spreadsheet  = "application/vnd.google-apps.spreadsheet"
	Presentation = "application/vnd.google-apps.presentation"
	Form         = "application/vnd.google-apps.form"
)

// Item is a file, a folder or a Google document the server holds.
type Item struct {
	Name string
	// Mime is the item's MIME type, a plain file's when empty.
	Mime string
	Body []byte
	// Children are the ids of a folder's items.
	Children []string
	// Big files come with the virus scan warning first.
	Big bool
	// Quota files have had their download quota used up.
	Quota bool
	// Private items are shared with some Google accounts only.
	Private bool
	// ResourceKey is the key an older shared link has to name.
	ResourceKey string
	// Target makes the item a shortcut to another item.
	Target string
}

// Server is a fake Google Drive. URL is the base of every one of its sites.
type Server struct {
	URL string
	srv *httptest.Server

	mu    sync.Mutex
	items map[string]Item
	// page is how many items one answer of the files API holds.
	page  int
	asked []string
}

func New(t *testing.T) *Server {
	s := &Server{items: map[string]Item{}, page: 50}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// Add stores an item under its id and returns the link Drive shares it with.
func (s *Server) Add(id string, it Item) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = it
	link := "https://drive.google.com/file/d/" + id + "/view?usp=sharing"
	switch it.Mime {
	case Folder:
		link = "https://drive.google.com/drive/folders/" + id + "?usp=sharing"
	case Document:
		link = "https://docs.google.com/document/d/" + id + "/edit?usp=sharing"
	}
	if it.ResourceKey != "" {
		link += "&resourcekey=" + it.ResourceKey
	}
	return link
}

// Page sets how many items an answer of the files API holds.
func (s *Server) Page(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.page = n
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
	case p == "/download":
		s.download(w, r)
	case strings.HasPrefix(p, "/drive/folders/"):
		s.folderPage(w, r, strings.TrimPrefix(p, "/drive/folders/"))
	case p == "/drive/v2beta/files":
		s.files(w, r)
	case strings.HasPrefix(p, "/document/d/") && strings.HasSuffix(p, "/export"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/document/d/"), "/export")
		http.Redirect(w, r, "/export/"+id+"?format="+r.URL.Query().Get("format"), http.StatusTemporaryRedirect)
	case strings.HasPrefix(p, "/export/"):
		s.export(w, r, strings.TrimPrefix(p, "/export/"))
	case p == "/ServiceLogin":
		w.Write([]byte("<html><title>Sign in - Google Accounts</title></html>"))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) item(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	return it, ok
}

// keyed reports whether a request names the item's resource key, in the
// query or in the header the files API reads.
func keyed(r *http.Request, id string, it Item) bool {
	return it.ResourceKey == "" || r.URL.Query().Get("resourcekey") == it.ResourceKey ||
		r.Header.Get("X-Goog-Drive-Resource-Keys") == id+"/"+it.ResourceKey
}

const virusPage = `<!DOCTYPE html><html><head><title>Google Drive - Virus scan warning</title><meta http-equiv="content-type" content="text/html; charset=utf-8"/></head><body><div class="uc-main"><div id="uc-text"><p class="uc-warning-caption">Google Drive can't scan this file for viruses.</p><p class="uc-warning-subcaption"><span class="uc-name-size"><a href="/open?id=%[1]s">%[2]s</a> (476M)</span> is too large for Google to scan for viruses. Would you still like to download this file?</p><form id="download-form" action="%[3]s/download" method="get"><input type="submit" id="uc-download-link" class="goog-inline-block jfk-button jfk-button-action" value="Download anyway"/><input type="hidden" name="id" value="%[1]s"><input type="hidden" name="export" value="download"><input type="hidden" name="confirm" value="t"><input type="hidden" name="uuid" value="0b5c2d10-0000-4000-8000-00000000c0f1"></form></div></div></body></html>`

const quotaPage = `<!DOCTYPE html><html><head><title>Google Drive - Quota exceeded</title></head><body><div class="uc-main"><div id="uc-text"><p class="uc-error-caption">Sorry, you can't view or download this file at this time.</p><p class="uc-error-subcaption">Too many users have viewed or downloaded this file recently. Please try accessing the file again later. If the file you are trying to access is particularly large or is shared with many people, it may take up to 24 hours to be able to view or download the file. If you still can't access a file after 24 hours, contact your domain administrator.</p></div></div></body></html>`

// folderPage is the part of a live folder page the client reads: the title,
// which names the folder ahead of a dash, and the key the files API takes, the
// first of a pair of keys. The live title uses an en dash and a no-break space
// inside the product's name.
const folderPage = `<!DOCTYPE html><html><head><title>%s %s Google%sDrive</title></head><body><script nonce="x">window['_DRIVE_ivd'] = '\x5b\x5d';</script><script>_initProjectorSettings([null,[1,3],201326592,1048576,null,10,"%s","AIzaInventedSecondKeyOfTheGdtestPage000",null,null,null,null,0,"https://drivemetadata.clients6.google.com",null]);</script></body></html>`

var enDash, nbsp = string(rune(0x2013)), string(rune(0xa0))

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	it, ok := s.item(id)
	switch {
	case !ok || !keyed(r, id, it) || it.Mime != "":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html><title>Error 404 (Not Found)!!1</title></html>"))
	case it.Private:
		http.Redirect(w, r, s.URL+"/ServiceLogin?continue=https://drive.usercontent.google.com/download", http.StatusFound)
	case it.Quota:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(quotaPage))
	case it.Big && r.URL.Query().Get("uuid") == "":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, virusPage, id, it.Name, s.URL)
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+it.Name+`"`)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(it.Body))
	}
}

func (s *Server) export(w http.ResponseWriter, r *http.Request, id string) {
	it, ok := s.item(id)
	if !ok || r.URL.Query().Get("format") != "docx" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", `attachment; filename="`+it.Name+`.docx"`)
	w.Write(it.Body)
}

func (s *Server) folderPage(w http.ResponseWriter, r *http.Request, id string) {
	it, ok := s.item(id)
	switch {
	case !ok || it.Mime != Folder || !keyed(r, id, it):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html><title>Error 404 (Not Found)!!1</title></html>"))
		return
	case it.Private:
		http.Redirect(w, r, s.URL+"/ServiceLogin?continue=https://drive.google.com/drive/folders/"+id, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, folderPage, it.Name, enDash, nbsp, Key)
}

var parentPattern = regexp.MustCompile(`^trashed = false and '([^']+)' in parents$`)

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	m := parentPattern.FindStringSubmatch(q.Get("q"))
	if q.Get("key") != Key || r.Header.Get("Referer") != "https://drive.google.com/" || m == nil {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":403,"message":"The request is missing a valid API key.","status":"PERMISSION_DENIED"}}`))
		return
	}
	folder, ok := s.item(m[1])
	if !ok || !keyed(r, m[1], folder) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":404,"message":"File not found: ` + m[1] + `."}}`))
		return
	}
	s.mu.Lock()
	page := s.page
	s.mu.Unlock()
	from, _ := strconv.Atoi(q.Get("pageToken"))
	to := min(from+page, len(folder.Children))
	var items []map[string]any
	for _, id := range folder.Children[from:to] {
		it, _ := s.item(id)
		e := map[string]any{"kind": "drive#file", "id": id, "title": it.Name, "mimeType": it.Mime}
		if it.Mime == "" {
			e["mimeType"] = "application/octet-stream"
			e["fileSize"] = strconv.Itoa(len(it.Body))
		}
		if it.ResourceKey != "" {
			e["resourceKey"] = it.ResourceKey
		}
		if it.Target != "" {
			target, _ := s.item(it.Target)
			mime := target.Mime
			if mime == "" {
				mime = "application/octet-stream"
			}
			e["mimeType"] = "application/vnd.google-apps.shortcut"
			e["shortcutDetails"] = map[string]any{"targetId": it.Target, "targetMimeType": mime}
		}
		items = append(items, e)
	}
	out := map[string]any{"kind": "drive#fileList", "incompleteSearch": false, "items": items}
	if to < len(folder.Children) {
		out["nextPageToken"] = strconv.Itoa(to)
	}
	json.NewEncoder(w).Encode(out)
}
