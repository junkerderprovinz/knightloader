package qbittorrent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// answering is a qBittorrent that answers the login and the add as told.
func answering(t *testing.T, loginStatus int, loginBody string, addStatus int, addBody string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(loginStatus)
		io.WriteString(w, loginBody)
	})
	mux.HandleFunc("POST /api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(addStatus)
		io.WriteString(w, addBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func reason(err error) string {
	var qe *Error
	if errors.As(err, &qe) {
		return qe.Reason
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestEveryWayALoginIsRefusedIsNamed(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"accepted", http.StatusOK, "Ok.", ""},
		{"wrong password, 4.x and 5.x", http.StatusOK, "Fails.", "qBittorrent refused the username or password"},
		{"wrong password, later versions", http.StatusUnauthorized, "", "qBittorrent refused the username or password"},
		{"banned", http.StatusForbidden, "Your IP address has been banned", "qBittorrent has banned this address after too many failed logins"},
		{"anything else", http.StatusInternalServerError, "", "qBittorrent refused the login with status 500"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := answering(t, c.status, c.body, http.StatusOK, "Ok.").Login(context.Background())
			if got := reason(err); got != c.want {
				t.Errorf("Login = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEveryWayAnAddIsRefusedIsNamed(t *testing.T) {
	notTaken := "qBittorrent did not take the torrent, which it may have already"
	for _, c := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"accepted", http.StatusOK, "Ok.", ""},
		{"accepted with counts", http.StatusOK, `{"success_count":1,"failure_count":0,"pending_count":0}`, ""},
		{"magnet still pending", http.StatusAccepted, `{"success_count":0,"failure_count":0,"pending_count":1}`, ""},
		{"refused, 4.x and 5.x", http.StatusOK, "Fails.", notTaken},
		{"refused with counts", http.StatusOK, `{"success_count":0,"failure_count":1,"pending_count":0}`, notTaken},
		{"refused, later versions", http.StatusConflict, "", notTaken},
		{"not a torrent", http.StatusUnsupportedMediaType, "", "qBittorrent could not read the torrent file"},
		{"session gone", http.StatusForbidden, "Forbidden", "qBittorrent refused the session"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := answering(t, http.StatusOK, "Ok.", c.status, c.body).Add(context.Background(), Torrent{Magnet: "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a"})
			if got := reason(err); got != c.want {
				t.Errorf("Add = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAnUnreachableQBittorrentIsNamedWithTheCause(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	c, err := New(addr, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Login(context.Background())
	var qe *Error
	if !errors.As(err, &qe) || qe.Reason != "qBittorrent could not be reached" || qe.Err == nil {
		t.Errorf("Login = %v, want it unreachable with the cause", err)
	}
}

func TestAnAddressThatIsNoWebAddressIsRefused(t *testing.T) {
	for _, addr := range []string{"", "qbit:8080", "ftp://qbit"} {
		if _, err := New(addr, "", ""); err == nil {
			t.Errorf("New(%q) made a client", addr)
		}
	}
}

func TestTheSavePathIsTheFolderUnderQBittorrentsDownloads(t *testing.T) {
	downloads := filepath.FromSlash("/mnt/user/downloads")
	in := func(p string) string { return filepath.Join(downloads, filepath.FromSlash(p)) }
	for _, c := range []struct {
		name, dir, root, want string
	}{
		{"the same path", in("tv"), "", in("tv")},
		{"mapped", in("tv/show"), "/data/downloads", "/data/downloads/tv/show"},
		{"mapped with a slash", in("tv"), "/data/downloads/", "/data/downloads/tv"},
		{"the folder itself", downloads, "/data", "/data"},
		{"onto Windows", in("tv"), `D:\Downloads`, `D:\Downloads\tv`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := SavePath(c.dir, downloads, c.root)
			if err != nil || got != c.want {
				t.Errorf("SavePath = %q, %v, want %q", got, err, c.want)
			}
		})
	}
	if _, err := SavePath(filepath.FromSlash("/mnt/user/other/tv"), downloads, "/data"); err == nil {
		t.Error("a folder outside the download folder was mapped")
	}
}
