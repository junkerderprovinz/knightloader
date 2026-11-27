package crossseed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/fsretry"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Daemon is the user's cross-seed daemon, which searches on its own once it
// is handed a torrent.
//
// KnightLoader's qBittorrent is private to it, so the daemon can neither read
// a torrent from there nor inject a twin into it. A folder both reach stands
// in: the daemon reads the torrents from its torrents folder as torrentDir
// and, with action save, leaves the twins it finds in found as outputDir.
type Daemon struct {
	base, key, folder string
	http              *http.Client
}

// NewDaemon is a client for the daemon at base, such as
// http://cross-seed:2468, with its API key and the folder it shares, empty
// for none.
func NewDaemon(base, key, folder string) *Daemon {
	return &Daemon{base: strings.TrimRight(base, "/"), key: key, folder: folder, http: httpx.New(httpx.Options{Timeout: 30 * time.Second})}
}

// Shares reports whether the daemon shares a folder.
func (d *Daemon) Shares() bool { return d.folder != "" }

func (d *Daemon) kept(hash string) string {
	return filepath.Join(d.folder, "torrents", strings.ToLower(hash)+".torrent")
}

// Hand has the daemon search for src through its webhook, which takes an
// info hash or the path of the files but refuses both. A torrent goes by its
// hash once its .torrent is in the shared folder, where the daemon reads it.
// Otherwise the daemon gets the path and has to see the files under it.
func (d *Daemon) Hand(ctx context.Context, src Source) error {
	form := url.Values{}
	if d.folder != "" && src.Torrent != nil {
		path := d.kept(src.Hash)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := fsretry.Replace(path, src.Torrent, 0o644); err != nil {
			return err
		}
		form.Set("infoHash", strings.ToLower(src.Hash))
	} else {
		form.Set("path", filepath.Join(src.Dir, src.Name))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/api/webhook", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Api-Key", d.key)
	return d.do(req)
}

// Forget takes the torrent with hash out of the shared folder, so the
// daemon searches for it no more.
func (d *Daemon) Forget(hash string) error {
	if d.folder == "" {
		return nil
	}
	if err := os.Remove(d.kept(hash)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Finds are the .torrent files the daemon saved in the shared folder.
func (d *Daemon) Finds() ([]string, error) {
	if d.folder == "" {
		return nil, nil
	}
	dir := filepath.Join(d.folder, "found")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".torrent") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// savedName is how cross-seed names a twin it saves:
// [type][tracker]name[info hash].torrent, the hash left out by older
// versions.
var savedName = regexp.MustCompile(`(?i)^\[(.+?)\]\[(.+?)\]`)

// trackerOfFind is the name of the tracker a saved twin is from, as
// cross-seed put it in the file's name, else the host it announces to.
func trackerOfFind(file string, torrent []byte) string {
	if m := savedName.FindStringSubmatch(filepath.Base(file)); m != nil {
		return m[2]
	}
	if v, err := bencode.Decode(torrent); err == nil {
		if top, ok := v.(map[string]any); ok {
			if a, ok := top["announce"].([]byte); ok {
				if u, err := url.Parse(string(a)); err == nil && u.Hostname() != "" {
					return u.Hostname()
				}
			}
		}
	}
	return DaemonKey
}

// Check asks whether the daemon is there and takes the API key. The webhook
// is sent with nothing to search for: cross-seed answers that with 400 once
// the key passed, and with 401 when it did not.
func (d *Daemon) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/api/webhook", strings.NewReader(""))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Api-Key", d.key)
	err = d.do(req)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusBadRequest {
		return nil
	}
	return err
}

// Prepare makes the two folders of the shared folder, since cross-seed will
// not start while its torrentDir is missing.
func (d *Daemon) Prepare() error {
	if d.folder == "" {
		return nil
	}
	for _, sub := range []string{"torrents", "found"} {
		if err := os.MkdirAll(filepath.Join(d.folder, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string { return "the cross-seed daemon answered " + e.status }

func (d *Daemon) do(req *http.Request) error {
	resp, err := d.http.Do(req)
	if err != nil {
		// The address is in the settings already, and the request line only
		// makes the reason longer.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("the cross-seed daemon could not be reached: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("the cross-seed daemon refused the API key")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return &statusError{code: resp.StatusCode, status: resp.Status}
	}
	return nil
}
