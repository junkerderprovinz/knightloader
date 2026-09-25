package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// tokenProvider builds one provider's solver against a fake server and says
// how that provider writes a token into a ready solution.
type tokenProvider struct {
	name     string
	build    func(base string) Solver
	solution func(taskType, token string) string
}

var tokenProviders = []tokenProvider{
	{
		name: "2Captcha",
		build: func(base string) Solver {
			s := NewTwoCaptchaSolver("key")
			s.base = base
			return s
		},
		// Both names for reCAPTCHA, token alone for Turnstile, as documented.
		solution: func(taskType, token string) string {
			if taskType == taskTurnstile {
				return fmt.Sprintf(`{"token":%q,"userAgent":"Mozilla/5.0"}`, token)
			}
			return fmt.Sprintf(`{"gRecaptchaResponse":%q,"token":%q}`, token, token)
		},
	},
	{
		name: "Anti-Captcha",
		build: func(base string) Solver {
			s := NewAntiCaptchaSolver("key")
			s.base = base
			return s
		},
		solution: func(taskType, token string) string {
			if taskType == taskTurnstile {
				return fmt.Sprintf(`{"token":%q,"userAgent":"Mozilla/5.0"}`, token)
			}
			return fmt.Sprintf(`{"gRecaptchaResponse":%q,"userAgent":"Mozilla/5.0"}`, token)
		},
	},
}

// tokenFakeServer records the task createTask was sent and answers
// getTaskResult with the provider's ready solution for it.
func tokenFakeServer(t *testing.T, p tokenProvider, token string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var task map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/createTask":
			var req struct {
				Task map[string]any `json:"task"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			task = req.Task
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":99}`))
		case "/getTaskResult":
			typ, _ := task["type"].(string)
			_, _ = fmt.Fprintf(w, `{"errorId":0,"status":"ready","solution":%s}`, p.solution(typ, token))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	return srv, &task
}

func widgetChallenge(p WidgetPayload) Challenge {
	return Challenge{ID: "5", Host: "host.example", Kind: KindWidget, Payload: &p}
}

func TestEveryTokenTypeGoesToBothProvidersAsTheirDocumentedTask(t *testing.T) {
	const page, key = "https://host.example/file/abc", "6Lc-site-key"
	cases := []struct {
		name    string
		payload WidgetPayload
		want    map[string]any
	}{
		{
			"reCAPTCHA v2",
			WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, ContextURL: "https://host.example", Type: "NORMAL"},
			map[string]any{"type": taskRecaptchaV2, "websiteURL": page, "websiteKey": key},
		},
		{
			"reCAPTCHA v2 invisible",
			WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, Type: "INVISIBLE"},
			map[string]any{"type": taskRecaptchaV2, "websiteURL": page, "websiteKey": key, "isInvisible": true},
		},
		{
			"reCAPTCHA v2 Enterprise",
			WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, Type: "NORMAL", Enterprise: true},
			map[string]any{"type": taskRecaptchaV2Enterprise, "websiteURL": page, "websiteKey": key},
		},
		{
			"reCAPTCHA v3",
			WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, V3Action: `{"action":"download"}`},
			map[string]any{"type": taskRecaptchaV3, "websiteURL": page, "websiteKey": key, "pageAction": "download", "minScore": 0.3},
		},
		{
			"reCAPTCHA v3 Enterprise",
			WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, V3Action: `{"action":"login"}`, Enterprise: true},
			map[string]any{"type": taskRecaptchaV3, "websiteURL": page, "websiteKey": key, "pageAction": "login", "minScore": 0.3, "isEnterprise": true},
		},
		{
			"Cloudflare Turnstile",
			WidgetPayload{Vendor: VendorTurnstile, SiteKey: "0x4AAAA-turnstile", SiteURL: page},
			map[string]any{"type": taskTurnstile, "websiteURL": page, "websiteKey": "0x4AAAA-turnstile"},
		},
	}
	for _, p := range tokenProviders {
		for _, c := range cases {
			t.Run(p.name+"/"+c.name, func(t *testing.T) {
				withFastPolling(t)
				srv, task := tokenFakeServer(t, p, "03AGdBq-the-token")
				defer srv.Close()

				ch := widgetChallenge(c.payload)
				s := p.build(srv.URL)
				if err := s.Takes(ch); err != nil {
					t.Fatalf("Takes = %v, want nil", err)
				}
				got, err := s.Solve(context.Background(), ch)
				if err != nil {
					t.Fatalf("Solve: %v", err)
				}
				if got != "03AGdBq-the-token" {
					t.Errorf("Solve() = %q, want the token from the solution", got)
				}
				if len(*task) != len(c.want) {
					t.Errorf("task = %v, want exactly %v", *task, c.want)
				}
				for k, want := range c.want {
					if (*task)[k] != want {
						t.Errorf("task[%q] = %v, want %v", k, (*task)[k], want)
					}
				}
			})
		}
	}
}

// Neither provider lists hCaptcha, so both refuse it without a request and the
// challenge stays with the person at the prompt.
func TestNeitherProviderIsSentAnHCaptcha(t *testing.T) {
	for _, p := range tokenProviders {
		t.Run(p.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			}))
			defer srv.Close()

			ch := widgetChallenge(WidgetPayload{Vendor: VendorHCaptcha, SiteKey: "10000000-ffff-ffff-ffff-000000000001", SiteURL: "https://host.example/"})
			s := p.build(srv.URL)
			var r *Refusal
			if err := s.Takes(ch); !errors.As(err, &r) || r.Code != RefusalUnsupported {
				t.Errorf("Takes = %v, want a RefusalUnsupported", err)
			}
			if _, err := s.Solve(context.Background(), ch); !errors.As(err, &r) || r.Code != RefusalUnsupported {
				t.Errorf("Solve = %v, want a RefusalUnsupported", err)
			}
			if called {
				t.Error("an hCaptcha reached the provider")
			}
		})
	}
}

// JD always writes the page's scheme and host as contextUrl, so a payload
// without the full address still names the site.
func TestATokenTaskFallsBackToTheContextURL(t *testing.T) {
	typ, f, err := tokenTask(widgetChallenge(WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "k", ContextURL: "https://host.example"}))
	if err != nil || typ != taskRecaptchaV2 || f.WebsiteURL != "https://host.example" {
		t.Errorf("tokenTask = %s %+v %v, want a v2 task on the context URL", typ, f, err)
	}
}

// The hoster refuses a v3 token asked for without its action, so such a
// captcha is not paid for. The widget page refuses the same payloads.
func TestAReCAPTCHAV3WithoutAUsableActionIsNeverSent(t *testing.T) {
	for _, action := range []string{`{"action":""}`, `{"foo":1}`, `{"action":`, `log in`} {
		for _, p := range tokenProviders {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			}))
			ch := widgetChallenge(WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "6Lc-key", SiteURL: "https://host.example/", V3Action: action})
			s := p.build(srv.URL)
			var r *Refusal
			if err := s.Takes(ch); !errors.As(err, &r) || r.Code != RefusalUnsupported {
				t.Errorf("%s, action %s: Takes = %v, want a RefusalUnsupported", p.name, action, err)
			}
			if _, err := s.Solve(context.Background(), ch); !errors.As(err, &r) {
				t.Errorf("%s, action %s: Solve = %v, want a refusal", p.name, action, err)
			}
			if called {
				t.Errorf("%s, action %s: the captcha reached the provider", p.name, action)
			}
			srv.Close()
		}
	}
}

func TestAWidgetWithoutAKnownVendorIsRefused(t *testing.T) {
	ch := widgetChallenge(WidgetPayload{SiteKey: "k", SiteURL: "https://host.example/"})
	for _, p := range tokenProviders {
		var r *Refusal
		if err := p.build("http://127.0.0.1:1").Takes(ch); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%s: Takes = %v, want a RefusalUnsupported", p.name, err)
		}
	}
}

func TestRefusalForNamesWhyASolverDidNotDeliver(t *testing.T) {
	cases := []struct {
		err  error
		want SolverRefusal
	}{
		{&Refusal{Code: "ERROR_ZERO_BALANCE", Detail: "no funds"}, SolverRefusal{Solver: "2Captcha", Code: "ERROR_ZERO_BALANCE", Detail: "no funds"}},
		{unsupported(taskHCaptcha), SolverRefusal{Solver: "2Captcha", Code: RefusalUnsupported, Detail: taskHCaptcha}},
		{fmt.Errorf("%w: timeout", ErrTaskTaken), SolverRefusal{Solver: "2Captcha", Code: RefusalNoAnswer, Taken: true}},
		{
			fmt.Errorf("%w: %w", ErrTaskTaken, &Refusal{Code: "ERROR_CAPTCHA_UNSOLVABLE", Detail: "workers gave up"}),
			SolverRefusal{Solver: "2Captcha", Code: "ERROR_CAPTCHA_UNSOLVABLE", Detail: "workers gave up", Taken: true},
		},
		{errors.New("dial tcp: connection refused"), SolverRefusal{Solver: "2Captcha", Code: RefusalFailed, Detail: "dial tcp: connection refused"}},
	}
	for _, c := range cases {
		if got := RefusalFor("2Captcha", c.err); got != c.want {
			t.Errorf("RefusalFor(%v) = %+v, want %+v", c.err, got, c.want)
		}
	}
}
