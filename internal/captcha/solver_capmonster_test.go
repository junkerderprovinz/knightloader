package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The two providers that reuse the Anti-Captcha client, as tokenFakeServer
// plays them: the reCAPTCHA and hCaptcha token as gRecaptchaResponse, the
// Turnstile one as token.
var capProviders = map[string]tokenProvider{
	"CapMonster Cloud": {
		name: "CapMonster Cloud",
		build: func(base string) Solver {
			s := NewCapMonsterSolver("key")
			s.base = base
			return s
		},
		solution: capSolution,
	},
	"CapSolver": {
		name: "CapSolver",
		build: func(base string) Solver {
			s := NewCapSolverSolver("key")
			s.base = base
			return s
		},
		solution: capSolution,
	},
}

func capSolution(taskType, token string) string {
	if strings.Contains(taskType, "Turnstile") {
		return fmt.Sprintf(`{"token":%q,"userAgent":"Mozilla/5.0"}`, token)
	}
	return fmt.Sprintf(`{"gRecaptchaResponse":%q}`, token)
}

func TestCapMonsterAndCapSolverNameEachTokenTaskAsTheirDocumentationDoes(t *testing.T) {
	const page, key = "https://host.example/file/abc", "6Lc-site-key"
	v2 := WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, Type: "NORMAL"}
	invisible := WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, Type: "INVISIBLE"}
	enterpriseInvisible := WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, Type: "INVISIBLE", Enterprise: true}
	v3 := WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, V3Action: `{"action":"download"}`}
	v3Enterprise := WidgetPayload{Vendor: VendorRecaptcha, SiteKey: key, SiteURL: page, V3Action: `{"action":"login"}`, Enterprise: true}
	hcaptcha := WidgetPayload{Vendor: VendorHCaptcha, SiteKey: "10000000-ffff", SiteURL: page}
	turnstile := WidgetPayload{Vendor: VendorTurnstile, SiteKey: "0x4AAAA-turnstile", SiteURL: page}

	cases := []struct {
		provider string
		name     string
		payload  WidgetPayload
		want     map[string]any
	}{
		{"CapMonster Cloud", "reCAPTCHA v2", v2, map[string]any{"type": "RecaptchaV2Task", "websiteURL": page, "websiteKey": key}},
		{"CapMonster Cloud", "reCAPTCHA v2 invisible", invisible, map[string]any{"type": "RecaptchaV2Task", "websiteURL": page, "websiteKey": key, "isInvisible": true}},
		{"CapMonster Cloud", "reCAPTCHA v2 Enterprise, invisible", enterpriseInvisible, map[string]any{"type": "RecaptchaV2EnterpriseTask", "websiteURL": page, "websiteKey": key}},
		{"CapMonster Cloud", "reCAPTCHA v3", v3, map[string]any{"type": taskRecaptchaV3, "websiteURL": page, "websiteKey": key, "pageAction": "download", "minScore": 0.3}},
		{"CapMonster Cloud", "reCAPTCHA v3 Enterprise", v3Enterprise, map[string]any{"type": taskRecaptchaV3, "websiteURL": page, "websiteKey": key, "pageAction": "login", "minScore": 0.3, "isEnterprise": true}},
		{"CapMonster Cloud", "hCaptcha", hcaptcha, map[string]any{"type": "HCaptchaTask", "websiteURL": page, "websiteKey": "10000000-ffff"}},
		{"CapMonster Cloud", "Turnstile", turnstile, map[string]any{"type": "TurnstileTask", "websiteURL": page, "websiteKey": "0x4AAAA-turnstile"}},
		{"CapSolver", "reCAPTCHA v2 invisible", invisible, map[string]any{"type": "ReCaptchaV2TaskProxyLess", "websiteURL": page, "websiteKey": key, "isInvisible": true}},
		{"CapSolver", "reCAPTCHA v2 Enterprise, invisible", enterpriseInvisible, map[string]any{"type": "ReCaptchaV2EnterpriseTaskProxyLess", "websiteURL": page, "websiteKey": key, "isInvisible": true}},
		{"CapSolver", "reCAPTCHA v3", v3, map[string]any{"type": "ReCaptchaV3TaskProxyLess", "websiteURL": page, "websiteKey": key, "pageAction": "download"}},
		{"CapSolver", "reCAPTCHA v3 Enterprise", v3Enterprise, map[string]any{"type": "ReCaptchaV3EnterpriseTaskProxyLess", "websiteURL": page, "websiteKey": key, "pageAction": "login"}},
		{"CapSolver", "Turnstile", turnstile, map[string]any{"type": "AntiTurnstileTaskProxyLess", "websiteURL": page, "websiteKey": "0x4AAAA-turnstile"}},
	}
	for _, c := range cases {
		t.Run(c.provider+"/"+c.name, func(t *testing.T) {
			withFastPolling(t)
			p := capProviders[c.provider]
			srv, task := tokenFakeServer(t, p, "03AGdBq-the-token")
			defer srv.Close()

			got, err := p.build(srv.URL).Solve(context.Background(), widgetChallenge(c.payload))
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

func TestCapMonsterAndCapSolverRefuseWhatTheyDoNotTakeWithoutARequest(t *testing.T) {
	click := imageChallenge(KindClick, "aGVsbG8=", "click the apple")
	hcaptcha := widgetChallenge(WidgetPayload{Vendor: VendorHCaptcha, SiteKey: "10000000-ffff", SiteURL: "https://host.example/"})
	cases := []struct {
		provider string
		c        Challenge
	}{
		{"CapMonster Cloud", click},
		{"CapSolver", click},
		{"CapSolver", hcaptcha},
	}
	for _, c := range cases {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
		s := capProviders[c.provider].build(srv.URL)
		var r *Refusal
		if err := s.Takes(c.c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%s, %s: Takes = %v, want a RefusalUnsupported", c.provider, c.c.Kind, err)
		}
		if _, err := s.Solve(context.Background(), c.c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
			t.Errorf("%s, %s: Solve = %v, want a RefusalUnsupported", c.provider, c.c.Kind, err)
		}
		if called {
			t.Errorf("%s, %s: the captcha reached the provider", c.provider, c.c.Kind)
		}
		srv.Close()
	}
}

// CapMonster's ImageToTextTask documents no comment, so the prompt is not
// sent, and the answer is polled for as at Anti-Captcha.
func TestCapMonsterSolvesAnImageWithoutAComment(t *testing.T) {
	withFastPolling(t)
	var task map[string]any
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/createTask":
			var req struct {
				Task map[string]any `json:"task"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			task = req.Task
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":7654321}`))
		case "/getTaskResult":
			if polls++; polls < 3 {
				_, _ = w.Write([]byte(`{"errorId":0,"status":"processing"}`))
				return
			}
			_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"text":"answer"}}`))
		}
	}))
	defer srv.Close()

	s := NewCapMonsterSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), imageChallenge(KindImage, "data:image/png;base64,aGVsbG8=", "type the red letters"))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "answer" || polls != 3 {
		t.Errorf("Solve() = %q after %d polls, want the text after the third", got, polls)
	}
	if len(task) != 2 || task["type"] != "ImageToTextTask" || task["body"] != "aGVsbG8=" {
		t.Errorf("task = %v, want only type and body", task)
	}
}

// CapMonster's createTask refusal keeps its code, and no task exists, so the
// next solver may try.
func TestCapMonsterReportsItsRefusalCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"ERROR_ZERO_BALANCE","errorDescription":"Insufficient funds","taskId":0}`))
	}))
	defer srv.Close()

	s := NewCapMonsterSolver("key")
	s.base = srv.URL
	_, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if got := RefusalFor("CapMonster Cloud", err); got.Code != "ERROR_ZERO_BALANCE" || got.Taken {
		t.Errorf("RefusalFor = %+v, want ERROR_ZERO_BALANCE, not taken", got)
	}
}
