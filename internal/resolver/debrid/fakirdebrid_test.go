package debrid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A PIN in the shape JDownloader accepts, so a test can also check that it
// never leaks into an error.
const fakirdebridTestPIN = "0123456789abcdef0123"

func fakirdebridAt(base string) *FakirDebrid {
	f := NewFakirDebrid(fakirdebridTestPIN)
	f.base = base
	f.poll = time.Millisecond
	return f
}

// fakirdebridServe answers each call by its path without the PIN, and fails
// the test for a call that does not end in the PIN.
func fakirdebridServe(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutSuffix(r.URL.Path, "/"+fakirdebridTestPIN)
		if !ok {
			t.Errorf("%s does not end in the PIN", r.URL.Path)
		}
		body, known := routes[path]
		if !known {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFakirDebridHostsSkipsHostsThatAreNotWorking(t *testing.T) {
	srv := fakirdebridServe(t, map[string]string{
		"/hosts": `{"supportedhosts":[
			{"host":"rapidgator.net","currently_working":true,"resumable":true,"maxChunks":4,"maxDownloads":2,"traffixmax_daily":0,"trafficleft":0},
			{"host":"www.1Fichier.com","currently_working":true},
			{"host":"deadhost.example","currently_working":false},
			{"host":"quiethost.example","currently_working":"0"},
			{"host":"nofield.example"},
			{"host":"notadomain","currently_working":true}]}`,
	})
	hosts, err := fakirdebridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "1fichier.com", "nofield.example"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, gone := range []string{"deadhost.example", "quiethost.example", "notadomain"} {
		if hosts[gone] {
			t.Errorf("Hosts claimed %q: %v", gone, hosts)
		}
	}
}

// The body is the one the live API answers for a wrong PIN.
func TestFakirDebridHostsRefusesAWrongPIN(t *testing.T) {
	srv := fakirdebridServe(t, map[string]string{
		"/hosts": `{"status":"error","success":false,"code":"CODE34","message":"CODE34 - The provided PIN is invalid.","server_time":1790240910}`,
	})
	_, err := fakirdebridAt(srv.URL).Hosts(context.Background())
	if err == nil {
		t.Fatal("Hosts succeeded against a refused PIN")
	}
	if !strings.Contains(err.Error(), "API PIN was refused") || !strings.Contains(err.Error(), "The provided PIN is invalid.") {
		t.Errorf("error = %q, want the translation and the service's own message", err)
	}
	if strings.Contains(err.Error(), fakirdebridTestPIN) {
		t.Errorf("error = %q carries the PIN", err)
	}
}

func TestFakirDebridUnlockWaitsForTheTransferToComplete(t *testing.T) {
	var polls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate/" + fakirdebridTestPIN:
			if r.Method != http.MethodPost {
				t.Errorf("/generate was a %s, want POST", r.Method)
			}
			if got := r.PostFormValue("url"); got != "https://rapidgator.net/file/x" {
				t.Errorf("url field = %q, want the hoster link", got)
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"resumable":true,"maxchunks":4,"link":%q}}`, srv.URL+"/transfer/abc")
		case "/transfer/abc":
			if polls.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"success":true,"data":{"state":"processing","completed":"40"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"success":true,"data":{"state":"completed","completed":100,"link":"https://dl.fakirdebrid.example/abc/File.ext"}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := fakirdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.fakirdebrid.example/abc/File.ext" {
		t.Errorf("Unlock = %+v, want the link from the completed transfer", got)
	}
	if n := polls.Load(); n != 2 {
		t.Errorf("transfer polled %d times, want 2", n)
	}
}

func TestFakirDebridUnlockTellsDeadLinkUnsupportedHostAndQuotaApart(t *testing.T) {
	cases := []struct {
		code, message, want string
	}{
		{"CODE32", "CODE32 - File removed by owner", "file is gone"},
		{"CODE33", "CODE33 - File ID does not exist", "file is gone"},
		{"CODE30", "CODE30 - Host not supported", "does not support this hoster"},
		{"CODE8", "CODE8 - Daily download limit reached", "daily download limit"},
		{"CODE13", "CODE13 - Daily download limit reached for hoster", "limit for this hoster"},
		{"CODE6", "CODE6 - Transfer limit reached", "no traffic left"},
		{"Password_Required", "Password required", "password-protected"},
		{"CODE31", "CODE31 - Unknown error", "CODE31 - Unknown error"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			srv := fakirdebridServe(t, map[string]string{
				"/generate": fmt.Sprintf(`{"status":"error","success":false,"code":%q,"message":%q,"server_time":1790240910}`, c.code, c.message),
			})
			_, err := fakirdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
			if err == nil {
				t.Fatal("Unlock succeeded against an error answer")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "fakirdebrid ") || !strings.Contains(msg, c.want) || !strings.Contains(msg, c.message) {
				t.Errorf("error = %q, want the service named, %q and the service's message", msg, c.want)
			}
		})
	}
}

func TestFakirDebridUnlockReportsAFailedTransfer(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/generate/") {
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"link":%q}}`, srv.URL+"/transfer/"+fakirdebridTestPIN)
			return
		}
		_, _ = io.WriteString(w, `{"success":false,"code":"CODE22","message":"CODE22 - File download link removed"}`)
	}))
	defer srv.Close()

	_, err := fakirdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil {
		t.Fatal("Unlock succeeded although the transfer failed")
	}
	if !strings.Contains(err.Error(), "CODE22 - File download link removed") {
		t.Errorf("error = %q, want the transfer's own message", err)
	}
	// The status link came from the service, and it may carry the PIN too.
	if strings.Contains(err.Error(), fakirdebridTestPIN) {
		t.Errorf("error = %q carries the PIN", err)
	}
}

// fakirdebridProcessing serves a transfer that never finishes, and calls
// polled for each status call.
func fakirdebridProcessing(t *testing.T, polled func()) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/generate/") {
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"link":%q}}`, srv.URL+"/transfer/abc")
			return
		}
		polled()
		_, _ = io.WriteString(w, `{"success":true,"data":{"state":"processing","completed":55}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An error that comes while the context is still live can say how far the
// transfer got; one cut off by the deadline only says that time ran out.
func TestFakirDebridUnlockReportsProgressBeforeTheDeadline(t *testing.T) {
	var polls atomic.Int32
	srv := fakirdebridProcessing(t, func() { polls.Add(1) })
	f := fakirdebridAt(srv.URL)
	f.poll = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), f.poll)
	defer cancel()

	_, err := f.Unlock(ctx, "https://rapidgator.net/file/x")
	if ctx.Err() != nil {
		t.Fatalf("Unlock returned after its deadline: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "still on its way") || !strings.Contains(err.Error(), "55%") {
		t.Fatalf("error = %v, want the transfer reported as unfinished with its progress", err)
	}
	if n := polls.Load(); n != 1 {
		t.Errorf("transfer polled %d times, want 1", n)
	}
}

// A status call may take the whole call timeout, so none goes out once a wait
// and such a call no longer fit before the deadline, however fast the service
// would have answered.
func TestFakirDebridUnlockSendsNoStatusCallTheDeadlineCouldCutOff(t *testing.T) {
	var polls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/generate/") {
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"link":%q}}`, srv.URL+"/transfer/abc")
			return
		}
		if polls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"success":true,"data":{"state":"processing","completed":55}}`)
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"data":{"state":"completed","link":"https://dl.fakirdebrid.example/abc/File.ext"}}`)
	}))
	defer srv.Close()
	f := fakirdebridAt(srv.URL)
	f.poll = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), f.poll+fakirdebridCallTimeout)
	defer cancel()

	_, err := f.Unlock(ctx, "https://rapidgator.net/file/x")
	if n := polls.Load(); n != 1 {
		t.Fatalf("transfer polled %d times, want no second status call with less than a whole call's time left", n)
	}
	if err == nil || !strings.Contains(err.Error(), "still on its way") {
		t.Errorf("error = %v, want the transfer reported as unfinished", err)
	}
}

func TestFakirDebridUnlockStopsWaitingWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polled := make(chan struct{}, 1)
	srv := fakirdebridProcessing(t, func() {
		select {
		case polled <- struct{}{}:
		default:
		}
	})
	f := fakirdebridAt(srv.URL)
	f.poll = time.Hour

	done := make(chan error, 1)
	go func() {
		_, err := f.Unlock(ctx, "https://rapidgator.net/file/x")
		done <- err
	}()
	<-polled
	// Long enough for the answer to arrive, so the cancel meets the wait and
	// not the status call.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Unlock = %v, want it to end with the context", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Unlock kept waiting after its context was cancelled")
	}
}

func TestFakirDebridErrorsNeverCarryThePIN(t *testing.T) {
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<!DOCTYPE html><title>Just a moment...</title>")
	}))
	defer cloudflare.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()

	for name, base := range map[string]string{"challenge page": cloudflare.URL, "unreachable": gone.URL} {
		_, err := fakirdebridAt(base).Hosts(context.Background())
		if err == nil {
			t.Fatalf("%s: Hosts succeeded", name)
		}
		if strings.Contains(err.Error(), fakirdebridTestPIN) {
			t.Errorf("%s: error = %q carries the PIN", name, err)
		}
	}
}

func TestFakirDebridAccountReadsPlanExpiryAndTraffic(t *testing.T) {
	// server_time is a year off the local clock; only the difference counts.
	serverNow := time.Now().Add(-365 * 24 * time.Hour).Unix()
	srv := fakirdebridServe(t, map[string]string{
		"/account": fmt.Sprintf(`{"success":true,"message":"OK","server_time":%d,
			"account":{"username":"amy","plan":"VIP","premium_until":"%d",
			"traffic":{"left":"750","limit":1000}}}`, serverNow, serverNow+30*24*3600),
	})
	info, err := fakirdebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "VIP" {
		t.Errorf("Tier = %q, want the plan name", info.Tier)
	}
	if d := time.Until(info.ExpiresAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("ExpiresAt is %v away, want about 30 days", d)
	}
	if info.Traffic.LimitBytes != 1000 || info.Traffic.UsedBytes != 250 || info.Traffic.Unlimited {
		t.Errorf("Traffic = %+v, want 250 of 1000 bytes used", info.Traffic)
	}
}

func TestFakirDebridAccountRefusesAnAnswerWithoutAccountDetails(t *testing.T) {
	srv := fakirdebridServe(t, map[string]string{
		"/account": `{"success":true,"message":"OK","server_time":1790240910}`,
	})
	info, err := fakirdebridAt(srv.URL).Account(context.Background())
	if err == nil {
		t.Fatalf("Account = %+v, want an error rather than a free account", info)
	}
}

func TestFakirDebridAccountWithoutPremiumIsFree(t *testing.T) {
	srv := fakirdebridServe(t, map[string]string{
		"/account": `{"success":true,"server_time":1790240910,
			"account":{"username":"amy","plan":"Premium","premium_until":0,"traffic":{"left":0,"limit":0}}}`,
	})
	info, err := fakirdebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" {
		t.Errorf("Tier = %q, want free", info.Tier)
	}
	if !info.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time", info.ExpiresAt)
	}
	if info.Traffic != (TrafficInfo{}) {
		t.Errorf("Traffic = %+v, want nothing for an account without a limit", info.Traffic)
	}
}
