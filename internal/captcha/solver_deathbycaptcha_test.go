package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// dbcServer plays Death By Captcha's HTTP API: an upload is recorded and
// answered 303 with uploadBody, and the polls of /captcha/123 get polls in
// turn.
type dbcServer struct {
	*httptest.Server
	mu    sync.Mutex
	forms []url.Values
}

func newDBCServer(t *testing.T, uploadStatus int, uploadBody string, polls ...string) *dbcServer {
	t.Helper()
	s := &dbcServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("%s %s without Accept: application/json", r.Method, r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/captcha":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("upload is not multipart/form-data: %v", err)
			}
			s.forms = append(s.forms, url.Values(r.MultipartForm.Value))
			w.Header().Set("Location", "/captcha/123")
			w.WriteHeader(uploadStatus)
			_, _ = w.Write([]byte(uploadBody))
		case r.Method == http.MethodGet && r.URL.Path == "/captcha/123":
			if len(polls) == 0 {
				t.Error("polled past the last answer")
				return
			}
			_, _ = w.Write([]byte(polls[0]))
			polls = polls[1:]
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *dbcServer) uploads() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.forms...)
}

func (s *dbcServer) solver() *DeathByCaptchaSolver {
	d := NewDeathByCaptchaSolver("user", "pass")
	d.base = s.URL
	return d
}

const dbcPending = `{"captcha":123,"is_correct":true,"status":0,"text":""}`

func TestDeathByCaptchaSolvesAnImageAfterPolling(t *testing.T) {
	withFastPolling(t)
	srv := newDBCServer(t, http.StatusSeeOther, `{"status":0,"captcha":123,"is_correct":1,"text":""}`,
		dbcPending, `{"captcha":123,"is_correct":0,"status":0,"text":null}`,
		`{"captcha":123,"is_correct":true,"status":0,"text":"tyrone slothrop"}`)

	got, err := srv.solver().Solve(context.Background(), imageChallenge(KindImage, "data:image/png;base64,aGVsbG8=", ""))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "tyrone slothrop" {
		t.Errorf("Solve() = %q, want the answer", got)
	}
	up := srv.uploads()[0]
	if up.Get("username") != "user" || up.Get("password") != "pass" || up.Get("captchafile") != "base64:aGVsbG8=" || up.Get("type") != "" {
		t.Errorf("upload = %v, want the login and a base64: captchafile", up)
	}
}

func TestDeathByCaptchaSendsEachTokenTypeWithItsParameters(t *testing.T) {
	const page = "https://host.example/file/abc"
	cases := []struct {
		name    string
		payload WidgetPayload
		typ     string
		field   string
		params  map[string]any
	}{
		{"reCAPTCHA v2 invisible", WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: page, Type: "INVISIBLE"},
			"4", "token_params", map[string]any{"googlekey": "6Lc-key", "pageurl": page}},
		{"reCAPTCHA v3", WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: page, V3Action: `{"action":"example/action"}`},
			"5", "token_params", map[string]any{"googlekey": "6Lc-key", "pageurl": page, "action": "example/action", "min_score": 0.3}},
		{"Turnstile", WidgetPayload{Vendor: VendorTurnstile, SiteKey: "0x4AAAA", SiteURL: page},
			"12", "turnstile_params", map[string]any{"sitekey": "0x4AAAA", "pageurl": page}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withFastPolling(t)
			srv := newDBCServer(t, http.StatusSeeOther, `{"status":0,"captcha":123,"is_correct":1,"text":""}`,
				`{"captcha":123,"is_correct":true,"status":0,"text":"03AGdBq-the-token"}`)

			got, err := srv.solver().Solve(context.Background(), widgetChallenge(c.payload))
			if err != nil {
				t.Fatalf("Solve: %v", err)
			}
			if got != "03AGdBq-the-token" {
				t.Errorf("Solve() = %q, want the token", got)
			}
			up := srv.uploads()[0]
			if up.Get("type") != c.typ {
				t.Errorf("type = %q, want %s", up.Get("type"), c.typ)
			}
			var params map[string]any
			if err := json.Unmarshal([]byte(up.Get(c.field)), &params); err != nil {
				t.Fatalf("%s = %q: %v", c.field, up.Get(c.field), err)
			}
			if len(params) != len(c.params) {
				t.Errorf("%s = %v, want exactly %v", c.field, params, c.params)
			}
			for k, want := range c.params {
				if params[k] != want {
					t.Errorf("%s[%q] = %v, want %v", c.field, k, params[k], want)
				}
			}
		})
	}
}

// The coordinates API reads only reCAPTCHA screenshots, the API documents no
// hCaptcha, v2 Enterprise needs a proxy and v3 has no Enterprise type.
func TestDeathByCaptchaRefusesWhatItsAPICannotTakeWithoutARequest(t *testing.T) {
	srv := newDBCServer(t, http.StatusSeeOther, "")
	s := srv.solver()
	for _, c := range []Challenge{
		imageChallenge(KindClick, "aGVsbG8=", ""),
		widgetChallenge(WidgetPayload{Vendor: VendorHCaptcha, SiteKey: "10000000-ffff", SiteURL: "https://host.example/"}),
		widgetChallenge(WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/", Enterprise: true}),
		widgetChallenge(WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/", V3Action: "login", Enterprise: true}),
	} {
		var r *Refusal
		if err := s.Takes(c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%s %+v: Takes = %v, want a RefusalUnsupported", c.Kind, c.Payload, err)
		}
		if _, err := s.Solve(context.Background(), c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%s %+v: Solve = %v, want a RefusalUnsupported", c.Kind, c.Payload, err)
		}
	}
	if len(srv.uploads()) != 0 {
		t.Error("a captcha Death By Captcha cannot take reached it")
	}
}

// A 403 upload created no captcha, so the next solver may try.
func TestDeathByCaptchaRejectedUploadIsARefusal(t *testing.T) {
	srv := newDBCServer(t, http.StatusForbidden, `{"status":255,"error":"not-logged-in"}`)
	_, err := srv.solver().Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	got := RefusalFor("DeathByCaptcha", err)
	if got.Code != "not-logged-in" || got.Detail != dbcStatusText[http.StatusForbidden] || got.Taken {
		t.Errorf("RefusalFor = %+v, want the body's error, not taken", got)
	}
}

// "?" with is_correct false is Death By Captcha giving up on a captcha it
// took, which must not be handed to JD as the answer.
func TestDeathByCaptchaUnsolvableCaptchaIsATakenTaskWithoutAnAnswer(t *testing.T) {
	withFastPolling(t)
	srv := newDBCServer(t, http.StatusSeeOther, `{"status":0,"captcha":123,"is_correct":1,"text":""}`,
		`{"captcha":123,"is_correct":false,"status":0,"text":"?"}`)
	_, err := srv.solver().Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if got := RefusalFor("DeathByCaptcha", err); got.Code != RefusalNoAnswer || !got.Taken {
		t.Errorf("RefusalFor = %+v, want noAnswer and taken", got)
	}
}

func TestDeathByCaptchaBalanceChecksTheLogin(t *testing.T) {
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		forms = append(forms, r.PostForm)
		switch {
		case r.PostForm.Get("password") == "pass" || r.PostForm.Get("authtoken") == "token":
			_, _ = w.Write([]byte(`{"is_banned":false,"status":0,"rate":0.139,"balance":455.23,"user":43122}`))
		case r.PostForm.Get("username") == "banned":
			_, _ = w.Write([]byte(`{"is_banned":true,"status":0,"rate":0.139,"balance":1,"user":1}`))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	solver := func(user, pass string) *DeathByCaptchaSolver {
		s := NewDeathByCaptchaSolver(user, pass)
		s.base = srv.URL
		return s
	}

	for _, login := range [][2]string{{"user", "pass"}, {"authtoken", "token"}} {
		if got, err := solver(login[0], login[1]).Balance(context.Background()); err != nil || got != 455.23 {
			t.Errorf("Balance for %s = %v, %v, want 455.23 cents", login[0], got, err)
		}
	}
	if f := forms[1]; f.Get("authtoken") != "token" || f.Has("username") || f.Has("password") {
		t.Errorf("token login sent %v, want the authtoken alone", f)
	}
	for _, login := range [][2]string{{"user", "typo"}, {"banned", "x"}} {
		_, err := solver(login[0], login[1]).Balance(context.Background())
		if err == nil || !strings.Contains(err.Error(), "refused the login") {
			t.Errorf("Balance for %s/%s = %v, want the login refused", login[0], login[1], err)
		}
	}
}
