package usenet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// PremiumizeAPI is the address of Premiumize.me's API
// (https://www.premiumize.me/api).
const PremiumizeAPI = "https://www.premiumize.me/api"

// maxFolderDepth bounds how far a finished transfer's folder is walked. A
// release unpacks into a folder or two, never more.
const maxFolderDepth = 4

// Premiumize is one Premiumize.me account's cloud transfers. The API documents
// the source of /transfer/create as a URI or an uploaded file, and an uploaded
// NZB is fetched from Usenet like a torrent is from its swarm.
type Premiumize struct {
	base string
	slot string
	key  string
	hc   *http.Client
}

// NewPremiumize builds the client for the account in slot, speaking to the
// API at base, which is PremiumizeAPI outside tests.
func NewPremiumize(base, slot, key string) *Premiumize {
	return &Premiumize{base: base, slot: slot, key: key, hc: httpx.New(httpx.Options{Timeout: apiTimeout})}
}

func (p *Premiumize) Slot() string      { return p.slot }
func (*Premiumize) Label() string       { return "Premiumize.me" }
func (*Premiumize) SubmitsPerHour() int { return 0 }

// pmAnswer is the status part of every answer. Failures come as HTTP 200 with
// status "error", so the body decides.
type pmAnswer struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// pmBusy are the codes Premiumize.me documents as semi-permanent: the account
// or the service is out of something for now, such as fair-use points or
// running transfers, and the same call works after a wait. pmTransient are
// the ones it documents as worth trying again at once.
var (
	pmBusy = map[string]bool{
		"rate_limit_reached":    true,
		"account_limit_reached": true,
		"service_limit_reached": true,
		"service_down":          true,
		"semi_permanent_error":  true,
	}
	pmTransient = map[string]bool{
		"link_generation_failed": true,
		"transient_error":        true,
	}
)

func (a pmAnswer) err(call string) error {
	// item/details answers a bare item without a status.
	if a.Status == "success" || a.Status == "" {
		return nil
	}
	msg := strings.TrimSpace(a.Message)
	if msg == "" {
		msg = a.Code
	}
	code := strings.ToLower(a.Code)
	switch {
	case pmBusy[code] || strings.Contains(strings.ToLower(msg), "rate limit"):
		return fmt.Errorf("premiumize %s: %s: %w", call, msg, ErrBusy)
	case pmTransient[code]:
		return unreachable{fmt.Errorf("premiumize %s: %s", call, msg)}
	case code == "not_found":
		return fmt.Errorf("premiumize %s: %s: %w", call, msg, ErrGone)
	}
	if msg == "" {
		msg = "the call failed and Premiumize.me named no reason"
	}
	return fmt.Errorf("premiumize %s: %s", call, msg)
}

func (p *Premiumize) send(req *http.Request, call string, out any) error {
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return unreachable{fmt.Errorf("premiumize %s: %w", call, errors.Unwrap(err))}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("premiumize %s: %w", call, ErrBusy)
	case resp.StatusCode >= 500:
		return unreachable{fmt.Errorf("premiumize %s: %s", call, resp.Status)}
	}
	var status pmAnswer
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("premiumize %s: %s", call, resp.Status)
	}
	if err := status.err(call); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (p *Premiumize) get(ctx context.Context, path string, q url.Values, out any) error {
	u := p.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return p.send(req, path, out)
}

func (p *Premiumize) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return p.send(req, path, out)
}

// Submit uploads the NZB as the source of a new transfer.
func (p *Premiumize) Submit(ctx context.Context, name string, nzb []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="src"; filename="` + quoteEscaper.Replace(nzbFilename(name)) + `"`},
		"Content-Type":        {"application/x-nzb"},
	})
	if err != nil {
		return "", err
	}
	if _, err := part.Write(nzb); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/transfer/create", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var created struct {
		ID string `json:"id"`
	}
	if err := p.send(req, "/transfer/create", &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("premiumize /transfer/create: the answer carries no transfer id")
	}
	return created.ID, nil
}

// pmTransfer is one entry of /transfer/list. FolderID is the folder a finished
// transfer was put into, and FileID is set as well when it left a single
// file, in which case the folder is the destination rather than its own.
type pmTransfer struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Message  string  `json:"message"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	FolderID string  `json:"folder_id"`
	FileID   string  `json:"file_id"`
}

// transfers reads /transfer/list, which has no lookup by id.
func (p *Premiumize) transfers(ctx context.Context) (map[string]pmTransfer, error) {
	var list struct {
		Transfers []pmTransfer `json:"transfers"`
	}
	if err := p.get(ctx, "/transfer/list", nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]pmTransfer, len(list.Transfers))
	for _, t := range list.Transfers {
		out[t.ID] = t
	}
	return out, nil
}

func (p *Premiumize) transfer(ctx context.Context, id string) (pmTransfer, error) {
	all, err := p.transfers(ctx)
	if err != nil {
		return pmTransfer{}, err
	}
	tr, ok := all[id]
	if !ok {
		return pmTransfer{}, fmt.Errorf("premiumize /transfer/list: transfer %s: %w", id, ErrGone)
	}
	return tr, nil
}

// Status reads the transfer list once for all of ids, and for a finished
// transfer the files it left in the cloud. Premiumize.me names no size while
// a transfer runs, only its progress. A finished transfer whose files cannot
// be read fails on its own, so the others still come through.
func (p *Premiumize) Status(ctx context.Context, ids []string) (map[string]Status, error) {
	all, err := p.transfers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Status, len(ids))
	for _, id := range ids {
		tr, ok := all[id]
		if !ok {
			continue
		}
		st := Status{Phase: PhaseFetching, Name: tr.Name, Progress: tr.Progress}
		switch strings.ToLower(tr.Status) {
		case "finished", "seeding":
			files, err := p.transferFiles(ctx, tr)
			switch {
			case err == nil:
				st.Phase, st.Files, st.Progress = PhaseReady, files, 1
				for _, f := range files {
					st.Size += f.Size
				}
			case errors.Is(err, ErrBusy):
				return nil, err
			case temporary(err):
				// Its files are read again next round.
			default:
				st.Phase, st.Reason = PhaseFailed, err.Error()
			}
		case "error", "deleted", "banned", "timeout":
			reason := strings.TrimSpace(tr.Message)
			if reason == "" {
				reason = tr.Status
			}
			st.Phase, st.Reason = PhaseFailed, "Premiumize.me: "+reason
		}
		out[id] = st
	}
	return out, nil
}

// pmItem is a file or folder in the cloud.
type pmItem struct {
	pmAnswer
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Link string `json:"link"`
}

// transferFiles lists what a finished transfer left: one file, or a folder
// that is walked for the files inside it.
func (p *Premiumize) transferFiles(ctx context.Context, tr pmTransfer) ([]File, error) {
	if tr.FileID != "" {
		it, err := p.item(ctx, tr.FileID)
		if err != nil {
			return nil, err
		}
		return []File{{ID: it.ID, Name: it.Name, Size: it.Size}}, nil
	}
	if tr.FolderID == "" {
		// Premiumize.me leaves out both for a transfer it routed to an external
		// cloud connected to the account.
		return nil, fmt.Errorf("premiumize: transfer %s went to an external cloud linked to the account, and its files cannot be fetched from there", tr.ID)
	}
	var files []File
	if err := p.walk(ctx, tr.FolderID, "", 0, &files); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("premiumize: transfer %s finished with an empty folder", tr.ID)
	}
	return files, nil
}

// folder reads one cloud folder: its name and what is in it.
func (p *Premiumize) folder(ctx context.Context, id string) (string, []pmItem, error) {
	var list struct {
		Name    string   `json:"name"`
		Content []pmItem `json:"content"`
	}
	if err := p.get(ctx, "/folder/list", url.Values{"id": {id}}, &list); err != nil {
		return "", nil, err
	}
	return list.Name, list.Content, nil
}

func (p *Premiumize) walk(ctx context.Context, folder, dir string, depth int, out *[]File) error {
	_, content, err := p.folder(ctx, folder)
	if err != nil {
		return err
	}
	for _, it := range content {
		switch {
		case it.Type == "folder" && depth+1 < maxFolderDepth:
			sub := it.Name
			if dir != "" {
				sub = dir + "/" + it.Name
			}
			if err := p.walk(ctx, it.ID, sub, depth+1, out); err != nil {
				return err
			}
		case it.Type == "file":
			*out = append(*out, File{ID: it.ID, Name: it.Name, Dir: dir, Size: it.Size})
		}
	}
	return nil
}

func (p *Premiumize) item(ctx context.Context, id string) (pmItem, error) {
	var it pmItem
	if err := p.get(ctx, "/item/details", url.Values{"id": {id}}, &it); err != nil {
		return pmItem{}, err
	}
	if it.ID == "" {
		return pmItem{}, fmt.Errorf("premiumize /item/details: item %s: %w", id, ErrGone)
	}
	return it, nil
}

// Link reads the file's address from /item/details. It does not expire while
// the file is in the cloud.
func (p *Premiumize) Link(ctx context.Context, _, fileID string) (string, error) {
	it, err := p.item(ctx, fileID)
	if err != nil {
		return "", err
	}
	if it.Link == "" {
		return "", fmt.Errorf("premiumize /item/details: item %s carries no address", fileID)
	}
	return it.Link, nil
}

// Delete removes the transfer and what it left in the cloud, which otherwise
// counts against the account's storage. A folder goes only when it carries the
// transfer's own name, so the folder a single file was put into is never
// deleted with everything else in it.
func (p *Premiumize) Delete(ctx context.Context, id string) error {
	tr, err := p.transfer(ctx, id)
	if err != nil {
		return err
	}
	if err := p.post(ctx, "/transfer/delete", url.Values{"id": {id}}, nil); err != nil {
		return err
	}
	if tr.FileID != "" {
		return p.post(ctx, "/item/delete", url.Values{"id": {tr.FileID}}, nil)
	}
	if tr.FolderID == "" {
		return nil
	}
	name, _, err := p.folder(ctx, tr.FolderID)
	if err != nil || name != tr.Name {
		return err
	}
	return p.post(ctx, "/folder/delete", url.Values{"id": {tr.FolderID}}, nil)
}
