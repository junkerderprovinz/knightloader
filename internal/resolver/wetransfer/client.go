package wetransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
)

// DefaultSite is where WeTransfer's website and API answer.
const DefaultSite = "https://wetransfer.com"

// maxConns is the most connections one download opens, as JDownloader's
// plugin opens one.
const maxConns = 1

// maxAnswer caps how much of an API answer is read.
const maxAnswer = 4 << 20

// Client asks WeTransfer's API for transfers and their download links.
type Client struct {
	hc   *http.Client
	site string
	// short stands in for the host of a short link, so tests can point the
	// client at a fake. Empty outside tests.
	short string
}

// NewClient talks to site, DefaultSite outside tests, through hc.
func NewClient(site string, hc *http.Client) *Client {
	return &Client{hc: hc, site: strings.TrimSuffix(site, "/")}
}

// expand reads a short link's redirect for the transfer it leads to. The
// redirect is not followed, only its path is read.
func (c *Client) expand(ctx context.Context, l link) (link, error) {
	if l.short == "" {
		return l, nil
	}
	u, err := url.Parse(l.short)
	if err != nil {
		return link{}, err
	}
	if c.short != "" {
		u, err = url.Parse(c.short + u.Path)
		if err != nil {
			return link{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return link{}, err
	}
	hc := *c.hc
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return link{}, err
	}
	resp.Body.Close()
	to, err := resp.Location()
	if err != nil {
		return link{}, hoster.Gone("wetransfer")
	}
	out, ok := parsePath(to)
	if !ok {
		return link{}, hoster.Gone("wetransfer")
	}
	return out, nil
}

// item is one file of a transfer. Its name carries the folders it was sent
// in.
type item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// transfer is what prepare-download says about a transfer. Items stay empty
// for a locked transfer asked without its password.
type transfer struct {
	State  string `json:"state"`
	Locked bool   `json:"password_protected"`
	Items  []item `json:"items"`
	Size   int64  `json:"size"`
	Zip    string `json:"recommended_filename"`
}

// apiError is what the API answers a request it turns down with.
type apiError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

var expired = &hoster.Refusal{Reason: core.ReasonGone, Msg: "wetransfer: the transfer has expired or was deleted"}

func passwordRefusal(password string) error {
	if password == "" {
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "wetransfer: the transfer is protected by a password, which goes in the link password of the add-links form"}
	}
	return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "wetransfer: the link password does not open the transfer"}
}

// call posts body to one of the transfer's API calls and decodes the answer.
func (c *Client) call(ctx context.Context, l link, name string, body map[string]any, password string, out any) error {
	body["security_hash"] = l.hash
	if l.recipient != "" {
		body["recipient_id"] = l.recipient
	}
	if password != "" {
		body["password"] = password
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.site+"/api/v4/transfers/"+l.id+"/"+name, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", DefaultSite)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return err
	}
	// A successful answer has no error fields and leaves e empty.
	var e apiError
	_ = json.Unmarshal(answer, &e)
	switch {
	case resp.StatusCode == http.StatusNotFound || e.Error == "invalid_transfer":
		return expired
	case e.Message == "invalid_transfer_password":
		return passwordRefusal(password)
	case e.Message == "No download access to this Transfer":
		return &hoster.Refusal{Reason: core.ReasonAuth, Msg: "wetransfer: the sender wants each recipient to confirm their email on WeTransfer's website first"}
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("wetransfer: the API answered %d: %s", resp.StatusCode, e.Message+e.Error)
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("wetransfer: the API answered in a shape it does not use: %w", err)
	}
	return nil
}

// prepare asks what a transfer holds.
func (c *Client) prepare(ctx context.Context, l link, password string) (transfer, error) {
	var t transfer
	if err := c.call(ctx, l, "prepare-download", map[string]any{}, password, &t); err != nil {
		return transfer{}, err
	}
	if t.State != "downloadable" {
		return transfer{}, expired
	}
	return t, nil
}

var _ hoster.Unlocker = (*Client)(nil)

// Unlock returns the direct link of one file of a transfer, or of the zip of
// the whole transfer.
func (c *Client) Unlock(ctx context.Context, raw, password string) (hoster.Link, error) {
	l, ok := parse(raw)
	if !ok {
		return hoster.Link{}, fmt.Errorf("wetransfer: %s is not a link to a transfer", raw)
	}
	l, err := c.expand(ctx, l)
	if err != nil {
		return hoster.Link{}, err
	}
	t, err := c.prepare(ctx, l, password)
	if err != nil {
		return hoster.Link{}, err
	}
	if t.Locked && len(t.Items) == 0 {
		return hoster.Link{}, passwordRefusal(password)
	}
	body := map[string]any{"intent": "entire_transfer"}
	got := hoster.Link{Name: t.Zip, Size: t.Size, Conns: maxConns}
	if l.file != "" {
		found := false
		for _, it := range t.Items {
			if it.ID == l.file {
				got.Name, got.Size, found = path.Base(it.Name), it.Size, true
			}
		}
		if !found {
			return hoster.Link{}, hoster.Gone("wetransfer")
		}
		body = map[string]any{"intent": "single_file", "file_ids": []string{l.file}}
	}
	var d struct {
		Link string `json:"direct_link"`
	}
	if err := c.call(ctx, l, "download", body, password, &d); err != nil {
		return hoster.Link{}, err
	}
	if d.Link == "" {
		return hoster.Link{}, fmt.Errorf("wetransfer: the API handed out no download link")
	}
	got.URL = d.Link
	return got, nil
}
