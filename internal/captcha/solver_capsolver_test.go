package captcha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ImageToTextTask answers in createTask at CapSolver, so there is nothing to
// poll for.
func TestCapSolverTakesAnImageAnswerFromCreateTask(t *testing.T) {
	withFastPolling(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/createTask" {
			t.Errorf("unexpected call to %s", r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"errorId":0,"errorCode":"","errorDescription":"","status":"ready","solution":{"text":"44795sds"},"taskId":"2376919c-1863-11ec-a012-94e6f7355a0b"}`))
	}))
	defer srv.Close()

	s := NewCapSolverSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "44795sds" {
		t.Errorf("Solve() = %q, want the text createTask answered with", got)
	}
}

// CapSolver's task ids are strings, and getTaskResult has to be asked for
// the same string, through "idle" and "processing" to "ready".
func TestCapSolverPollsWithItsStringTaskID(t *testing.T) {
	withFastPolling(t)
	const id = "61138bb6-19fb-11ec-a9c8-0242ac110006"
	var asked []json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/createTask":
			_, _ = w.Write([]byte(`{"errorId":0,"status":"idle","taskId":"` + id + `"}`))
		case "/getTaskResult":
			var req antiCaptchaResultReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			asked = append(asked, req.TaskID)
			switch len(asked) {
			case 1:
				_, _ = w.Write([]byte(`{"errorId":0,"status":"idle"}`))
			case 2:
				_, _ = w.Write([]byte(`{"errorId":0,"status":"processing"}`))
			default:
				_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"token":"0.mF74FV8w","type":"turnstile"}}`))
			}
		}
	}))
	defer srv.Close()

	s := NewCapSolverSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), widgetChallenge(WidgetPayload{Vendor: VendorTurnstile, SiteKey: "0x4AAAA", SiteURL: "https://host.example/"}))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "0.mF74FV8w" {
		t.Errorf("Solve() = %q, want the Turnstile token", got)
	}
	if len(asked) != 3 {
		t.Fatalf("getTaskResult was called %d times, want 3", len(asked))
	}
	for _, a := range asked {
		if string(a) != `"`+id+`"` {
			t.Errorf("getTaskResult asked for %s, want the string id %q", a, id)
		}
	}
}
