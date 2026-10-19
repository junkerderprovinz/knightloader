package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

const discordSecret = "dQw4w9WgXcQ-webhook-token_value"

// discordAt makes a test server count as Discord for the length of one test,
// which is why the hosts are a package var, and returns its webhook address.
func discordAt(t *testing.T, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	discordHosts[u.Hostname()] = true
	t.Cleanup(func() { delete(discordHosts, u.Hostname()) })
	return base + "/api/webhooks/123456789/" + discordSecret
}

type sentEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
	Fields      []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Inline bool   `json:"inline"`
	} `json:"fields"`
	Footer struct {
		Text string `json:"text"`
	} `json:"footer"`
	Timestamp string `json:"timestamp"`
}

type sentMessage struct {
	Embeds          []sentEmbed `json:"embeds"`
	AllowedMentions *struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

func decodeDiscord(t *testing.T, body string) sentEmbed {
	t.Helper()
	var m sentMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("the body is not JSON (%v): %s", err, body)
	}
	if len(m.Embeds) != 1 {
		t.Fatalf("the body carries %d embeds, want one: %s", len(m.Embeds), body)
	}
	return m.Embeds[0]
}

func (e sentEmbed) field(name string) string {
	for _, f := range e.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

func units(s string) int { return len(utf16.Encode([]rune(s))) }

func TestADiscordAddressIsRecognisedByItsHostAndPath(t *testing.T) {
	for _, tc := range []struct {
		addr  string
		token string
	}{
		{"https://discord.com/api/webhooks/123/abc-DEF_1", "abc-DEF_1"},
		{"https://discordapp.com/api/webhooks/123/abc", "abc"},
		{"https://ptb.discord.com/api/webhooks/123/abc", "abc"},
		{"https://canary.discord.com/api/webhooks/123/abc", "abc"},
		{"https://Discord.com/api/v10/webhooks/123/abc?thread_id=9", "abc"},
		{"https://discord.com/api/webhooks/123/abc/", "abc"},
		// Slack and GitHub payloads go to their own suffix and are not an embed.
		{"https://discord.com/api/webhooks/123/abc/slack", ""},
		{"https://discord.com/api/webhooks/123/abc/github", ""},
		{"https://discord.com/api/webhooks/123", ""},
		{"https://discord.com/channels/123/456", ""},
		{"https://discord.com.example/api/webhooks/123/abc", ""},
		{"https://ntfy.example/api/webhooks/123/abc", ""},
		{"not an address", ""},
	} {
		if got := discordToken(tc.addr); got != tc.token {
			t.Errorf("discordToken(%q) = %q, want %q", tc.addr, got, tc.token)
		}
	}
}

func TestADiscordMessageIsColouredByHowTheEventWent(t *testing.T) {
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	task := &script.TaskView{Name: "film.mkv", Package: "Films", Category: "Movies", Host: "example.invalid", Size: 7_000_000_000}
	failed := &script.TaskView{Name: "film.mkv", Error: "the hoster says the file was removed", Reason: "offline"}
	for _, tc := range []struct {
		name   string
		f      script.Firing
		title  string
		colour int
		field  string
		value  string
	}{
		{"a finished download", script.Firing{Trigger: script.TriggerTaskDone, Task: task},
			"Download finished", discordSuccess, "Size", "7.0 GB"},
		{"its category", script.Firing{Trigger: script.TriggerTaskDone, Task: task},
			"Download finished", discordSuccess, "Category", "Movies"},
		{"a failed download", script.Firing{Trigger: script.TriggerTaskFailed, Task: failed},
			"Download failed", discordFailure, "Error", "the hoster says the file was removed"},
		{"a failure with only a reason code", script.Firing{Trigger: script.TriggerTaskFailed, Task: &script.TaskView{Name: "x", Reason: "disk_full"}},
			"Download failed", discordFailure, "Error", "disk\\_full"},
		{"a link", script.Firing{Trigger: script.TriggerLinkAdded, Task: task},
			"Link added", discordInfo, "Package", "Films"},
		{"a whole package", script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Films", Files: 3, Done: 3, Bytes: 2_500_000}},
			"Package finished", discordSuccess, "Size", "2.5 MB"},
		{"a package with a missing file", script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Films", Files: 3, Done: 2, Failed: 1}},
			"Package finished with failed files", discordFailure, "Failed", "1"},
		{"an archive", script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{Name: "a.rar", OK: true, Files: 4}},
			"Unpacked", discordSuccess, "Files", "4"},
		{"an archive that would not open", script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{Name: "a.rar", Error: "wrong password"}},
			"Unpacking failed", discordFailure, "Error", "wrong password"},
		{"a bad checksum", script.Firing{Trigger: script.TriggerChecksumFailed, Task: task},
			"Checksum mismatch", discordFailure, "Host", "example.invalid"},
		{"a new address", script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{OK: true, Changed: true, From: "203.0.113.7", To: "203.0.113.9"}},
			"New IP address", discordSuccess, "To", "203.0.113.9"},
		{"the same address", script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{OK: true, From: "203.0.113.7", To: "203.0.113.7"}},
			"Reconnect kept the same IP address", discordInfo, "From", "203.0.113.7"},
		{"a reconnect that failed", script.Firing{Trigger: script.TriggerReconnectDone, Reconnect: &script.ReconnectView{Error: "router unreachable"}},
			"Reconnect failed", discordFailure, "Error", "router unreachable"},
		{"an account", script.Firing{Trigger: script.TriggerAccountExpired, Account: &script.AccountView{Service: "example", Tier: "premium"}},
			"Account expired", discordFailure, "Tier", "premium"},
		{"a captcha", script.Firing{Trigger: script.TriggerCaptchaPending, Captcha: &script.CaptchaView{Host: "example.invalid", Kind: "image"}},
			"Captcha waiting", discordInfo, "Kind", "image"},
		{"an idle queue", script.Firing{Trigger: script.TriggerQueueIdle},
			"Queue is idle", discordInfo, "", ""},
	} {
		tc.f.At = at
		e := decodeDiscord(t, discordBody(tc.f, "box"))
		if e.Title != tc.title || e.Color != tc.colour {
			t.Errorf("%s: title %q colour %#x, want %q %#x", tc.name, e.Title, e.Color, tc.title, tc.colour)
		}
		if tc.field != "" && e.field(tc.field) != tc.value {
			t.Errorf("%s: field %s is %q, want %q (fields %+v)", tc.name, tc.field, e.field(tc.field), tc.value, e.Fields)
		}
		if e.Footer.Text != "box" || e.Timestamp != "2026-09-08T12:00:00Z" {
			t.Errorf("%s: footer %q timestamp %q, want the instance and the time of the event", tc.name, e.Footer.Text, e.Timestamp)
		}
		for _, f := range e.Fields {
			if f.Value == "" {
				t.Errorf("%s: field %s is empty, which Discord refuses", tc.name, f.Name)
			}
		}
	}
}

func TestADiscordMessageNamesWhatTheEventIsAbout(t *testing.T) {
	for _, tc := range []struct {
		f    script.Firing
		want string
	}{
		{script.Firing{Trigger: script.TriggerTaskDone, Task: &script.TaskView{Name: "film.mkv"}}, "film.mkv"},
		{script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: "Season 1"}}, "Season 1"},
		{script.Firing{Trigger: script.TriggerExtractDone, Extract: &script.ExtractView{Name: "a.rar", OK: true}}, "a.rar"},
		{script.Firing{Trigger: script.TriggerAccountExpired, Account: &script.AccountView{Service: "example", Label: "Main key"}}, "Main key"},
		{script.Firing{Trigger: script.TriggerAccountExpired, Account: &script.AccountView{Service: "example"}}, "example"},
		{script.Firing{Trigger: script.TriggerCaptchaPending, Captcha: &script.CaptchaView{Host: "example.invalid"}}, "example.invalid"},
	} {
		if got := decodeDiscord(t, discordBody(tc.f, "")).Description; got != tc.want {
			t.Errorf("%s: description %q, want %q", tc.f.Trigger, got, tc.want)
		}
	}
}

func TestADiscordMessageFromAnUnnamedInstanceIsSignedKnightLoader(t *testing.T) {
	e := decodeDiscord(t, discordBody(firingWithTask("x.mkv"), ""))
	if e.Footer.Text != "KnightLoader" {
		t.Errorf("the footer reads %q", e.Footer.Text)
	}
}

func TestADiscordMessageShowsANameExactlyAsItIs(t *testing.T) {
	name := "**bold** _x_ ~~y~~ `z` ||s|| [link](https://a.example) <@123> # - > 01. Track\nnext"
	body := discordBody(firingWithTask(name), "")
	e := decodeDiscord(t, body)
	want := `\*\*bold\*\* \_x\_ \~\~y\~\~ ` + "\\`z\\`" + ` \|\|s\|\| \[link\](https://a.example) \<@123\> \# \- \> 01. Track next`
	if e.Description != want {
		t.Errorf("description\n got %s\nwant %s", e.Description, want)
	}
	// A leading number and a full stop make an ordered list, which drops the
	// zero from "01. Track".
	if got := decodeDiscord(t, discordBody(firingWithTask("01. Track.mp3"), "")).Description; got != `01\. Track.mp3` {
		t.Errorf("a track number reads %q", got)
	}
	var m sentMessage
	_ = json.Unmarshal([]byte(body), &m)
	if m.AllowedMentions == nil || m.AllowedMentions.Parse == nil || len(m.AllowedMentions.Parse) != 0 {
		t.Errorf("the message does not switch every mention off: %s", body)
	}
}

func TestADiscordMessageStaysInsideDiscordsLimits(t *testing.T) {
	// Every escape doubles a character and every emoji is two UTF-16 units,
	// so this is the longest each value can get.
	long := strings.Repeat("*😀", 6000)
	f := script.Firing{
		Trigger: script.TriggerTaskFailed,
		At:      time.Now(),
		Task: &script.TaskView{
			Name: long, Package: long, Category: long, Host: long, Error: long, Size: 1,
		},
	}
	e := decodeDiscord(t, discordBody(f, long))

	total := units(e.Title) + units(e.Description) + units(e.Footer.Text)
	if n := units(e.Title); n > 256 {
		t.Errorf("the title is %d characters, Discord takes 256", n)
	}
	if n := units(e.Description); n > 4096 {
		t.Errorf("the description is %d characters, Discord takes 4096", n)
	}
	if n := units(e.Footer.Text); n > 2048 {
		t.Errorf("the footer is %d characters, Discord takes 2048", n)
	}
	for _, fl := range e.Fields {
		total += units(fl.Name) + units(fl.Value)
		if n := units(fl.Value); n > 1024 {
			t.Errorf("field %s is %d characters, Discord takes 1024", fl.Name, n)
		}
	}
	if total > 6000 {
		t.Errorf("the embed is %d characters in all, Discord takes 6000", total)
	}
	for _, s := range []string{e.Description, e.field("Error"), e.Footer.Text} {
		if !strings.HasSuffix(s, "…") {
			t.Errorf("a cut value does not say so: %q", s[len(s)-20:])
		}
		// A backslash left before the ellipsis would escape it and the cut
		// would show as a stray character.
		if strings.HasSuffix(strings.TrimSuffix(s, "…"), `\`) {
			t.Errorf("an escape was cut in half: %q", s[len(s)-20:])
		}
	}
}

func TestSendGivesADiscordWebhookItsEmbed(t *testing.T) {
	var gotType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	att := Send(context.Background(), Target{URL: discordAt(t, srv.URL)}, firingWithTask(awkwardName), "box")
	if !att.OK() {
		t.Fatalf("Discord's 204 was not taken as delivered: %+v", att)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type is %q, Discord reads only JSON", gotType)
	}
	if e := decodeDiscord(t, gotBody); e.Title != "Download finished" {
		t.Errorf("the embed is titled %q", e.Title)
	}
	if att.Sent.Body != gotBody || att.Sent.Headers["Content-Type"] != "application/json" {
		t.Errorf("the test panel is told %+v, which is not what went out", att.Sent)
	}
}

func TestSendKeepsABodyTheOperatorWroteForDiscord(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	target := Target{
		URL:     discordAt(t, srv.URL),
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"content":"%%task.name%% is here"}`,
	}
	Send(context.Background(), target, firingWithTask("film.mkv"), "")
	if gotBody != `{"content":"film.mkv is here"}` {
		t.Errorf("the operator's own body was replaced: %s", gotBody)
	}
}

func TestSendKeepsTheDiscordTokenOutOfWhatItReports(t *testing.T) {
	att := Send(context.Background(), Target{URL: discordAt(t, deadAddress(t))}, firingWithTask("x"), "")
	if att.Err == "" {
		t.Fatalf("a dead address answered without an error: %+v", att)
	}
	if strings.Contains(att.Err, discordSecret) {
		t.Errorf("the webhook token is in the error, which reaches the health row and the log: %s", att.Err)
	}
	if !strings.Contains(att.Err, "/api/webhooks/123456789/"+RedactedValue) {
		t.Errorf("the error no longer says which webhook it was: %s", att.Err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message": "Unknown Webhook `+r.URL.Path+`", "code": 10015}`)
	}))
	defer srv.Close()
	att = Send(context.Background(), Target{URL: discordAt(t, srv.URL)}, firingWithTask("x"), "")
	if strings.Contains(att.Body, discordSecret) || strings.Contains(att.ResponseHeaders["X-Echo"], discordSecret) {
		t.Errorf("the answer put the token on screen: %s %v", att.Body, att.ResponseHeaders)
	}
}

func TestSendReadsHowLongTheFarEndAsksToBeLeftAlone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		want    time.Duration
	}{
		{"Discord's own figure", http.StatusTooManyRequests,
			map[string]string{"Retry-After": "2"}, `{"message":"You are being rate limited.","retry_after":1.5,"global":false}`, 1500 * time.Millisecond},
		{"the header alone", http.StatusTooManyRequests, map[string]string{"Retry-After": "2"}, "slow down", 2 * time.Second},
		{"a 429 that says nothing", http.StatusTooManyRequests, nil, "", 0},
		{"the last request of a bucket", http.StatusNoContent,
			map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset-After": "0.25"}, "", 250 * time.Millisecond},
		{"a bucket with room left", http.StatusNoContent,
			map[string]string{"X-RateLimit-Remaining": "4", "X-RateLimit-Reset-After": "0.25"}, "", 0},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			for k, v := range tc.headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		att := Send(context.Background(), Target{URL: srv.URL}, firingWithTask("x"), "")
		srv.Close()
		if att.Wait != tc.want {
			t.Errorf("%s: wait %s, want %s", tc.name, att.Wait, tc.want)
		}
	}
}

func TestARateLimitedTargetWaitsAsLongAsItWasToldBeforeTryingAgain(t *testing.T) {
	fastBackoff(t)
	var mu sync.Mutex
	var hits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits = append(hits, time.Now())
		first := len(hits) == 1
		mu.Unlock()
		if first {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"retry_after":0.3,"global":false}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := New(Options{})
	defer d.Close()
	d.Set([]Target{{ID: "1", Enabled: true, URL: discordAt(t, srv.URL), Triggers: []script.Trigger{script.TriggerTaskDone}, Attempts: 3}})
	d.On(taskDone("film.mkv"))

	waitFor(t, "the message to get through", func() bool { return healthOf(d, "1").Sent == 1 })
	mu.Lock()
	defer mu.Unlock()
	if gap := hits[1].Sub(hits[0]); gap < 300*time.Millisecond {
		t.Errorf("the retry came %s after the 429, which asked for 300ms", gap)
	}
}

func TestATargetThatUsedUpItsAllowanceHoldsTheNextMessage(t *testing.T) {
	var mu sync.Mutex
	var hits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits = append(hits, time.Now())
		mu.Unlock()
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset-After", "0.3")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := New(Options{})
	defer d.Close()
	d.Set([]Target{{ID: "1", Enabled: true, URL: discordAt(t, srv.URL), Triggers: []script.Trigger{script.TriggerTaskDone}}})
	d.On(taskDone("one.mkv"))
	d.On(taskDone("two.mkv"))

	waitFor(t, "both messages to arrive", func() bool { return healthOf(d, "1").Sent == 2 })
	mu.Lock()
	defer mu.Unlock()
	if gap := hits[1].Sub(hits[0]); gap < 300*time.Millisecond {
		t.Errorf("the second message went %s after the first, inside the 300ms the far end asked for", gap)
	}
	if h := healthOf(d, "1"); h.Attempts != 2 {
		t.Errorf("%d attempts for two messages, so one of them ran into the limit", h.Attempts)
	}
}

func TestAWaitLongerThanTheNewsIsWorthDropsTheMessage(t *testing.T) {
	var out lockedBuffer
	prev := log.Writer()
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(prev) })

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	d := New(Options{})
	d.Set([]Target{{ID: "1", Enabled: true, URL: discordAt(t, srv.URL), Triggers: []script.Trigger{script.TriggerTaskDone}, Attempts: 3}})
	d.On(taskDone("film.mkv"))

	waitFor(t, "the message to be given up on", func() bool { return strings.Contains(out.String(), "dropping") })
	done := make(chan struct{})
	go func() {
		d.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker is asleep for the hour the far end asked for")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("the far end was asked %d times inside the hour it asked for", got)
	}
	if strings.Contains(out.String(), discordSecret) {
		t.Errorf("the log carries the webhook token: %s", out.String())
	}
}

// lockedBuffer is a log destination the test can read while a worker writes.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
