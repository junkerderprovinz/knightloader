package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Deepbrid speaks the Deepbrid REST v1 API (https://www.deepbrid.com/api-docs)
// with the key from https://www.deepbrid.com/devices as Bearer token.
//
// Where the docs are silent or the live service departs from them, this client
// follows JDownloader's DeepbridCom.java: the public /hosts answers with bare
// hoster names instead of domains, and per-link failures reuse the numeric
// codes of the older backend-dl API.
type Deepbrid struct {
	key  string
	base string
	hc   *http.Client
}

func NewDeepbrid(apiKey string) *Deepbrid {
	return &Deepbrid{
		key:  apiKey,
		base: "https://www.deepbrid.com/api/v1",
		// A request the API cannot authenticate may be redirected to the login
		// page, which is only recognisable as long as the 302 is not followed.
		hc: httpx.New(httpx.Options{Timeout: 30 * time.Second, MaxRedirects: -1}),
	}
}

func (*Deepbrid) ID() string    { return "deepbrid" }
func (*Deepbrid) Label() string { return "Deepbrid" }

// deepbridStatus is the error/message pair every object answer carries; error
// is 0 on success.
type deepbridStatus struct {
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
}

// failed reports whether the answer is a refusal and its code. Only a zero or
// an empty error means success, so an error sent as true or as a sentence
// cannot pass a wrong key as verified.
func (s deepbridStatus) failed() (int, bool) {
	switch v := strings.Trim(string(s.Error), `" `); v {
	case "", "0", "null", "false":
		return 0, false
	default:
		code, _ := strconv.Atoi(v)
		return code, true
	}
}

// call performs one request and returns the body of a successful answer.
// Failures come both as HTTP errors and as a nonzero error code in a 200, and
// both carry the same body.
func (d *Deepbrid) call(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// Without it a refused request is redirected to the login page instead of
	// getting the JSON 401.
	req.Header.Set("Accept", "application/json")
	if d.key != "" {
		req.Header.Set("Authorization", "Bearer "+d.key)
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepbrid %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc, err := url.Parse(resp.Header.Get("Location")); err == nil && strings.HasSuffix(strings.TrimRight(loc.Path, "/"), "/login") {
			return nil, fmt.Errorf("deepbrid %s: the API key was refused", path)
		}
		return nil, fmt.Errorf("deepbrid %s: unexpected redirect (%s)", path, resp.Status)
	}
	var st deepbridStatus
	code, failed := 0, false
	if json.Unmarshal(raw, &st) == nil {
		code, failed = st.failed()
	}
	if resp.StatusCode >= 400 || failed {
		return nil, deepbridFailure(path, resp, code, st.Message)
	}
	return raw, nil
}

// deepbridFailure names the kind of refusal, followed by Deepbrid's own
// message. The small codes are the per-link ones JDownloader handles; an HTTP
// error repeats its status as the code.
func deepbridFailure(path string, resp *http.Response, code int, msg string) error {
	msg = deepbridPlain(msg)
	say := func(format string, args ...any) error {
		s := fmt.Sprintf(format, args...)
		if msg == "" {
			return errors.New(s)
		}
		return fmt.Errorf("%s: %s", s, msg)
	}
	if code == 0 {
		code = resp.StatusCode
	}
	switch code {
	case 3:
		return say("deepbrid: Deepbrid does not support this host")
	case 8:
		return say("deepbrid: the account has to wait before its next download")
	case 9:
		return say("deepbrid: the daily limit for this hoster is used up")
	case 10:
		return say("deepbrid: this hoster is in maintenance at Deepbrid")
	case 15:
		return say("deepbrid: Deepbrid took this server for a proxy, VPN or VPS; its support can allow the account")
	case http.StatusUnauthorized:
		return say("deepbrid %s: the API key was refused", path)
	case http.StatusForbidden:
		return say("deepbrid %s: the account was refused", path)
	case http.StatusTooManyRequests:
		if wait := strings.TrimSpace(resp.Header.Get("Retry-After")); wait != "" {
			return say("deepbrid %s: too many requests, retry in %ss", path, wait)
		}
		return say("deepbrid %s: too many requests", path)
	case http.StatusServiceUnavailable:
		return say("deepbrid %s: Deepbrid is under maintenance", path)
	}
	if msg == "" {
		return fmt.Errorf("deepbrid %s: refused without a reason (%s, code %d)", path, resp.Status, code)
	}
	return fmt.Errorf("deepbrid %s: %s", path, msg)
}

var deepbridTag = regexp.MustCompile(`<[^>]*>`)

// deepbridPlain strips the markup Deepbrid puts into some messages, such as
// the bold wait time and the upgrade link in code 8.
func deepbridPlain(msg string) string {
	return strings.Join(strings.Fields(html.UnescapeString(deepbridTag.ReplaceAllString(msg, " "))), " ")
}

// deepbridDomains maps the bare names the live /hosts answers with to the
// domains their links use: those JDownloader's plugin for each hoster matches,
// without the ones it marks dead and without download servers. A name missing
// here is left out rather than given a guessed TLD.
var deepbridDomains = map[string][]string{
	"1fichier": {"1fichier.com", "alterupload.com", "cjoint.net", "desfichiers.com", "desfichiers.net",
		"dfichiers.com", "dl4free.com", "megadl.fr", "mesfichiers.org", "piecejointe.net", "pjointe.com", "tenvoi.com"},
	"4shared":      {"4shared.com", "4shared-china.com", "4s.io"},
	"dailyuploads": {"dailyuploads.net", "dailyuploads.cc", "dailyuploads.im", "dailyuploads.in", "dailyuploads.io"},
	"ddownload":    {"ddownload.com", "ddl.to"},
	// JDownloader serves depositfiles and dfiles with one plugin.
	"depositfiles": {"depositfiles.com", "depositfiles.org", "dfiles.com", "dfiles.eu", "dfiles.ru"},
	"dfiles":       {"depositfiles.com", "depositfiles.org", "dfiles.com", "dfiles.eu", "dfiles.ru"},
	"dropapk":      {"drop.download", "dropapk.com", "dropapk.to", "fastclick.to", "mixloads.com", "upstream.to"},
	"easybytez": {"easybytez.com", "easybytez.co", "easybytez.eu", "easybytez.me", "easybytez.to",
		"ebytez.com", "easyload.to", "ezbytez.com", "zingload.com"},
	"emload":      {"emload.com", "emload.al", "wdupload.com"},
	"extmatrix":   {"extmatrix.com"},
	"fileblade":   {"fileblade.com"},
	"filecat":     {"filecat.net"},
	"filefactory": {"filefactory.com"},
	"filenext":    {"filenext.com"},
	"filespace":   {"filespace.com", "spaceforfiles.com"},
	"filestore":   {"filestore.me"},
	"filestoreto": {"filestore.to"},
	"gofile":      {"gofile.io"},
	"hexload":     {"hexload.com", "hexupload.com", "hexupload.net"},
	"hitfile":     {"hitfile.net"},
	"jumploads":   {"jumploads.com", "goloady.com"},
	"katfile": {"katfile.com", "katfile.biz", "katfile.cloud", "katfile.online", "katfile.space",
		"katfile.vip", "katfile.ws"},
	"kenfiles":    {"kenfiles.com", "kfs.space"},
	"krakenfiles": {"krakenfiles.com"},
	"mediafire":   {"mediafire.com", "mfi.re"},
	"mega":        {"mega.nz", "mega.co.nz"},
	"prefiles":    {"prefiles.com"},
	"safedock":    {"safedock.io"},
	"subyshare":   {"subyshare.com"},
	"syncs":       {"syncs.online"},
	"terabox": {"terabox.com", "1024tera.com", "1024terabox.com", "4funbox.com", "dubox.com", "gibibox.com",
		"goaibox.com", "mirrobox.com", "terabox.app", "teraboxapp.com", "teraearn.com"},
	"terabytez": {"terabytez.org"},
	"turbobit": {"turbobit.net", "fayloobmennik.com", "filemaster.ru", "hotshare.biz", "kilofile.com",
		"rapidfile.tk", "torbobit.net", "tourbobit.com", "trbt.cc", "trubobit.com", "turb.cc", "turb.to",
		"turbo.to", "turbobeet.net", "turbobi.pw", "turbobif.com", "turbobit.cc", "turbobita.net",
		"turbobite.net", "turbobitn.com", "turbobyt.com", "turbobyte.net", "turboget.net", "turboot.ru",
		"twobit.ru", "wayupload.com", "xrfiles.ru"},
	"uploadhaven": {"uploadhaven.com"},
	"worldbytez":  {"worldbytez.com", "worldbytez.info", "worldbytez.net", "worldbytez.org", "worldbytez.xyz"},
	"wushare":     {"wushare.com"},
	"youtube":     {"youtube.com", "youtu.be"},
}

// deepbridHoster maps each domain in deepbridDomains back to its hoster, so a
// domain the documented answer names brings the hoster's other domains along,
// as the JDownloader plugin that takes it matches them all.
var deepbridHoster = func() map[string]string {
	m := map[string]string{}
	for name, domains := range deepbridDomains {
		for _, d := range domains {
			m[d] = name
		}
	}
	return m
}()

// Hosts sends the key to /hosts although the endpoint is public, as
// JDownloader does: the documented answer with a status per host may be the
// one only an authenticated call gets.
func (d *Deepbrid) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, err := d.call(ctx, http.MethodGet, "/hosts", nil)
	if err != nil {
		return nil, err
	}
	set := deepbridParseHosts(raw)
	if len(set) == 0 {
		return nil, errors.New("deepbrid: /hosts named no host this build knows")
	}
	return set, nil
}

// deepbridParseHosts reads each shape /hosts is known in: a flat array of bare
// names (live), an array of single-key objects mapping a comma-separated
// domain list to "up" or "down (date)" (documented), and either of those sent
// as an object keyed "0", "1", and so on (seen by JDownloader).
func deepbridParseHosts(raw []byte) map[string]bool {
	set := map[string]bool{}
	entry := func(e json.RawMessage) {
		var names string
		if json.Unmarshal(e, &names) == nil {
			deepbridAddNames(set, names)
			return
		}
		var byStatus map[string]json.RawMessage
		if json.Unmarshal(e, &byStatus) == nil {
			for names, status := range byStatus {
				if deepbridUp(status) {
					deepbridAddNames(set, names)
				}
			}
		}
	}

	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, e := range list {
			entry(e)
		}
		return set
	}
	var keyed map[string]json.RawMessage
	if json.Unmarshal(raw, &keyed) != nil {
		return set
	}
	for k, v := range keyed {
		if strings.Trim(k, "0123456789") == "" {
			entry(v)
		} else if deepbridUp(v) {
			deepbridAddNames(set, k)
		}
	}
	return set
}

// deepbridUp reads a host status. Only "up" counts, as in JDownloader, so a
// state this client does not know leaves the host out.
func deepbridUp(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && strings.EqualFold(strings.TrimSpace(s), "up")
}

// deepbridAddNames adds each comma-separated entry with all of its hoster's
// domains. A domain without a known hoster is added as it is, a bare name
// only through deepbridDomains.
func deepbridAddNames(set map[string]bool, names string) {
	for _, n := range strings.Split(names, ",") {
		// Lower-cased first, because NormalizeHost strips only a lower-case "www.".
		n = NormalizeHost(strings.ToLower(n))
		if name, ok := deepbridHoster[n]; ok {
			n = name
		}
		if domains, ok := deepbridDomains[n]; ok {
			for _, d := range domains {
				set[d] = true
			}
		} else if strings.Contains(n, ".") {
			set[n] = true
		}
	}
}

// Unlock posts the link to /generate/link. The size in the answer is rounded
// ("1.50 GB"), which is close enough to show until the download reports the
// exact figure.
func (d *Deepbrid) Unlock(ctx context.Context, link string) (Direct, error) {
	raw, err := d.call(ctx, http.MethodPost, "/generate/link", url.Values{"link": {link}})
	if err != nil {
		return Direct{}, err
	}
	var got struct {
		Link     string          `json:"link"`
		Filename string          `json:"filename"`
		Size     json.RawMessage `json:"size"`
	}
	if json.Unmarshal(raw, &got) != nil {
		return Direct{}, errors.New("deepbrid: unreadable answer to /generate/link")
	}
	if got.Link == "" {
		return Direct{}, errors.New("deepbrid: no direct link returned")
	}
	return Direct{URL: got.Link, Name: got.Filename, Size: deepbridSize(got.Size)}, nil
}

var deepbridSizeText = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([KMGT]?)i?B$`)

// deepbridUnits steps by 1024 because the docs pair "15.30 GB" with
// 16424337408 bytes.
var deepbridUnits = map[string]float64{"": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}

// deepbridSize reads a size sent as a number, a numeric string or a figure
// like "1.50 GB".
func deepbridSize(raw json.RawMessage) int64 {
	if n := looseInt(raw); n > 0 {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0
	}
	m := deepbridSizeText.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return int64(v * deepbridUnits[strings.ToUpper(m[2])])
}

// deepbridUser is the part of /user this client reads.
type deepbridUser struct {
	Type       string `json:"type"`
	Expiration string `json:"expiration"`
}

func (d *Deepbrid) user(ctx context.Context) (deepbridUser, error) {
	raw, err := d.call(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return deepbridUser{}, err
	}
	var u deepbridUser
	if json.Unmarshal(raw, &u) != nil {
		return deepbridUser{}, errors.New("deepbrid: unreadable answer to /user")
	}
	return u, nil
}

// Authenticate checks the key against /user. It is exported for the credential
// check, because /hosts answers without a key and would accept a wrong one.
func (d *Deepbrid) Authenticate(ctx context.Context) error {
	if d.key == "" {
		return errors.New("deepbrid: an API key is required")
	}
	_, err := d.user(ctx)
	return err
}

// Account reads /user for plan and expiry. Deepbrid caps no traffic across the
// account, only per hoster, so a premium account reads Unlimited.
func (d *Deepbrid) Account(ctx context.Context) (AccountInfo, error) {
	u, err := d.user(ctx)
	if err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: strings.ToLower(strings.TrimSpace(u.Type))}
	if info.Tier == "" {
		info.Tier = "free"
	}
	if info.Tier != "premium" {
		return info, nil
	}
	info.Traffic.Unlimited = true
	// expiration is the last day of premium, so the account runs out when
	// that day ends.
	if day, err := time.Parse(time.DateOnly, strings.TrimSpace(u.Expiration)); err == nil {
		info.ExpiresAt = day.Add(24 * time.Hour)
	}
	return info, nil
}
