// Package qbittorrent hands a torrent whose files are already in place to an
// external qBittorrent to seed, through three calls of its Web API v2: logging
// in, reading its version and adding the torrent.
//
// qBittorrent's versions name some of the add call's fields differently
// (paused or stopped, skip_checking or seedMode) and each ignores a field it
// does not know, so the add sends every spelling.
package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Error is a call qBittorrent did not answer as asked. Reason says so in a
// plain sentence, for a task's row; Err, when there is one, is the cause.
type Error struct {
	Reason string
	Err    error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Reason
	}
	return e.Reason + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Client is one session with a qBittorrent Web UI.
type Client struct {
	base       string
	user, pass string
	http       *http.Client
}

// New makes a client for the Web UI at addr, which may carry a path when a
// reverse proxy serves it under one.
func New(addr, user, pass string) (*Client, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, &Error{Reason: "no qBittorrent address is set"}
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &Error{Reason: "the qBittorrent address is not an http or https address"}
	}
	// The session is the SID cookie the login sets.
	jar, _ := cookiejar.New(nil)
	return &Client{
		base: strings.TrimRight(u.String(), "/") + "/api/v2/",
		user: user,
		pass: pass,
		http: httpx.New(httpx.Options{Jar: jar}),
	}, nil
}

// answer is a response read whole; qBittorrent answers these calls in a few
// bytes.
type answer struct {
	status int
	body   string
}

func (c *Client) call(ctx context.Context, method, endpoint, contentType string, body io.Reader) (answer, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+endpoint, body)
	if err != nil {
		return answer{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return answer{}, &Error{Reason: "qBittorrent could not be reached", Err: err}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return answer{}, &Error{Reason: "qBittorrent broke off its answer", Err: err}
	}
	return answer{status: resp.StatusCode, body: strings.TrimSpace(string(b))}, nil
}

// Login opens the session. qBittorrent 4.x and 5.x answer a wrong password
// with "Fails.", later versions with 401, and all of them 403 to an address
// banned after too many failed logins.
func (c *Client) Login(ctx context.Context) error {
	form := url.Values{"username": {c.user}, "password": {c.pass}}
	a, err := c.call(ctx, http.MethodPost, "auth/login", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	switch {
	case err != nil:
		return err
	case a.status == http.StatusForbidden:
		return &Error{Reason: "qBittorrent has banned this address after too many failed logins"}
	case a.status == http.StatusUnauthorized, a.status == http.StatusOK && a.body == "Fails.":
		return &Error{Reason: "qBittorrent refused the username or password"}
	case a.status != http.StatusOK:
		return refused("the login", a)
	}
	return nil
}

// Version is the qBittorrent version, such as "v5.0.4".
func (c *Client) Version(ctx context.Context) (string, error) {
	a, err := c.call(ctx, http.MethodGet, "app/version", "", nil)
	if err != nil {
		return "", err
	}
	if a.status != http.StatusOK {
		return "", refused("the version request", a)
	}
	return a.body, nil
}

// Torrent is a torrent to seed from files already in SavePath.
type Torrent struct {
	// File is the .torrent's bytes, or nil for Magnet.
	File   []byte
	Magnet string
	// SavePath is the folder the torrent lands in, as qBittorrent sees it: the
	// one holding its file, or the one holding its folder of several files.
	SavePath string
	// Category files the torrent in qBittorrent, which makes it on first use.
	Category string
	// Folder says the torrent has several files, in a folder of its name.
	Folder bool
}

// Add hands t to qBittorrent to seed. It skips the hash check, since the
// caller has checked the files, and starts the torrent at once.
func (c *Client) Add(ctx context.Context, t Torrent) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fields := [][2]string{
		{"savepath", t.SavePath},
		{"autoTMM", "false"},
		{"contentLayout", "Original"},
		{"skip_checking", "true"},
		{"seedMode", "true"},
		{"paused", "false"},
		{"stopped", "false"},
	}
	// Before 4.3.2 a torrent of several files lands in a folder of its name
	// only when asked.
	if t.Folder {
		fields = append(fields, [2]string{"root_folder", "true"})
	}
	if t.Category != "" {
		fields = append(fields, [2]string{"category", t.Category})
	}
	if t.File == nil {
		fields = append(fields, [2]string{"urls", t.Magnet})
	}
	for _, f := range fields {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	if t.File != nil {
		part, err := w.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="torrents"; filename="seed.torrent"`},
			"Content-Type":        {"application/x-bittorrent"},
		})
		if err != nil {
			return err
		}
		if _, err := part.Write(t.File); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	a, err := c.call(ctx, http.MethodPost, "torrents/add", w.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	switch {
	case a.status == http.StatusUnsupportedMediaType:
		return &Error{Reason: "qBittorrent could not read the torrent file"}
	case a.status == http.StatusConflict, a.status == http.StatusOK && (a.body == "Fails." || addedNone(a.body)):
		return &Error{Reason: "qBittorrent did not take the torrent, which it may have already"}
	case a.status < 200 || a.status > 299:
		return refused("the torrent", a)
	}
	return nil
}

// addedNone reports an answer in the JSON later versions give that counts
// nothing added and nothing pending.
func addedNone(body string) bool {
	var r struct {
		Success *int `json:"success_count"`
		Pending int  `json:"pending_count"`
	}
	return json.Unmarshal([]byte(body), &r) == nil && r.Success != nil && *r.Success == 0 && r.Pending == 0
}

func refused(what string, a answer) error {
	if a.status == http.StatusForbidden {
		return &Error{Reason: "qBittorrent refused the session"}
	}
	return &Error{Reason: fmt.Sprintf("qBittorrent refused %s with status %d", what, a.status)}
}

// SavePath is dir as qBittorrent sees it. With root empty that is dir itself;
// otherwise downloads, where KnightLoader has its download folder, is root
// there. A dir outside downloads has no known place in qBittorrent.
func SavePath(dir, downloads, root string) (string, error) {
	if root == "" {
		return dir, nil
	}
	rel, err := filepath.Rel(downloads, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("its folder is outside the download folder, so qBittorrent's path to it is not known")
	}
	// The two may run on different systems, and the path is written the way
	// qBittorrent's side writes it.
	sep := "/"
	if !strings.Contains(root, "/") && strings.Contains(root, `\`) {
		sep = `\`
	}
	root = strings.TrimRight(root, `/\`)
	if rel == "." {
		if root == "" {
			return sep, nil
		}
		return root, nil
	}
	return root + sep + strings.ReplaceAll(filepath.ToSlash(rel), "/", sep), nil
}
