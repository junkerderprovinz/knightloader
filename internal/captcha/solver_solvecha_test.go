package captcha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ImageToTextTask may answer in createTask, so there is nothing to poll for.
func TestSolvechaTakesAnImageAnswerFromCreateTask(t *testing.T) {
	withFastPolling(t)
	var got struct {
		ClientKey string `json:"clientKey"`
		Task      struct {
			Type string `json:"type"`
			Body string `json:"body"`
		} `json:"task"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/createTask" {
			t.Errorf("unexpected call to %s", r.URL.Path)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","taskId":"t1","solution":{"text":"44795sds"}}`))
	}))
	defer srv.Close()

	s := NewSolvechaSolver("key")
	s.base = srv.URL
	text, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if text != "44795sds" {
		t.Errorf("Solve() = %q, want the text createTask answered with", text)
	}
	if got.ClientKey != "key" || got.Task.Type != "ImageToTextTask" || got.Task.Body == "" {
		t.Errorf("createTask got %+v, want the key and an ImageToTextTask with a body", got)
	}
}

// A task still running after createTask is fetched with its string id.
func TestSolvechaPollsWhileProcessing(t *testing.T) {
	withFastPolling(t)
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/createTask":
			_, _ = w.Write([]byte(`{"errorId":0,"status":"processing","taskId":"1234567"}`))
		case "/getTaskResult":
			var req antiCaptchaResultReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			if string(req.TaskID) != `"1234567"` {
				t.Errorf("getTaskResult asked for %s, want the string id", req.TaskID)
			}
			if polls++; polls < 2 {
				_, _ = w.Write([]byte(`{"errorId":0,"status":"processing"}`))
				return
			}
			_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"text":"abc"}}`))
		}
	}))
	defer srv.Close()

	s := NewSolvechaSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if err != nil || got != "abc" {
		t.Fatalf("Solve() = %q, %v, want abc", got, err)
	}
	if polls != 2 {
		t.Errorf("getTaskResult was called %d times, want 2", polls)
	}
}

// Solvecha reads pictures and text only: widget and click captchas are refused
// before any request goes out.
func TestSolvechaRefusesWhatItDoesNotRead(t *testing.T) {
	s := NewSolvechaSolver("key")
	s.base = "http://127.0.0.1:1"
	for name, c := range map[string]Challenge{
		"turnstile": widgetChallenge(WidgetPayload{Vendor: VendorTurnstile, SiteKey: "0x4AAAA", SiteURL: "https://host.example/"}),
		"recaptcha": widgetChallenge(WidgetPayload{Vendor: VendorRecaptcha, SiteKey: "k", SiteURL: "https://host.example/"}),
		"hcaptcha":  widgetChallenge(WidgetPayload{Vendor: VendorHCaptcha, SiteKey: "k", SiteURL: "https://host.example/"}),
		"click":     imageChallenge(KindClick, "aGVsbG8=", ""),
	} {
		if err := s.Takes(c); err == nil {
			t.Errorf("%s: Takes() = nil, want a refusal", name)
		}
	}
	if err := s.Takes(imageChallenge(KindImage, "aGVsbG8=", "")); err != nil {
		t.Errorf("Takes(image) = %v, want nil", err)
	}
}

// An API error keeps the message of its JSON body, whatever the HTTP status.
func TestSolvechaKeepsTheErrorMessage(t *testing.T) {
	withFastPolling(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"ERROR_KEY_DOES_NOT_EXIST","errorDescription":"Key not found."}`))
	}))
	defer srv.Close()

	s := NewSolvechaSolver("key")
	s.base = srv.URL
	_, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if err == nil || !strings.Contains(err.Error(), "ERROR_KEY_DOES_NOT_EXIST") || !strings.Contains(err.Error(), "Key not found.") {
		t.Errorf("Solve() error = %v, want the code and description of the body", err)
	}
}

// A balance of 0 is a valid answer and never stops a solve.
func TestSolvechaBalanceOfZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getBalance" {
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"errorId":0,"balance":0,"packages":[]}`))
	}))
	defer srv.Close()

	s := NewSolvechaSolver("key")
	s.base = srv.URL
	got, err := s.Balance(context.Background())
	if err != nil || got != 0 {
		t.Errorf("Balance() = %v, %v, want 0 and no error", got, err)
	}
}
