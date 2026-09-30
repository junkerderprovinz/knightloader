package captcha

// AntiCaptchaSolver against an httptest.Server, covering what differs from
// 2Captcha's wire shape. The shared helpers are tested in
// solver_2captcha_test.go, beside where they are defined.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
			t.Errorf("unexpected path %s", r.URL.Path)
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
	got, err := s.Solve(context.Background(), imageChallenge(KindImage, "data:image/png;base64,aGVsbG8=", ""))
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
	got, err := s.Solve(context.Background(), imageChallenge(KindClick, "aGVsbG8=", "click the apple"))
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
	got, err := s.Solve(context.Background(), imageChallenge(KindClick, "aGVsbG8=", ""))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != `{"x":[1,3,5],"y":[2,4,6]}` {
		t.Errorf("Solve() = %s, want the MultiClickedPoint shape for three resolved points", got)
	}
}

// getBalance is the same request at all four createTask-style providers; each
// words a key it does not know in its own code.
func TestBalanceChecksTheKeyAtEveryCreateTaskProvider(t *testing.T) {
	type balancer interface {
		Balance(context.Context) (float64, error)
	}
	cases := []struct {
		name    string
		build   func(key, base string) balancer
		refused string
	}{
		{"2Captcha", func(key, base string) balancer {
			s := NewTwoCaptchaSolver(key)
			s.base = base
			return s
		}, "ERROR_KEY_DOES_NOT_EXIST"},
		{"Anti-Captcha", func(key, base string) balancer {
			s := NewAntiCaptchaSolver(key)
			s.base = base
			return s
		}, "ERROR_KEY_DOES_NOT_EXIST"},
		{"CapMonster Cloud", func(key, base string) balancer {
			s := NewCapMonsterSolver(key)
			s.base = base
			return s
		}, "ERROR_KEY_DOES_NOT_EXIST"},
		{"CapSolver", func(key, base string) balancer {
			s := NewCapSolverSolver(key)
			s.base = base
			return s
		}, "ERROR_KEY_DENIED_ACCESS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ClientKey string `json:"clientKey"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if r.URL.Path != "/getBalance" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if req.ClientKey != "good-key" {
					_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"` + c.refused + `","errorDescription":"Wrong account key"}`))
					return
				}
				_, _ = w.Write([]byte(`{"errorId":0,"balance":12.3456}`))
			}))
			defer srv.Close()

			got, err := c.build("good-key", srv.URL).Balance(context.Background())
			if err != nil || got != 12.3456 {
				t.Errorf("Balance with a good key = %v, %v, want 12.3456", got, err)
			}
			_, err = c.build("typo", srv.URL).Balance(context.Background())
			if err == nil || !strings.Contains(err.Error(), "refused the key") || !strings.Contains(err.Error(), c.refused) {
				t.Errorf("Balance with a wrong key = %v, want the key refused with %s", err, c.refused)
			}
		})
	}
}

func TestAntiCaptchaSolverNoKeyConfigured(t *testing.T) {
	s := NewAntiCaptchaSolver("")
	if _, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", "")); err == nil {
		t.Error("Solve() with no API key = nil error, want one")
	}
}
