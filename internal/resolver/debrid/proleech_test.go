package debrid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ProLeech answers every call on one path and tells them apart by query
// parameter, so the fake server routes on the query.

const proleechTestKey = "k3y0123456789abcdefgh"

func proleechAt(base string) *ProLeech {
	p := NewProLeech("amy", proleechTestKey)
	p.base = base
	return p
}

func proleechServer(t *testing.T, reply func(q url.Values) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply(r.URL.Query()))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func proleechHasCredentials(q url.Values) bool {
	return q.Get("apiusername") == "amy" && q.Get("apikey") == proleechTestKey
}

func TestProLeechHostsSplitsCombinedEntriesAndAddsTheDomainMap(t *testing.T) {
	srv := proleechServer(t, func(q url.Values) string {
		switch {
		case q.Has("hosts"):
			if q.Has("apiusername") || q.Has("apikey") {
				t.Error("the public host list was sent the credentials")
			}
			return `["rapidgator.net","Uploaded.net / ul.to","www.1fichier.com",42,""]`
		case q.Get("domainmap") == "1":
			if !proleechHasCredentials(q) {
				t.Error("the domain map was asked for without the configured credentials")
			}
			return `{"error":0,"domains":["rg.to","alterupload.com"]}`
		}
		t.Errorf("unexpected query %v", q)
		return `{}`
	})

	hosts, err := proleechAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "uploaded.net", "ul.to", "1fichier.com", "rg.to", "alterupload.com"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	if len(hosts) != 6 {
		t.Errorf("Hosts = %v, want only the six domains named", hosts)
	}
}

func TestProLeechHostsWithoutCredentialsReadsOnlyThePublicList(t *testing.T) {
	srv := proleechServer(t, func(q url.Values) string {
		if !q.Has("hosts") {
			t.Errorf("request %v needs credentials that were never given", q)
		}
		return `["rapidgator.net"]`
	})
	p := NewProLeech("", "")
	p.base = srv.URL

	hosts, err := p.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] {
		t.Errorf("Hosts = %v, want the public list", hosts)
	}
}

func TestProLeechHostsKeepsThePublicListWhenTheDomainMapIsRefused(t *testing.T) {
	srv := proleechServer(t, func(q url.Values) string {
		if q.Has("hosts") {
			return `["rapidgator.net"]`
		}
		return `{"error":-6,"message":"Username or API key not found."}`
	})

	hosts, err := proleechAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] {
		t.Errorf("Hosts = %v, want the public list", hosts)
	}
}

// JD guards against the list coming back as null.
func TestProLeechHostsRefusesAnEmptyList(t *testing.T) {
	srv := proleechServer(t, func(q url.Values) string {
		if q.Has("hosts") {
			return `null`
		}
		return `{"error":0,"domains":[]}`
	})

	if hosts, err := proleechAt(srv.URL).Hosts(context.Background()); err == nil {
		t.Fatalf("Hosts = %v, want an error for a list without hosts", hosts)
	}
}

func TestProLeechUnlockReadsLinkNameSizeAndChunks(t *testing.T) {
	const link = "https://rapidgator.net/file/abc/File.ext.html"
	srv := proleechServer(t, func(q url.Values) string {
		if q.Get("link") != link {
			t.Errorf("link parameter = %q, want the hoster link", q.Get("link"))
		}
		if !proleechHasCredentials(q) {
			t.Error("the unlock was sent without the configured credentials")
		}
		return `{"error":0,"message":"OK","hoster":"rapidgator.net",
			"link":"https://dl.proleech.link/x/File.ext","filename":"File.ext",
			"size":"10.15 MB","max_chunks":"4","traffic_left":"399 GB"}`
	})
	p := proleechAt(srv.URL)

	if got := p.HostLimit("rapidgator.net"); got != 1 {
		t.Fatalf("HostLimit before any unlock = %d, want JD's default of 1", got)
	}
	got, err := p.Unlock(context.Background(), link)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.proleech.link/x/File.ext" || got.Name != "File.ext" {
		t.Errorf("Unlock = %+v, want the link and file name from the answer", got)
	}
	if got.Size != 10643046 {
		t.Errorf("Size = %d, want 10.15 MiB read from the text", got.Size)
	}
	if got := p.HostLimit("www.rapidgator.net"); got != 4 {
		t.Errorf("HostLimit(rapidgator.net) = %d, want the 4 from max_chunks", got)
	}
	if got := p.HostLimit("dl.proleech.link"); got != 1 {
		t.Errorf("HostLimit(dl.proleech.link) = %d, want the default for the direct link's own host", got)
	}
}

func TestProLeechUnlockWithoutMaxChunksPutsTheHosterBackToOneConnection(t *testing.T) {
	const link = "https://rapidgator.net/file/abc"
	srv := proleechServer(t, func(url.Values) string {
		return `{"error":0,"link":"https://dl.proleech.link/x/File.ext"}`
	})
	p := proleechAt(srv.URL)
	p.rememberChunks(link, 8)

	if _, err := p.Unlock(context.Background(), link); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got := p.HostLimit("rapidgator.net"); got != 1 {
		t.Errorf("HostLimit = %d, want 1 once an answer carries no max_chunks", got)
	}
}

func TestProLeechUnlockHoldsBackAnUnlockInsideTheGap(t *testing.T) {
	var requests atomic.Int32
	srv := proleechServer(t, func(url.Values) string {
		requests.Add(1)
		return `{"error":0,"link":"https://dl.proleech.link/x/File.ext"}`
	})
	p := proleechAt(srv.URL)

	if _, err := p.Unlock(context.Background(), "https://rapidgator.net/file/a"); err != nil {
		t.Fatalf("first Unlock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), proleechUnlockGap/2)
	defer cancel()
	_, err := p.Unlock(ctx, "https://rapidgator.net/file/b")
	if err == nil || !strings.Contains(err.Error(), "holding unlocks back") {
		t.Fatalf("second Unlock error = %v, want it held back by the pacer", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("the server saw %d unlocks, want only the first", n)
	}
}

func TestProLeechUnlockErrorsSayWhatWentWrong(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"dead link", `{"error":7,"message":"Link dead"}`, "the file is gone"},
		{"unsupported host", `{"error":3,"message":"Link not supported"}`, "does not support this link"},
		{"host quota", `{"error":8,"message":"Host limit reached"}`, "daily limit for this hoster"},
		{"traffic quota", `{"error":"9","message":"File too big, reset in 3h"}`, "traffic this account has left"},
		{"wrong credentials", `{"error":-6,"message":"Username or API key not found."}`, "was refused"},
		{"maintenance", `{"error":-4,"message":"Site is temporarily disabled"}`, "maintenance"},
		{"api lock", `{"error":-12,"message":"API locked"}`, "too many unlocks"},
		{"sentence instead of a code", `{"error":"Invalid link"}`, "Invalid link"},
	}
	for _, c := range cases {
		srv := proleechServer(t, func(url.Values) string { return c.body })
		_, err := proleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err == nil {
			t.Errorf("%s: Unlock succeeded against a refusal", c.name)
			continue
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "proleech: ") || !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want it to name the service and say %q", c.name, msg, c.want)
		}
		var body struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(c.body), &body)
		if !strings.Contains(msg, body.Message) {
			t.Errorf("%s: error = %q, want ProLeech's own message %q in it", c.name, msg, body.Message)
		}
		if strings.Contains(msg, proleechTestKey) {
			t.Errorf("%s: error = %q carries the API key", c.name, msg)
		}
	}
}

func TestProLeechUnlockWithoutADirectLinkFails(t *testing.T) {
	for _, body := range []string{
		`{"error":0,"message":"OK"}`,
		`{"error":0,"message":"OK","link":"null"}`,
		`{"error":0,"message":"OK","link":"/dl/x/File.ext"}`,
	} {
		srv := proleechServer(t, func(url.Values) string { return body })
		if got, err := proleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x"); err == nil {
			t.Errorf("%s: Unlock = %+v, want an error without an http link in the answer", body, got)
		}
	}
}

func TestProLeechUnreadableAnswerIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "<html><body>maintenance</body></html>")
	}))
	defer srv.Close()

	_, err := proleechAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("Unlock error = %v, want an unreadable answer", err)
	}
}

// The credentials travel in the query string, and net/http puts the whole URL
// into its transport errors.
func TestProLeechTransportErrorsDoNotCarryTheCredentials(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	_, err := proleechAt(base).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded against a closed server")
	}
	if strings.Contains(err.Error(), proleechTestKey) || strings.Contains(err.Error(), "apiusername") {
		t.Errorf("error = %q carries the credentials", err)
	}
}

func TestProLeechAuthenticateRefusesAWrongKey(t *testing.T) {
	srv := proleechServer(t, func(url.Values) string {
		return `{"error":-6,"message":"Username or API key not found."}`
	})

	err := proleechAt(srv.URL).Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "was refused") {
		t.Fatalf("Authenticate error = %v, want the credentials refused", err)
	}
}

func TestProLeechAuthenticateAcceptsAWorkingKey(t *testing.T) {
	srv := proleechServer(t, func(q url.Values) string {
		if q.Get("account") != "1" || !proleechHasCredentials(q) {
			t.Errorf("Authenticate sent %v, want the account call with the credentials", q)
		}
		return `{"error":0,"login":"amy","premium":"yes"}`
	})

	if err := proleechAt(srv.URL).Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestProLeechAuthenticateNeedsBothValues(t *testing.T) {
	srv := proleechServer(t, func(q url.Values) string {
		t.Errorf("a request went out without a key: %v", q)
		return `{}`
	})
	p := NewProLeech("amy", "")
	p.base = srv.URL

	if err := p.Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate succeeded without an API key")
	}
}

func TestProLeechAccountReadsPlanExpiryAndTraffic(t *testing.T) {
	srv := proleechServer(t, func(url.Values) string {
		return `{"error":0,"login":"amy","email":"amy@example.com","premium":"yes",
			"subscriptions_date":"2030-05-17","used_today":"1.5 GB","traffic_left":1073741824,
			"last_ip":"192.0.2.1","last_user_agent":"KnightLoader"}`
	})

	info, err := proleechAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	if want := time.Date(2030, 5, 18, 0, 0, 0, 0, time.UTC); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want the end of the last day, %v", info.ExpiresAt, want)
	}
	const used, left = 1610612736, 1073741824
	if info.Traffic.UsedBytes != used || info.Traffic.LimitBytes != used+left {
		t.Errorf("Traffic = %+v, want %d used and %d left", info.Traffic, used, left)
	}
	if info.Traffic.Unlimited {
		t.Error("Traffic reads as unlimited, but ProLeech meters it")
	}
}

func TestProLeechAccountLeavesTheLimitOpenWhenTrafficLeftIsZero(t *testing.T) {
	srv := proleechServer(t, func(url.Values) string {
		return `{"error":0,"premium":"true","used_today":5000,"traffic_left":0}`
	})

	info, err := proleechAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium from \"true\"", info.Tier)
	}
	if info.Traffic.UsedBytes != 5000 || info.Traffic.LimitBytes != 0 {
		t.Errorf("Traffic = %+v, want 5000 used and no limit", info.Traffic)
	}
	if !info.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time without a subscriptions_date", info.ExpiresAt)
	}
}

func TestProLeechFreeAccountReadsAsFree(t *testing.T) {
	for _, body := range []string{
		`{"error":-5,"message":"Free accounts are not supported."}`,
		`{"error":0,"premium":"no"}`,
	} {
		srv := proleechServer(t, func(url.Values) string { return body })
		info, err := proleechAt(srv.URL).Account(context.Background())
		if err != nil {
			t.Errorf("%s: Account: %v", body, err)
			continue
		}
		if info.Tier != "free" {
			t.Errorf("%s: Tier = %q, want free", body, info.Tier)
		}
	}
}

func TestProLeechPacerSpacesUnlocksAndCapsTheWindow(t *testing.T) {
	var pc proleechPacer
	start := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	if at, _ := pc.reserve(start, time.Time{}); !at.Equal(start) {
		t.Fatalf("first slot = %v, want right away", at)
	}
	if at, _ := pc.reserve(start, time.Time{}); !at.Equal(start.Add(proleechUnlockGap)) {
		t.Fatalf("second slot = %v, want one gap later", at)
	}
	for i := 2; i < proleechUnlockBudget; i++ {
		pc.reserve(start, time.Time{})
	}
	if at, _ := pc.reserve(start, time.Time{}); !at.Equal(start.Add(proleechUnlockWindow)) {
		t.Fatalf("slot past the budget = %v, want once the first unlock leaves the window", at)
	}

	late, ok := pc.reserve(start, start.Add(time.Minute))
	if ok {
		t.Fatalf("a slot at %v was booked past the caller's deadline", late)
	}
	if at, _ := pc.reserve(start, time.Time{}); !at.Equal(late) {
		t.Errorf("slot after a refused booking = %v, want %v, the one left free", at, late)
	}
}

func TestProLeechPacerRefusesASlotThatLeavesNoTimeForTheCall(t *testing.T) {
	var pc proleechPacer
	start := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	pc.reserve(start, time.Time{})
	next := start.Add(proleechUnlockGap)

	if at, ok := pc.reserve(start, next.Add(proleechCallTimeout)); ok {
		t.Fatalf("a slot at %v was booked with only one call timeout left before the deadline", at)
	}
	if at, ok := pc.reserve(start, next.Add(proleechCallRoom)); !ok || !at.Equal(next) {
		t.Errorf("reserve = %v, %v, want the slot at %v booked once a whole call fits after it", at, ok, next)
	}
}

func TestProLeechPacerGivesBackTheSlotOfACancelledWait(t *testing.T) {
	var pc proleechPacer
	first, _ := pc.reserve(time.Now(), time.Time{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := pc.wait(ctx); err == nil {
		t.Fatal("wait returned nil on a cancelled context")
	}
	if at, _ := pc.reserve(time.Now(), time.Time{}); !at.Equal(first.Add(proleechUnlockGap)) {
		t.Errorf("slot after a cancelled wait = %v, want %v, the one the cancelled wait held", at, first.Add(proleechUnlockGap))
	}
}

func TestProLeechSizeReadsNumbersDigitsAndText(t *testing.T) {
	cases := map[string]int64{
		`12345`:       12345,
		`"12345"`:     12345,
		`"10.15 MB"`:  10643046,
		`"1.5GB"`:     1610612736,
		`"700 KiB"`:   716800,
		`"lots"`:      0,
		`"5 parsecs"`: 0,
		`null`:        0,
	}
	for raw, want := range cases {
		if got := proleechSize(json.RawMessage(raw)); got != want {
			t.Errorf("proleechSize(%s) = %d, want %d", raw, got, want)
		}
	}
}
