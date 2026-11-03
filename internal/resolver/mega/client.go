package mega

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultAPI is the address of MEGA's API.
const DefaultAPI = "https://g.api.mega.co.nz"

// maxAnswer caps how much of an API answer is read. A folder listing is the
// largest, at a few hundred bytes per node.
const maxAnswer = 64 << 20

// folderTTL is how long a folder's listing is reused. Staging a folder and
// starting its files asks for the same listing once per file.
const folderTTL = 10 * time.Minute

// Client speaks the part of MEGA's API that public links need. It logs in to
// nothing: a file link and a folder link each carry their own key.
type Client struct {
	api  string
	http *http.Client
	seq  atomic.Uint64

	mu      sync.Mutex
	folders map[string]cachedTree
}

type cachedTree struct {
	tree *tree
	at   time.Time
}

// NewClient talks to the API at api through hc.
func NewClient(api string, hc *http.Client) *Client {
	c := &Client{api: strings.TrimRight(api, "/"), http: hc, folders: map[string]cachedTree{}}
	// MEGA wants the sequence number to differ between clients.
	c.seq.Store(uint64(time.Now().UnixNano() & 0xffffffff))
	return c
}

// APIError is one of the negative numbers MEGA answers a request with.
type APIError int

const (
	errAgain       APIError = -3
	errRateLimit   APIError = -4
	errNotFound    APIError = -9
	errAccess      APIError = -11
	errKey         APIError = -14
	errBlocked     APIError = -16
	errOverQuota   APIError = -17
	errUnavailable APIError = -18
)

func (e APIError) Error() string {
	switch e {
	case errNotFound:
		return "mega: the file or folder is not there any more"
	case errBlocked:
		return "mega: MEGA has taken this file or folder down"
	case errAccess:
		return "mega: MEGA refuses access to this link"
	case errKey:
		return errBadKey.Error()
	case errAgain, errUnavailable:
		return "mega: MEGA is temporarily unavailable"
	}
	return fmt.Sprintf("mega: MEGA answered with error %d", int(e))
}

// Gone reports whether err means the link is dead for good.
func Gone(err error) bool {
	var e APIError
	return errors.As(err, &e) && (e == errNotFound || e == errBlocked)
}

// QuotaError is MEGA's transfer quota for this address used up. Wait is how
// long until it is back, zero when MEGA did not say.
type QuotaError struct {
	Wait time.Duration
}

func (e *QuotaError) Error() string {
	if e.Wait <= 0 {
		return "mega: the transfer quota of this address is used up for now"
	}
	return "mega: the transfer quota of this address is used up, it comes back in " + e.Wait.Round(time.Minute).String()
}

// apiError turns an error number into an error, with the time left that MEGA
// sends beside a spent quota.
func apiError(code int, timeLeft int64) error {
	switch e := APIError(code); e {
	case errOverQuota, errRateLimit:
		return &QuotaError{Wait: time.Duration(timeLeft) * time.Second}
	default:
		return e
	}
}

// call sends one command and decodes its answer into out. folder is the
// public folder handle the command is about, or "".
func (c *Client) call(ctx context.Context, folder string, cmd, out any) error {
	body, err := json.Marshal([]any{cmd})
	if err != nil {
		return err
	}
	var cash string
	for try := 0; ; try++ {
		raw, challenge, err := c.post(ctx, folder, body, cash)
		if err != nil {
			return err
		}
		if challenge != "" {
			// One proof of work per call. A second challenge would mean the
			// first answer was wrong, and solving again would be the same.
			if cash != "" {
				return errors.New("mega: MEGA did not accept the proof of work it asked for")
			}
			if cash, err = solveHashcash(ctx, challenge); err != nil {
				return err
			}
			continue
		}
		err = decodeAnswer(raw, out)
		if errors.Is(err, errAgain) && try < 3 {
			if err := sleep(ctx, time.Second<<try); err != nil {
				return err
			}
			continue
		}
		return err
	}
}

// post makes one request. A 402 with a challenge comes back as the challenge
// rather than as an error, so call can answer it.
func (c *Client) post(ctx context.Context, folder string, body []byte, cash string) ([]byte, string, error) {
	q := url.Values{"id": {strconv.FormatUint(c.seq.Add(1), 10)}}
	if folder != "" {
		q.Set("n", folder)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/cs?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cash != "" {
		req.Header.Set("X-Hashcash", cash)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPaymentRequired {
		if h := resp.Header.Get("X-Hashcash"); h != "" {
			return nil, h, nil
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("mega: the API answered HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	return raw, "", err
}

// decodeAnswer reads an answer to a single command: an error number for the
// whole request, or a list whose one entry is an error number or the result.
func decodeAnswer(raw []byte, out any) error {
	raw = bytes.TrimSpace(raw)
	if code, err := strconv.Atoi(string(raw)); err == nil {
		if code < 0 {
			return apiError(code, 0)
		}
		return nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		return errors.New("mega: the API sent an answer that is not one")
	}
	if code, err := strconv.Atoi(string(bytes.TrimSpace(list[0]))); err == nil {
		if code < 0 {
			return apiError(code, 0)
		}
		return nil
	}
	if err := json.Unmarshal(list[0], out); err != nil {
		return fmt.Errorf("mega: the API sent an answer that is not one: %w", err)
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// solveHashcash answers the proof of work MEGA asks of a client it wants to
// slow down. The challenge is "1:easiness:time:token"; the answer is a 4-byte
// prefix whose SHA-256 over the prefix and the token, repeated, falls under
// the threshold the easiness stands for.
func solveHashcash(ctx context.Context, challenge string) (string, error) {
	parts := strings.Split(challenge, ":")
	if len(parts) != 4 || parts[0] != "1" {
		return "", fmt.Errorf("mega: a proof-of-work challenge this client does not know: %q", challenge)
	}
	easiness, err := strconv.Atoi(parts[1])
	if err != nil || easiness < 0 || easiness > 255 {
		return "", fmt.Errorf("mega: a proof-of-work challenge this client does not know: %q", challenge)
	}
	token, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", fmt.Errorf("mega: a proof-of-work challenge this client does not know: %q", challenge)
	}
	threshold := uint32((easiness&63)<<1+1) << ((easiness>>6)*7 + 3)
	const copies, slot = 262144, 48
	buf := make([]byte, 4+copies*slot)
	for i := range copies {
		copy(buf[4+i*slot:4+(i+1)*slot], token)
	}
	for n := uint32(1); n != 0; n++ {
		if n%64 == 0 && ctx.Err() != nil {
			return "", ctx.Err()
		}
		binary.LittleEndian.PutUint32(buf, n)
		sum := sha256.Sum256(buf)
		if binary.BigEndian.Uint32(sum[:4]) <= threshold {
			return "1:" + parts[3] + ":" + base64.RawURLEncoding.EncodeToString(buf[:4]), nil
		}
	}
	return "", errors.New("mega: no answer to MEGA's proof of work exists")
}

// node is one file or folder of a shared folder, with its key opened.
type node struct {
	handle, parent string
	folder         bool
	name           string
	size           int64
	key            []byte
}

// tree is a shared folder's listing.
type tree struct {
	root  string
	nodes map[string]*node
}

type folderAnswer struct {
	F []struct {
		H string `json:"h"`
		P string `json:"p"`
		T int    `json:"t"`
		A string `json:"a"`
		K string `json:"k"`
		S int64  `json:"s"`
	} `json:"f"`
}

// folder lists the shared folder l belongs to, the whole tree at once.
func (c *Client) folder(ctx context.Context, l Link) (*tree, error) {
	id := l.Handle + "#" + string(l.Key)
	c.mu.Lock()
	cached, ok := c.folders[id]
	c.mu.Unlock()
	if ok && time.Since(cached.at) < folderTTL {
		return cached.tree, nil
	}

	var ans folderAnswer
	if err := c.call(ctx, l.Handle, map[string]any{"a": "f", "c": 1, "ca": 1, "r": 1}, &ans); err != nil {
		return nil, err
	}
	t := &tree{nodes: make(map[string]*node, len(ans.F))}
	var order []string
	for _, f := range ans.F {
		if f.T != 0 && f.T != 1 {
			continue
		}
		n := &node{handle: f.H, parent: f.P, folder: f.T == 1, size: f.S}
		key, err := nodeKey(l.Key, f.K, n.folder)
		if err != nil {
			continue
		}
		n.key = key
		attrKey := key
		if !n.folder {
			fk, _ := splitFileKey(key)
			attrKey = fk.aes
		}
		if n.name, err = decryptName(attrKey, f.A); err != nil {
			continue
		}
		t.nodes[n.handle] = n
		order = append(order, n.handle)
	}
	for _, h := range order {
		if n := t.nodes[h]; n.folder && t.nodes[n.parent] == nil {
			t.root = h
			break
		}
	}
	if t.root == "" {
		return nil, errBadKey
	}

	c.mu.Lock()
	// Emptied whole when full, as a listing is cheap to fetch again.
	if len(c.folders) >= 64 {
		clear(c.folders)
	}
	c.folders[id] = cachedTree{tree: t, at: time.Now()}
	c.mu.Unlock()
	return t, nil
}

// nodeKey opens a node's key from the listing, which is "owner:key", several
// of them joined by "/" when the node is shared more than once.
func nodeKey(folderKey []byte, field string, folder bool) ([]byte, error) {
	want := 32
	if folder {
		want = 16
	}
	for _, part := range strings.Split(field, "/") {
		_, sealed, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		raw, err := decode64(sealed)
		if err != nil || len(raw) != want {
			continue
		}
		if key, err := decryptKey(folderKey, raw); err == nil {
			return key, nil
		}
	}
	return nil, errBadKey
}

// remoteFile is what MEGA says about one file: its name, size and key, and
// where to fetch it when that was asked for.
type remoteFile struct {
	name string
	size int64
	key  fileKey
	url  string
}

type fileAnswer struct {
	S  int64  `json:"s"`
	At string `json:"at"`
	G  string `json:"g"`
	E  int    `json:"e"`
	TL int64  `json:"tl"`
}

// errFolder is a folder link where a file was expected.
var errFolder = errors.New("mega: this link is a folder, not a file; paste it again to stage the files in it")

// file looks up the file l points at. With download it also asks for a
// download address, which is what spends transfer quota once used.
func (c *Client) file(ctx context.Context, l Link, download bool) (remoteFile, error) {
	cmd := map[string]any{"a": "g", "ssl": 2}
	if download {
		cmd["g"] = 1
	}
	var folder string
	var key []byte
	if l.Folder {
		t, err := c.folder(ctx, l)
		if err != nil {
			return remoteFile{}, err
		}
		n := t.nodes[l.Node]
		switch {
		case l.Node == "":
			return remoteFile{}, errFolder
		case n == nil:
			return remoteFile{}, errNotFound
		case n.folder:
			return remoteFile{}, errFolder
		}
		fk, _ := splitFileKey(n.key)
		if !download {
			// The listing already has everything but the address.
			return remoteFile{name: n.name, size: n.size, key: fk}, nil
		}
		folder, key = l.Handle, n.key
		cmd["n"] = l.Node
	} else {
		key = l.Key
		cmd["p"] = l.Handle
	}

	fk, err := splitFileKey(key)
	if err != nil {
		return remoteFile{}, err
	}
	var ans fileAnswer
	if err := c.call(ctx, folder, cmd, &ans); err != nil {
		return remoteFile{}, err
	}
	if ans.E < 0 {
		return remoteFile{}, apiError(ans.E, ans.TL)
	}
	name, err := decryptName(fk.aes, ans.At)
	if err != nil {
		return remoteFile{}, err
	}
	f := remoteFile{name: name, size: ans.S, key: fk, url: ans.G}
	if download && f.url == "" {
		if ans.TL > 0 {
			return remoteFile{}, &QuotaError{Wait: time.Duration(ans.TL) * time.Second}
		}
		return remoteFile{}, errors.New("mega: MEGA gave no address to download the file from")
	}
	return f, nil
}
