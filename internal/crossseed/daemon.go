package crossseed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Daemon is the user's cross-seed daemon, which searches and injects on its
// own once it is handed a torrent.
type Daemon struct {
	base, key string
	http      *http.Client
}

// NewDaemon is a client for the daemon at base, such as
// http://cross-seed:2468, with its API key.
func NewDaemon(base, key string) *Daemon {
	return &Daemon{base: strings.TrimRight(base, "/"), key: key, http: httpx.New(httpx.Options{Timeout: 30 * time.Second})}
}

// Announce hands the daemon the torrent with infoHash through its webhook,
// with path, its file or folder on disk, for a daemon that cannot reach the
// torrent client and searches by the files themselves.
func (d *Daemon) Announce(ctx context.Context, infoHash, path string) error {
	form := url.Values{"infoHash": {infoHash}}
	if path != "" {
		form.Set("path", path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/api/webhook", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Api-Key", d.key)
	return d.do(req)
}

// Ping asks whether the daemon is there, without its key.
func (d *Daemon) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/api/ping", nil)
	if err != nil {
		return err
	}
	return d.do(req)
}

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
		return fmt.Errorf("the cross-seed daemon answered %s", resp.Status)
	}
	return nil
}
