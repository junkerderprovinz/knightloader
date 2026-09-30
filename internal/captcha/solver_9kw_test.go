package captcha

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// nineKWServer plays 9kw's index.cgi: it records every upload's form and every
// GET's query, answers the upload with uploadAnswer, and hands out answers in
// turn to the polls for the solution.
type nineKWServer struct {
	*httptest.Server
	mu      sync.Mutex
	forms   []url.Values
	queries []url.Values
}

func (s *nineKWServer) uploads() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.forms...)
}

func (s *nineKWServer) gets() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.queries...)
}

func newNineKWServer(t *testing.T, uploadAnswer string, answers ...string) *nineKWServer {
	t.Helper()
	s := &nineKWServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path != "/index.cgi" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		if r.Method == http.MethodPost {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("upload is not multipart/form-data: %v", err)
			}
			s.forms = append(s.forms, url.Values(r.MultipartForm.Value))
			_, _ = w.Write([]byte(uploadAnswer))
			return
		}
		s.queries = append(s.queries, r.URL.Query())
		if len(answers) > 0 {
			_, _ = w.Write([]byte(answers[0]))
			answers = answers[1:]
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *nineKWServer) solver() *NineKWSolver {
	n := NewNineKWSolver("the-key")
	n.base = s.URL
	return n
}

func TestNineKWSolvesAnImageAfterPolling(t *testing.T) {
	withFastPolling(t)
	srv := newNineKWServer(t, "130875948", "", "", "VX32JGLK\n")

	got, err := srv.solver().Solve(context.Background(), imageChallenge(KindImage, "data:image/png;base64,aGVsbG8=", "type the red letters"))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "VX32JGLK" {
		t.Errorf("Solve() = %q, want the answer", got)
	}
	up := srv.uploads()[0]
	want := map[string]string{
		"action": "usercaptchaupload", "apikey": "the-key", "base64": "1", "file-upload-01": "aGVsbG8=",
		"textinstructions": "type the red letters", "maxtimeout": nineKWMaxTimeout,
	}
	for k, v := range want {
		if up.Get(k) != v {
			t.Errorf("upload %s = %q, want %q", k, up.Get(k), v)
		}
	}
	if len(srv.gets()) != 3 {
		t.Fatalf("polled %d times, want 3", len(srv.gets()))
	}
	for _, q := range srv.gets() {
		if q.Get("action") != "usercaptchacorrectdata" || q.Get("id") != "130875948" || q.Get("apikey") != "the-key" {
			t.Errorf("poll = %v, want usercaptchacorrectdata for the uploaded id", q)
		}
	}
}

// A click captcha goes up as multimouse, and 9kw's "XxY;XxY" answer becomes
// the click JSON JD reads.
func TestNineKWClickAnswersBecomeJDClickPoints(t *testing.T) {
	for answer, want := range map[string]string{
		"324x184":       `{"x":324,"y":184}`,
		"68x149;81x192": `{"x":[68,81],"y":[149,192]}`,
	} {
		withFastPolling(t)
		srv := newNineKWServer(t, "1", answer)
		got, err := srv.solver().Solve(context.Background(), imageChallenge(KindClick, "aGVsbG8=", ""))
		if err != nil {
			t.Fatalf("Solve(%s): %v", answer, err)
		}
		if got != want {
			t.Errorf("Solve(%s) = %s, want %s", answer, got, want)
		}
		if srv.uploads()[0].Get("multimouse") != "1" {
			t.Errorf("upload = %v, want multimouse set", srv.uploads()[0])
		}
	}
}

func TestNineKWSendsReCAPTCHAV2AndHCaptchaAsInteractiveUploads(t *testing.T) {
	cases := []struct {
		payload WidgetPayload
		source  string
	}{
		{WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/file", Type: "NORMAL"}, "recaptchav2"},
		{WidgetPayload{Vendor: VendorHCaptcha, SiteKey: "10000000-ffff", SiteURL: "https://host.example/file"}, "hcaptcha"},
	}
	for _, c := range cases {
		withFastPolling(t)
		srv := newNineKWServer(t, "22569276", "03AGdBq-the-token")
		got, err := srv.solver().Solve(context.Background(), widgetChallenge(c.payload))
		if err != nil {
			t.Fatalf("%s: Solve: %v", c.source, err)
		}
		if got != "03AGdBq-the-token" {
			t.Errorf("%s: Solve() = %q, want the token", c.source, got)
		}
		up := srv.uploads()[0]
		if up.Get("interactive") != "1" || up.Get("oldsource") != c.source ||
			up.Get("file-upload-01") != c.payload.SiteKey || up.Get("pageurl") != c.payload.SiteURL {
			t.Errorf("%s: upload = %v, want an interactive upload of the site key", c.source, up)
		}
	}
}

// The upload has no field for a v3 action or an invisible or Enterprise key,
// and Turnstile is not among 9kw's variants.
func TestNineKWRefusesWidgetsItsUploadCannotDescribeWithoutARequest(t *testing.T) {
	srv := newNineKWServer(t, "1")
	s := srv.solver()
	for _, p := range []WidgetPayload{
		{Vendor: VendorTurnstile, SiteKey: "0x4AAAA", SiteURL: "https://host.example/"},
		{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/", V3Action: `{"action":"login"}`},
		{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/", Type: "INVISIBLE"},
		{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/", Enterprise: true},
	} {
		c := widgetChallenge(p)
		var r *Refusal
		if err := s.Takes(c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%+v: Takes = %v, want a RefusalUnsupported", p, err)
		}
		if _, err := s.Solve(context.Background(), c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%+v: Solve = %v, want a RefusalUnsupported", p, err)
		}
	}
	if len(srv.uploads())+len(srv.gets()) != 0 {
		t.Error("a captcha 9kw cannot take reached it")
	}
}

// An upload 9kw refuses created nothing, so its code is the refusal and the
// next solver may try.
func TestNineKWUploadErrorIsARefusalWithItsCode(t *testing.T) {
	srv := newNineKWServer(t, "0011 Balance insufficient")
	_, err := srv.solver().Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	want := SolverRefusal{Solver: "9kw.eu", Code: "0011", Detail: "Balance insufficient"}
	if got := RefusalFor("9kw.eu", err); got != want {
		t.Errorf("RefusalFor = %+v, want %+v", got, want)
	}
}

func TestNineKWBalanceChecksTheKey(t *testing.T) {
	srv := newNineKWServer(t, "", "54514", "0002 API key not found")
	s := srv.solver()

	got, err := s.Balance(context.Background())
	if err != nil || got != 54514 {
		t.Errorf("Balance = %v, %v, want 54514 credits", got, err)
	}
	if q := srv.gets()[0]; q.Get("action") != "usercaptchaguthaben" || q.Get("apikey") != "the-key" {
		t.Errorf("balance request = %v, want usercaptchaguthaben with the key", q)
	}
	_, err = s.Balance(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refused the key") || !strings.Contains(err.Error(), "0002") {
		t.Errorf("Balance with a refused key = %v, want the key refused with 0002", err)
	}
}

// The key rides in the query of every GET, so a transport error must not
// carry the URL into a log or onto the accounts page.
func TestNineKWKeepsTheKeyOutOfTransportErrors(t *testing.T) {
	s := NewNineKWSolver("secret-key")
	s.hc = &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}}
	_, err := s.Balance(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Errorf("Balance = %v, want an error without the key", err)
	}
}
