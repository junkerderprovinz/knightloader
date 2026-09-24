package debrid

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	rpnetTestID  = "1234567"
	rpnetTestKey = "0123456789abcdef0123456789abcdef01234567"
)

func newRPNetAt(base string) *RPNet {
	r := NewRPNet(rpnetTestID, rpnetTestKey)
	r.base = base
	r.pollEvery = time.Millisecond
	r.queueWait = time.Second
	return r
}

// rpnetAPI answers client_api.php by action and checks that every call
// carries the customer ID and the API key the way the API expects them.
func rpnetAPI(t *testing.T, answer func(action string, q url.Values) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client_api.php" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("username") != rpnetTestID || q.Get("password") != rpnetTestKey {
			t.Errorf("%s did not carry the customer ID and API key as username and password", q.Get("action"))
		}
		_, _ = io.WriteString(w, answer(q.Get("action"), q))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The body is the one hostlist.php answered live, feature words included.
func TestRPNetHostsKeepsDomainsAndDropsFeatureWords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hostlist.php" || r.URL.RawQuery != "" {
			t.Errorf("request = %s?%s, want a bare /hostlist.php", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, "filextras.com,hexload.com,mediafire.com,1fichier.com,filer.net,file.al,"+
			"clicknupload.to,isra.cloud,uploadgig.com,mega.nz,modsbase.com,filestore.to,alfafile.net,"+
			"depositfiles.com,rapidrar.com,torrent,directlink,youtubedl,cloud_download,downup.me\n")
	}))
	defer srv.Close()

	hosts, err := newRPNetAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 16 {
		t.Errorf("Hosts has %d entries, want the 16 domains: %v", len(hosts), hosts)
	}
	for _, want := range []string{"1fichier.com", "mega.nz", "downup.me"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q", want)
		}
	}
	for _, word := range []string{"torrent", "directlink", "youtubedl", "cloud_download"} {
		if hosts[word] {
			t.Errorf("the feature word %q was taken as a host", word)
		}
	}
}

// The page carries a line that reads as a domain, so only the status stops it.
func TestRPNetHostsFailsOnAnErrorPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "<html><body>\nDown for maintenance, see\nstatus.example.com\n</body></html>")
	}))
	defer srv.Close()

	if hosts, err := newRPNetAt(srv.URL).Hosts(context.Background()); err == nil {
		t.Fatalf("Hosts = %v from an error page, want an error", hosts)
	}
}

func TestRPNetUnlockReturnsTheGeneratedLink(t *testing.T) {
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		if action != "generate" || q.Get("links") != "https://mega.nz/file/x" {
			t.Errorf("action %q with links %q, want generate for the hoster link", action, q.Get("links"))
		}
		return `{"links":[{"generated":"https://dl.rpnet.example/one","filename":"File.ext","max_connections":"4"}]}`
	})

	got, err := newRPNetAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.rpnet.example/one" || got.Name != "File.ext" {
		t.Errorf("Unlock = %+v, want the generated link and its file name", got)
	}
}

func TestRPNetUnlockReadsDownloadsAsASingleObject(t *testing.T) {
	srv := rpnetAPI(t, func(string, url.Values) string {
		return `{"downloads":{"generated":"https://dl.rpnet.example/two","filename":"Two.ext"}}`
	})

	got, err := newRPNetAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.rpnet.example/two" {
		t.Errorf("Unlock = %+v, want the link from the downloads object", got)
	}
}

func TestRPNetUnlockTellsTheRefusalsApart(t *testing.T) {
	cases := []struct {
		kind, body, said, want string
	}{
		{"dead link", `{"links":[{"error":"File not found"}]}`, "File not found", "gone from the hoster"},
		{"unsupported host", `{"links":[{"error":"Hoster not supported"}]}`, "Hoster not supported", "does not support this hoster"},
		{"quota", `{"links":[{"error":"Daily traffic limit reached"}]}`, "Daily traffic limit reached", "quota exceeded"},
		{"wrong key", `{"error":["Invalid authentication."]}`, "Invalid authentication.", "customer ID or API key"},
		{"ban as plain text", `IP Ban in effect for 15 minutes`, "IP Ban in effect for 15 minutes", "banned this IP address"},
		{"anything else", `{"links":[{"error":"Server busy"}]}`, "Server busy", "rpnet: Server busy"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		srv := rpnetAPI(t, func(string, url.Values) string { return c.body })
		_, err := newRPNetAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/x")
		if err == nil {
			t.Errorf("%s: Unlock succeeded against a refusal", c.kind)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, c.want) || !strings.Contains(msg, c.said) {
			t.Errorf("%s: error = %q, want %q and the service's own %q", c.kind, msg, c.want, c.said)
		}
		if other, dup := seen[msg]; dup {
			t.Errorf("%s reads the same as %s: %q", c.kind, other, msg)
		}
		seen[msg] = c.kind
	}
}

func TestRPNetUnlockWaitsForTheQueueEntry(t *testing.T) {
	var polls atomic.Int32
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		switch action {
		case "generate":
			return `{"links":[{"id":77}]}`
		case "downloadsInformation":
			if q.Get("type") != "queue" || q.Get("ids[]") != "77" {
				t.Errorf("poll = type %q ids[] %q, want the queue entry 77", q.Get("type"), q.Get("ids[]"))
			}
			if polls.Add(1) == 1 {
				return `{"downloads":[{"text_status":"[40]"}]}`
			}
			return `{"downloads":[{"text_status":"completed","rpnet_link":"https://hdd.rpnet.example/77","filename":"Big.ext"}]}`
		}
		t.Errorf("unexpected action %q", action)
		return `{}`
	})

	got, err := newRPNetAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://hdd.rpnet.example/77" || got.Name != "Big.ext" {
		t.Errorf("Unlock = %+v, want the rpnet_link of the finished entry", got)
	}
}

func TestRPNetRetryFollowsTheStoredQueueEntry(t *testing.T) {
	var generated atomic.Int32
	var finished atomic.Bool
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		if action == "generate" {
			generated.Add(1)
			return `{"links":[{"id":"12"}]}`
		}
		if finished.Load() {
			return `{"downloads":[{"text_status":"Completed","rpnet_link":"https://hdd.rpnet.example/12"}]}`
		}
		return `{"downloads":[{"text_status":"[40]"}]}`
	})
	r := newRPNetAt(srv.URL)
	r.queueWait = 0

	_, err := r.Unlock(context.Background(), "https://mega.nz/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded while the file was still queued")
	}
	if !strings.Contains(err.Error(), "temporarily unavailable") || !strings.Contains(err.Error(), "40%") {
		t.Errorf("error = %q, want a temporary error with the progress", err)
	}

	finished.Store(true)
	got, err := r.Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
	if got.URL != "https://hdd.rpnet.example/12" {
		t.Errorf("second Unlock = %+v, want the finished entry's link", got)
	}
	if n := generated.Load(); n != 1 {
		t.Errorf("generate was called %d times, want once so the file is not queued twice", n)
	}
}

func TestRPNetQueueEntryAtOneHundredPercentIsDone(t *testing.T) {
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		if action == "generate" {
			return `{"links":[{"id":4}]}`
		}
		return `{"downloads":[{"text_status":"[100]","rpnet_link":"https://hdd.rpnet.example/4"}]}`
	})

	got, err := newRPNetAt(srv.URL).Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil || got.URL != "https://hdd.rpnet.example/4" {
		t.Errorf("Unlock = %+v, %v, want the rpnet_link of the entry at [100]", got, err)
	}
}

// A poll that got no answer says nothing about the entry, and starting over
// would queue the file a second time.
func TestRPNetPollWithoutAnAnswerKeepsTheQueueEntry(t *testing.T) {
	var generated atomic.Int32
	var down atomic.Bool
	down.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("action") {
		case "generate":
			generated.Add(1)
			_, _ = io.WriteString(w, `{"links":[{"id":9}]}`)
		case "downloadsInformation":
			if down.Load() {
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					conn.Close()
				}
				return
			}
			_, _ = io.WriteString(w, `{"downloads":[{"text_status":"completed","rpnet_link":"https://hdd.rpnet.example/9"}]}`)
		}
	}))
	defer srv.Close()
	r := newRPNetAt(srv.URL)

	if _, err := r.Unlock(context.Background(), "https://mega.nz/file/x"); err == nil {
		t.Fatal("Unlock succeeded although the poll got no answer")
	}
	down.Store(false)
	got, err := r.Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
	if got.URL != "https://hdd.rpnet.example/9" || generated.Load() != 1 {
		t.Errorf("second Unlock = %+v after %d generate calls, want the stored entry's link and a single generate", got, generated.Load())
	}
}

// The wait gives up early enough for the last poll to end before the caller's
// deadline, so the service's own error is what gets reported.
func TestRPNetQueueWaitEndsBeforeTheCallersDeadline(t *testing.T) {
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		if action == "generate" {
			return `{"links":[{"id":3}]}`
		}
		return `{"downloads":[{"text_status":"Queued"}]}`
	})
	r := newRPNetAt(srv.URL)
	r.pollEvery = 10 * time.Millisecond
	r.queueWait = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), rpnetDeadlineMargin+300*time.Millisecond)
	defer cancel()

	_, err := r.Unlock(ctx, "https://mega.nz/file/x")
	if ctx.Err() != nil || err == nil || !strings.Contains(err.Error(), "temporarily unavailable") {
		t.Errorf("Unlock = %v with the context at %v, want a temporary error before the deadline", err, ctx.Err())
	}
}

func TestRPNetDeadlineMarginOutlastsOneCall(t *testing.T) {
	if r := NewRPNet(rpnetTestID, rpnetTestKey); rpnetDeadlineMargin <= r.hc.Timeout {
		t.Errorf("rpnetDeadlineMargin = %v, want more than the %v one call may take", rpnetDeadlineMargin, r.hc.Timeout)
	}
}

// The limit is keyed by the hoster link's domain, never by RPNet's own.
func TestRPNetHostLimitLearnsMaxConnections(t *testing.T) {
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		link := q.Get("links")
		switch {
		case action == "downloadsInformation":
			return `{"downloads":[{"text_status":"completed","rpnet_link":"https://hdd.rpnet.example/9","max_connections":1}]}`
		case strings.Contains(link, "rapidrar.com"):
			return `{"links":[{"id":9}]}`
		case strings.HasSuffix(link, "filer.net/y"):
			return `{"links":[{"generated":"https://dl.rpnet.example/f","max_connections":0}]}`
		case strings.HasSuffix(link, "filer.net/z"):
			return `{"links":[{"generated":"https://dl.rpnet.example/g","max_connections":2}]}`
		case strings.HasSuffix(link, "/b"):
			return `{"links":[{"generated":"https://dl.rpnet.example/b","max_connections":"8"}]}`
		}
		return `{"links":[{"generated":"https://dl.rpnet.example/a","max_connections":"4"}]}`
	})
	r := newRPNetAt(srv.URL)

	for _, link := range []string{"https://www.mega.nz/file/a", "https://mega.nz/file/b", "https://rapidrar.com/x", "https://filer.net/y"} {
		if _, err := r.Unlock(context.Background(), link); err != nil {
			t.Fatalf("Unlock %s: %v", link, err)
		}
	}
	if got := r.HostLimit("filer.net"); got != 0 {
		t.Errorf("HostLimit(filer.net) = %d after a max_connections of 0, want 0 (no opinion)", got)
	}
	if _, err := r.Unlock(context.Background(), "https://filer.net/z"); err != nil {
		t.Fatalf("Unlock filer.net/z: %v", err)
	}
	for host, want := range map[string]int{"mega.nz": 4, "www.mega.nz": 4, "rapidrar.com": 1, "filer.net": 2, "dl.rpnet.example": 0} {
		if got := r.HostLimit(host); got != want {
			t.Errorf("HostLimit(%s) = %d, want %d", host, got, want)
		}
	}
	if got := (Resolver{ServiceID: "rpnet", Svc: r}).HostCap("mega.nz"); got != 4 {
		t.Errorf("HostCap(mega.nz) = %d, want the 4 RPNet named", got)
	}
}

func TestRPNetStartsOverWhenTheQueueEntryIsGone(t *testing.T) {
	var generated atomic.Int32
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		if action == "generate" {
			if generated.Add(1) == 1 {
				return `{"links":[{"id":5}]}`
			}
			return `{"links":[{"generated":"https://dl.rpnet.example/fresh"}]}`
		}
		return `{"downloads":[]}`
	})
	r := newRPNetAt(srv.URL)

	if _, err := r.Unlock(context.Background(), "https://mega.nz/file/x"); err == nil {
		t.Fatal("Unlock succeeded although the queue entry vanished")
	}
	got, err := r.Unlock(context.Background(), "https://mega.nz/file/x")
	if err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
	if got.URL != "https://dl.rpnet.example/fresh" || generated.Load() != 2 {
		t.Errorf("second Unlock = %+v after %d generate calls, want a fresh generate", got, generated.Load())
	}
}

// The key travels in the query string, and Go's transport errors quote the URL.
func TestRPNetErrorsNeverQuoteTheCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()
	r := newRPNetAt(srv.URL)

	_, unlockErr := r.Unlock(context.Background(), "https://mega.nz/file/x")
	_, accountErr := r.Account(context.Background())
	for _, err := range []error{unlockErr, accountErr} {
		if err == nil {
			t.Fatal("a call succeeded although the server dropped the connection")
		}
		if strings.Contains(err.Error(), rpnetTestKey) || strings.Contains(err.Error(), rpnetTestID) {
			t.Errorf("error = %q, it quotes a credential", err)
		}
	}
}

func TestRPNetAuthenticateChecksTheKey(t *testing.T) {
	var refuse atomic.Bool
	srv := rpnetAPI(t, func(action string, q url.Values) string {
		if action != "showAccountInformation" {
			t.Errorf("Authenticate called %q, want showAccountInformation", action)
		}
		if refuse.Load() {
			return `{"error":["Invalid authentication."]}`
		}
		return `{"accountInfo":{"premiumExpiry":null,"currentServer":"RPNETSERVER"}}`
	})
	r := newRPNetAt(srv.URL)

	if err := r.Authenticate(context.Background()); err != nil {
		t.Errorf("Authenticate refused a working key: %v", err)
	}
	refuse.Store(true)
	if err := r.Authenticate(context.Background()); err == nil {
		t.Error("Authenticate accepted a key RPNet refused")
	}
}

func TestRPNetAuthenticateWantsTheAccountBlock(t *testing.T) {
	srv := rpnetAPI(t, func(string, url.Values) string { return `{"message":"Bad gateway"}` })

	if err := newRPNetAt(srv.URL).Authenticate(context.Background()); err == nil {
		t.Error("Authenticate accepted an answer with neither an error nor the account")
	}
}

func TestRPNetAuthenticateNeedsBothFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a request went out without credentials")
	}))
	defer srv.Close()
	r := NewRPNet(rpnetTestID, " ")
	r.base = srv.URL

	if err := r.Authenticate(context.Background()); err == nil {
		t.Error("Authenticate succeeded without an API key")
	}
}

func TestRPNetAccountReadsPremiumExpiry(t *testing.T) {
	until := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	srv := rpnetAPI(t, func(string, url.Values) string {
		// A string rather than a number, to cover the loose decoding.
		return `{"accountInfo":{"premiumExpiry":"` + strconv.FormatInt(until.Unix(), 10) + `","currentServer":"RPNETSERVER"}}`
	})

	info, err := newRPNetAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || !info.ExpiresAt.Equal(until) {
		t.Errorf("Account = %+v, want premium until %v", info, until)
	}
	if info.Traffic != (TrafficInfo{}) {
		t.Errorf("Traffic = %+v, want it left empty since RPNet reports none", info.Traffic)
	}
}

func TestRPNetAccountWithoutPremiumIsFree(t *testing.T) {
	lapsed := time.Now().Add(-24 * time.Hour).Unix()
	for _, body := range []string{
		`{"accountInfo":{"premiumExpiry":null,"currentServer":"RPNETSERVER"}}`,
		`{"accountInfo":{"premiumExpiry":` + strconv.FormatInt(lapsed, 10) + `}}`,
	} {
		srv := rpnetAPI(t, func(string, url.Values) string { return body })
		info, err := newRPNetAt(srv.URL).Account(context.Background())
		if err != nil {
			t.Fatalf("Account: %v", err)
		}
		if info.Tier != "free" {
			t.Errorf("Tier = %q for %s, want free", info.Tier, body)
		}
	}
}
