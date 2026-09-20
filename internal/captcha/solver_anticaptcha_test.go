package captcha

// AntiCaptchaSolver against an httptest.Server, covering what differs from
// 2Captcha's wire shape. The shared helpers are tested in
// solver_2captcha_test.go, beside where they are defined.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func antiCaptchaFakeServer(t *testing.T, createBody, resultBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/createTask":
			_, _ = w.Write([]byte(createBody))
		case "/getTaskResult":
			_, _ = w.Write([]byte(resultBody))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestAntiCaptchaSolverSolvesImage(t *testing.T) {
	withFastPolling(t)
	srv := antiCaptchaFakeServer(t, `{"errorId":0,"taskId":7654321}`,
		`{"errorId":0,"status":"ready","solution":{"text":"deditur","url":"http://x/1.jpg"}}`)
	defer srv.Close()

	s := NewAntiCaptchaSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), KindImage, "data:image/png;base64,aGVsbG8=", "")
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "deditur" {
		t.Errorf("Solve() = %q, want the solved text with the url ignored", got)
	}
}

// A KindClick task is an ImageToCoordinatesTask with mode "points" sent
// explicitly, and a coordinate row reduces to its first two numbers.
func TestAntiCaptchaSolverSolvesClickSetsPointsMode(t *testing.T) {
	withFastPolling(t)
	var gotReq antiCaptchaCreateReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createTask" {
			_ = json.NewDecoder(r.Body).Decode(&gotReq)
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"coordinates":[[358,268]]}}`))
	}))
	defer srv.Close()

	s := NewAntiCaptchaSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), KindClick, "aGVsbG8=", "click the apple")
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != `{"x":358,"y":268}` {
		t.Errorf("Solve() = %s, want the single-point ClickedPoint JSON", got)
	}
	if gotReq.Task.Type != "ImageToCoordinatesTask" || gotReq.Task.Mode != "points" || gotReq.Task.Comment != "click the apple" {
		t.Errorf("createTask task = %+v, want ImageToCoordinatesTask in points mode with the comment", gotReq.Task)
	}
}

func TestAntiCaptchaSolverMultiPointClick(t *testing.T) {
	withFastPolling(t)
	srv := antiCaptchaFakeServer(t, `{"errorId":0,"taskId":1}`,
		`{"errorId":0,"status":"ready","solution":{"coordinates":[[1,2],[3,4],[5,6]]}}`)
	defer srv.Close()

	s := NewAntiCaptchaSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), KindClick, "aGVsbG8=", "")
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != `{"x":[1,3,5],"y":[2,4,6]}` {
		t.Errorf("Solve() = %s, want the MultiClickedPoint shape for three resolved points", got)
	}
}

func TestAntiCaptchaSolverGetTaskResultError(t *testing.T) {
	withFastPolling(t)
	srv := antiCaptchaFakeServer(t, `{"errorId":0,"taskId":1}`,
		`{"errorId":12,"errorCode":"ERROR_CAPTCHA_UNSOLVABLE","errorDescription":"workers could not solve it"}`)
	defer srv.Close()

	s := NewAntiCaptchaSolver("key")
	s.base = srv.URL
	if _, err := s.Solve(context.Background(), KindImage, "aGVsbG8=", ""); err == nil {
		t.Error("Solve() with an unsolvable result = nil error, want one")
	}
}

func TestAntiCaptchaSolverNoKeyConfigured(t *testing.T) {
	s := NewAntiCaptchaSolver("")
	if _, err := s.Solve(context.Background(), KindImage, "aGVsbG8=", ""); err == nil {
		t.Error("Solve() with no API key = nil error, want one")
	}
}
