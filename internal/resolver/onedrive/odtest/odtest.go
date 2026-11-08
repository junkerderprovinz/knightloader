// Package odtest is a stand-in for OneDrive and SharePoint: the redirects a
// share link takes until it has opened a guest session, the REST API that
// lists a shared folder to that guest, and the files. The redirects, the
// guest cookie and the REST answers are shaped as the live services' are for
// a personal OneDrive and for a business SharePoint. The password prompt
// follows what SharePoint shows, since no locked share was at hand.
package odtest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Share is one shared file or folder.
type Share struct {
	// Folder makes the share a folder link.
	Folder bool
	// Business shares come from a SharePoint tenant, the others from a
	// personal OneDrive.
	Business bool
	// Path is the shared item's path on its site, such as
	// /personal/17e16246b3df8f06/Documents/ShuffleNetSeries.
	Path string
	// Gone, Login and Password are shares that were removed, that need a
	// Microsoft account and that are locked with a password.
	Gone, Login, Password bool
}

// Server is a fake OneDrive and SharePoint. URL is the base every share link
// and site of it answers on.
type Server struct {
	URL string
	srv *httptest.Server

	mu     sync.Mutex
	shares map[string]Share
	files  map[string][]byte
	direct map[string][]byte
	asked  []string
}

func New(t *testing.T) *Server {
	s := &Server{shares: map[string]Share{}, files: map[string][]byte{}, direct: map[string][]byte{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.URL = s.srv.URL
	return s
}

func (s *Server) Client() *http.Client { return s.srv.Client() }

// File stores a file under its path on its site.
func (s *Server) File(p string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[p] = body
}

// Direct stores a file an older onedrive.live.com link hands out without a
// redirect, and returns that link.
func (s *Server) Direct(resid string, body []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.direct[resid] = body
	return "https://onedrive.live.com/download?resid=" + resid + "&authkey=!AIehxKx7N7XgoBg"
}

// Add shares an item under token and returns the share link: a 1drv.ms link
// for a personal OneDrive, a tenant's link for SharePoint.
func (s *Server) Add(token string, sh Share) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares[token] = sh
	kind := map[bool]string{true: "f", false: "u"}[sh.Folder]
	if sh.Business {
		return "https://contoso-my.sharepoint.com/:" + kind + ":/g/personal/" + owner(sh.Path) + "/" + token + "?e=CvGohw"
	}
	return "https://1drv.ms/" + kind + "/s!" + token
}

// Asked lists every request, path and query, in order.
func (s *Server) Asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// owner is the second segment of a site path, /personal/OWNER/...
func owner(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.asked = append(s.asked, r.URL.RequestURI())
	s.mu.Unlock()
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/u/s!") || strings.HasPrefix(p, "/f/s!"):
		s.short(w, r)
	case strings.HasPrefix(p, "/:"):
		s.redeem(w, r)
	case p == "/download":
		s.mu.Lock()
		body, ok := s.direct[r.URL.Query().Get("resid")]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="old share.7z"`)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	case strings.Contains(p, "/_api/web/"):
		s.api(w, r)
	case strings.HasSuffix(p, "/_layouts/15/download.aspx"):
		s.file(w, r, r.URL.Query().Get("SourceUrl"), true)
	case strings.Contains(p, "/_layouts/15/") || strings.HasPrefix(p, "/_forms/") || p == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><title>OneDrive</title></html>"))
	default:
		s.file(w, r, p, false)
	}
}

// short is 1drv.ms sending a personal share on to OneDrive.
func (s *Server) short(w http.ResponseWriter, r *http.Request) {
	kind, token, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/s!")
	s.mu.Lock()
	sh, ok := s.shares[token]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := url.Values{"resid": {strings.ToUpper(owner(sh.Path)) + "!892327"}, "migratedtospo": {"true"}, "redeem": {"aHR0cHM6Ly8xZHJ2Lm1z"}}
	if r.URL.Query().Get("download") == "1" {
		q.Set("download", "1")
	}
	if sh.Folder {
		q.Set("ithint", "folder")
	}
	http.Redirect(w, r, "/:"+kind+":/g/personal/"+strings.ToUpper(owner(sh.Path))+"/s!"+token+"?"+q.Encode(), http.StatusMovedPermanently)
}

// fedAuth is the guest session cookie a redeemed share hands out.
const fedAuth = "invented-guest-session"

func (s *Server) redeem(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	token := strings.TrimPrefix(parts[len(parts)-1], "s!")
	s.mu.Lock()
	sh, ok := s.shares[token]
	s.mu.Unlock()
	switch {
	case !ok || sh.Gone:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html><title>Something went wrong</title></html>"))
		return
	case sh.Login:
		http.Redirect(w, r, "/_forms/default.aspx?ReturnUrl="+url.QueryEscape(sh.Path), http.StatusFound)
		return
	case sh.Password:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body><form method="post" action="./guestaccess.aspx"><input name="txtPassword" type="password" id="txtPassword"/><input type="submit" id="btnSubmitPassword"/></form></body></html>`))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "FedAuth", Value: fedAuth, Path: "/", HttpOnly: true})
	site := "/personal/" + owner(sh.Path)
	switch {
	case sh.Folder && sh.Business:
		http.Redirect(w, r, site+"/_layouts/15/onedrive.aspx?"+url.Values{"id": {sh.Path}, "ga": {"1"}}.Encode(), http.StatusFound)
	case sh.Folder:
		http.Redirect(w, r, "/?"+url.Values{"id": {strings.ToUpper(owner(sh.Path)) + "!663"}, "cid": {strings.ToUpper(owner(sh.Path))}, "spopath": {sh.Path}, "listurl": {site + "/Documents"}, "ithint": {"folder"}}.Encode(), http.StatusFound)
	case r.URL.Query().Get("download") != "1":
		http.Redirect(w, r, site+"/_layouts/15/onedrive.aspx?"+url.Values{"id": {sh.Path}, "parent": {path.Dir(sh.Path)}, "ga": {"1"}}.Encode(), http.StatusFound)
	case sh.Business:
		http.Redirect(w, r, (&url.URL{Path: sh.Path, RawQuery: "ga=1"}).String(), http.StatusFound)
	default:
		http.Redirect(w, r, (&url.URL{Path: sh.Path, RawQuery: "redeem=aHR0cHM6Ly8xZHJ2Lm1z"}).String(), http.StatusFound)
	}
}

// guest reports whether a request carries the guest session cookie.
func guest(r *http.Request) bool {
	c, err := r.Cookie("FedAuth")
	return err == nil && c.Value == fedAuth
}

func (s *Server) file(w http.ResponseWriter, r *http.Request, p string, named bool) {
	s.mu.Lock()
	body, ok := s.files[p]
	s.mu.Unlock()
	switch {
	case !guest(r):
		http.Redirect(w, r, "/personal/"+owner(p)+"/_layouts/15/Authenticate.aspx?Source="+url.QueryEscape(p), http.StatusFound)
		return
	case !ok:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	// A personal OneDrive and download.aspx name the file, a business
	// SharePoint answering on the file's own path does not.
	if named || !strings.Contains(r.URL.RawQuery, "ga=1") {
		w.Header().Set("Content-Disposition", `attachment;filename*=utf-8''`+url.PathEscape(path.Base(p))+`;filename="`+path.Base(p)+`"`)
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

var folderCall = regexp.MustCompile(`/_api/web/GetFolderByServerRelativeUrl\('(.*)'\)$`)

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json;odata=nometadata;streaming=true;charset=utf-8")
	m := folderCall.FindStringSubmatch(r.URL.Path)
	switch {
	case !guest(r):
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"odata.error":{"code":"-2147024891, System.UnauthorizedAccessException","message":{"lang":"en-US","value":"Access denied."}}}`))
		return
	case m == nil || r.URL.Query().Get("$expand") != "Files,Folders" || !strings.HasPrefix(r.Header.Get("Accept"), "application/json"):
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"odata.error":{"code":"-2147024809, System.ArgumentException","message":{"lang":"en-US","value":"URL is not web relative"}}}`))
		return
	}
	dir := strings.ReplaceAll(m[1], "''", "'")
	type file struct {
		Name, Length, ServerRelativeUrl string
	}
	type folder struct {
		Name              string
		ItemCount         int
		ServerRelativeUrl string
	}
	var files []file
	subs := map[string]bool{}
	s.mu.Lock()
	for p, body := range s.files {
		rest, ok := strings.CutPrefix(p, dir+"/")
		if !ok {
			continue
		}
		if sub, _, nested := strings.Cut(rest, "/"); nested {
			subs[sub] = true
			continue
		}
		files = append(files, file{Name: rest, Length: strconv.Itoa(len(body)), ServerRelativeUrl: p})
	}
	s.mu.Unlock()
	if len(files) == 0 && len(subs) == 0 {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"odata.error":{"code":"-2147024894, System.IO.FileNotFoundException","message":{"lang":"en-US","value":"File Not Found."}}}`))
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var folders []folder
	for sub := range subs {
		folders = append(folders, folder{Name: sub, ItemCount: 1, ServerRelativeUrl: dir + "/" + sub})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	json.NewEncoder(w).Encode(map[string]any{
		"Files": files, "Folders": folders, "Exists": true, "ItemCount": len(files) + len(folders),
		"Name": path.Base(dir), "ServerRelativeUrl": dir, "UniqueId": "8a5b6c1e-0000-4000-8000-000000000000",
	})
}
