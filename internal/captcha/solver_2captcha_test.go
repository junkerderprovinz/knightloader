package captcha

// Shared-helper tests (decodeSolverImage, encodeClickAnswer, solverAnswerFor,
// solverSleep) live here beside where those functions are defined, and
// TwoCaptchaSolver's own HTTP-level tests follow the TestJDClientXxx style
// jdclient_test.go already established for this package: a real
// httptest.Server pinning the wire shapes this file's own package comment
// documents as verified, so a future edit that "simplifies" a field name
// fails loudly here instead of only against a real account.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// withFastPolling shortens solverPollInterval/solverMaxWait for the life of
// one test, restoring both on cleanup - see their own var doc comment for
// why they are var rather than const.
func withFastPolling(t *testing.T) {
	t.Helper()
	prevInterval, prevWait := solverPollInterval, solverMaxWait
	solverPollInterval, solverMaxWait = time.Millisecond, 2*time.Second
	t.Cleanup(func() { solverPollInterval, solverMaxWait = prevInterval, prevWait })
}

func TestDecodeSolverImageStripsDataURLPrefix(t *testing.T) {
	got, err := decodeSolverImage("data:image/png;base64,aGVsbG8=")
	if err != nil {
		t.Fatalf("decodeSolverImage: %v", err)
	}
	if got != "aGVsbG8=" {
		t.Errorf("decodeSolverImage() = %q, want the bare base64", got)
	}
}

func TestDecodeSolverImageAcceptsBareBase64(t *testing.T) {
	got, err := decodeSolverImage("aGVsbG8=")
	if err != nil {
		t.Fatalf("decodeSolverImage: %v", err)
	}
	if got != "aGVsbG8=" {
		t.Errorf("decodeSolverImage() = %q, want the input echoed back canonically", got)
	}
}

func TestDecodeSolverImageRejectsEmptyAndGarbage(t *testing.T) {
	if _, err := decodeSolverImage(""); err == nil {
		t.Error("decodeSolverImage(\"\") = nil error, want one")
	}
	if _, err := decodeSolverImage("data:image/png;base64,"); err == nil {
		t.Error("decodeSolverImage of an empty payload = nil error, want one")
	}
	if _, err := decodeSolverImage("not base64 at all!!"); err == nil {
		t.Error("decodeSolverImage of garbage = nil error, want one")
	}
}

// TestEncodeClickAnswerSingleVsMulti pins the two JD-verified shapes - see
// encodeClickAnswer's own doc comment for the JDownloader source citations.
func TestEncodeClickAnswerSingleVsMulti(t *testing.T) {
	one, err := encodeClickAnswer([]solverPoint{{X: 12, Y: 34}})
	if err != nil {
		t.Fatalf("encodeClickAnswer(one point): %v", err)
	}
	if one != `{"x":12,"y":34}` {
		t.Errorf("encodeClickAnswer(one point) = %s, want the ClickedPoint shape", one)
	}

	many, err := encodeClickAnswer([]solverPoint{{X: 1, Y: 2}, {X: 3, Y: 4}})
	if err != nil {
		t.Fatalf("encodeClickAnswer(two points): %v", err)
	}
	if many != `{"x":[1,3],"y":[2,4]}` {
		t.Errorf("encodeClickAnswer(two points) = %s, want the MultiClickedPoint shape (parallel arrays)", many)
	}

	if _, err := encodeClickAnswer(nil); err == nil {
		t.Error("encodeClickAnswer(nil) = nil error, want one - there is no answer to submit")
	}
}

func TestSolverAnswerForUnsupportedKind(t *testing.T) {
	if _, err := solverAnswerFor(KindWidget, "text", nil); err != ErrUnsupportedKind {
		t.Errorf("solverAnswerFor(KindWidget) error = %v, want ErrUnsupportedKind", err)
	}
	if _, err := solverAnswerFor(KindImage, "", nil); err == nil {
		t.Error("solverAnswerFor(KindImage, \"\") = nil error, want one - an empty answer is not a solve")
	}
}

// twoCaptchaFakeServer wires up createTask/getTaskResult against fixed
// bodies, returning "processing" resultsBeforeReady times before "ready" -
// exercising the poll loop, not just the happy path's first response.
func twoCaptchaFakeServer(t *testing.T, resultsBeforeReady int, readyBody string) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/createTask":
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":42}`))
		case "/getTaskResult":
			if polls < resultsBeforeReady {
				polls++
				_, _ = w.Write([]byte(`{"errorId":0,"status":"processing"}`))
				return
			}
			_, _ = w.Write([]byte(readyBody))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	return srv, &paths
}

func TestTwoCaptchaSolverSolvesImageAfterPolling(t *testing.T) {
	withFastPolling(t)
	srv, paths := twoCaptchaFakeServer(t, 2, `{"errorId":0,"status":"ready","solution":{"text":"hello world"}}`)
	defer srv.Close()

	s := NewTwoCaptchaSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), KindImage, "data:image/png;base64,aGVsbG8=", "type what you see")
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != "hello world" {
		t.Errorf("Solve() = %q, want the solved text", got)
	}
	if len(*paths) < 3 || (*paths)[0] != "/createTask" {
		t.Errorf("paths = %v, want createTask then at least two getTaskResult polls", *paths)
	}
}

func TestTwoCaptchaSolverSolvesClick(t *testing.T) {
	withFastPolling(t)
	srv, _ := twoCaptchaFakeServer(t, 0, `{"errorId":0,"status":"ready","solution":{"coordinates":[{"x":358,"y":268}]}}`)
	defer srv.Close()

	s := NewTwoCaptchaSolver("key")
	s.base = srv.URL
	got, err := s.Solve(context.Background(), KindClick, "aGVsbG8=", "")
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != `{"x":358,"y":268}` {
		t.Errorf("Solve() = %s, want the single-point ClickedPoint JSON", got)
	}
}

func TestTwoCaptchaSolverCreateTaskErrorStopsBeforePolling(t *testing.T) {
	withFastPolling(t)
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		_, _ = w.Write([]byte(`{"errorId":10,"errorCode":"ERROR_ZERO_BALANCE","errorDescription":"no funds"}`))
	}))
	defer srv.Close()

	s := NewTwoCaptchaSolver("key")
	s.base = srv.URL
	_, err := s.Solve(context.Background(), KindImage, "aGVsbG8=", "")
	if err == nil {
		t.Fatal("Solve() with a createTask error = nil error, want one")
	}
	if len(gotPaths) != 1 {
		t.Errorf("server saw %v, want exactly one createTask call and no polling", gotPaths)
	}
}

func TestTwoCaptchaSolverUnsupportedKindNeverCallsTheNetwork(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"errorId":0,"taskId":1}`))
	}))
	defer srv.Close()

	s := NewTwoCaptchaSolver("key")
	s.base = srv.URL
	if _, err := s.Solve(context.Background(), KindWidget, "aGVsbG8=", ""); err != ErrUnsupportedKind {
		t.Errorf("Solve(KindWidget) error = %v, want ErrUnsupportedKind", err)
	}
	if called {
		t.Error("Solve(KindWidget) reached the network; it must refuse before ever dialing out")
	}
}

func TestTwoCaptchaSolverNoKeyConfigured(t *testing.T) {
	s := NewTwoCaptchaSolver("")
	if _, err := s.Solve(context.Background(), KindImage, "aGVsbG8=", ""); err == nil {
		t.Error("Solve() with no API key = nil error, want one")
	}
}

// TestTwoCaptchaSolverSendsBodyAndComment pins the exact request shape
// against api-docs/normal-captcha's documented fields.
func TestTwoCaptchaSolverSendsBodyAndComment(t *testing.T) {
	withFastPolling(t)
	var gotReq twoCaptchaCreateReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createTask" {
			_ = json.NewDecoder(r.Body).Decode(&gotReq)
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"text":"ok"}}`))
	}))
	defer srv.Close()

	s := NewTwoCaptchaSolver("the-key")
	s.base = srv.URL
	if _, err := s.Solve(context.Background(), KindImage, "aGVsbG8=", "enter red text"); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if gotReq.ClientKey != "the-key" || gotReq.Task.Type != "ImageToTextTask" ||
		gotReq.Task.Body != "aGVsbG8=" || gotReq.Task.Comment != "enter red text" {
		t.Errorf("createTask request = %+v, want clientKey/type/body/comment set from Solve's arguments", gotReq)
	}
}
