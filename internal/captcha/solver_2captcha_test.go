package captcha

// The shared helpers (decodeSolverImage, encodeClickAnswer, solverAnswerFor)
// beside where they are defined, and TwoCaptchaSolver against an
// httptest.Server pinning 2Captcha's request and response shapes.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// withFastPolling shortens solverPollInterval and solverMaxWait for the life of
// one test.
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

// The two shapes JD parses a click answer as.
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
		t.Errorf("encodeClickAnswer(two points) = %s, want the MultiClickedPoint shape", many)
	}

	if _, err := encodeClickAnswer(nil); err == nil {
		t.Error("encodeClickAnswer(nil) = nil error, want one")
	}
}

// A solution the provider marked ready but left empty has been paid for, so it
// must not read as a refusal the next solver could pick up.
func TestAnEmptySolutionCountsAsATakenTask(t *testing.T) {
	for _, kind := range []Kind{KindImage, KindClick, KindWidget} {
		_, err := solverAnswerFor(kind, solverResult{})
		if !errors.Is(err, ErrTaskTaken) {
			t.Errorf("solverAnswerFor(%s, empty) error = %v, want ErrTaskTaken", kind, err)
		}
	}
	var r *Refusal
	if _, err := solverAnswerFor(KindUnsupported, solverResult{text: "x"}); !errors.As(err, &r) || r.Code != RefusalUnsupported {
		t.Errorf("solverAnswerFor(KindUnsupported) error = %v, want a RefusalUnsupported", err)
	}
}

func imageChallenge(kind Kind, image, prompt string) Challenge {
	return Challenge{ID: "1", Host: "host.example", Kind: kind, Prompt: prompt, Payload: &ImagePayload{DataURL: image}}
}

// twoCaptchaFakeServer answers createTask and getTaskResult from fixed bodies,
// reporting "processing" resultsBeforeReady times so the poll loop runs.
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
			t.Errorf("unexpected path %s", r.URL.Path)
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
	got, err := s.Solve(context.Background(), imageChallenge(KindImage, "data:image/png;base64,aGVsbG8=", "type what you see"))
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
	got, err := s.Solve(context.Background(), imageChallenge(KindClick, "aGVsbG8=", ""))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if got != `{"x":358,"y":268}` {
		t.Errorf("Solve() = %s, want the single-point ClickedPoint JSON", got)
	}
}

// A refusal carries 2Captcha's own code and description, which is what the
// prompt shows, and stops before any polling.
func TestTwoCaptchaSolverReportsTheRefusalReason(t *testing.T) {
	withFastPolling(t)
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		_, _ = w.Write([]byte(`{"errorId":10,"errorCode":"ERROR_ZERO_BALANCE","errorDescription":"no funds"}`))
	}))
	defer srv.Close()

	s := NewTwoCaptchaSolver("key")
	s.base = srv.URL
	_, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("Solve() with a createTask error = %v, want a *Refusal", err)
	}
	if r.Code != "ERROR_ZERO_BALANCE" || r.Detail != "no funds" {
		t.Errorf("refusal = %+v, want 2Captcha's code and description", r)
	}
	if len(gotPaths) != 1 {
		t.Errorf("server saw %v, want exactly one createTask call and no polling", gotPaths)
	}
}

// Once 2Captcha holds the task, losing the connection is not a refusal: the
// task may still be solved and billed.
func TestTwoCaptchaSolverLostConnectionAfterCreateTaskIsATakenTask(t *testing.T) {
	withFastPolling(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createTask" {
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":7}`))
			return
		}
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	s := NewTwoCaptchaSolver("key")
	s.base = srv.URL
	_, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
	if !errors.Is(err, ErrTaskTaken) {
		t.Fatalf("Solve() = %v, want ErrTaskTaken", err)
	}
}

// A provider that accepted the task and then gave up on it may bill it:
// Anti-Captcha charges for ERROR_CAPTCHA_UNSOLVABLE. So the verdict counts as
// a taken task, and keeps the provider's code for the prompt.
func TestAProviderGivingUpOnATaskItTookCountsAsTaken(t *testing.T) {
	for _, p := range tokenProviders {
		t.Run(p.name, func(t *testing.T) {
			withFastPolling(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/createTask" {
					_, _ = w.Write([]byte(`{"errorId":0,"taskId":1}`))
					return
				}
				_, _ = w.Write([]byte(`{"errorId":12,"errorCode":"ERROR_CAPTCHA_UNSOLVABLE","errorDescription":"workers could not solve it"}`))
			}))
			defer srv.Close()

			_, err := p.build(srv.URL).Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
			if !errors.Is(err, ErrTaskTaken) {
				t.Fatalf("Solve() = %v, want ErrTaskTaken", err)
			}
			var r *Refusal
			if !errors.As(err, &r) || r.Code != "ERROR_CAPTCHA_UNSOLVABLE" {
				t.Errorf("Solve() = %v, want the provider's code kept", err)
			}
		})
	}
}

// createTask went out and its answer was lost, so the provider may have
// created the task all the same.
func TestALostCreateTaskAnswerCountsAsTaken(t *testing.T) {
	for _, p := range tokenProviders {
		t.Run(p.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = conn.Close()
				}
			}))
			defer srv.Close()

			_, err := p.build(srv.URL).Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
			if !errors.Is(err, ErrTaskTaken) {
				t.Errorf("Solve() = %v, want ErrTaskTaken", err)
			}
		})
	}
}

// A request that never left this machine created nothing, so another solver
// may have the captcha.
func TestAnUnreachableProviderHasNotTakenTheTask(t *testing.T) {
	refused := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}}
	for _, p := range tokenProviders {
		t.Run(p.name, func(t *testing.T) {
			s := p.build("http://provider.test")
			switch s := s.(type) {
			case *TwoCaptchaSolver:
				s.hc = refused
			case *AntiCaptchaSolver:
				s.hc = refused
			}
			_, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", ""))
			if err == nil || errors.Is(err, ErrTaskTaken) {
				t.Fatalf("Solve() = %v, want an error that is not ErrTaskTaken", err)
			}
			if got := RefusalFor(p.name, err); got.Code != RefusalFailed || got.Taken {
				t.Errorf("RefusalFor = %+v, want failed and not taken", got)
			}
		})
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
	c := Challenge{ID: "1", Kind: KindUnsupported, Payload: &UnsupportedPayload{Vendor: "AccountLoginOAuthChallenge"}}
	var r *Refusal
	if _, err := s.Solve(context.Background(), c); !errors.As(err, &r) || r.Code != RefusalUnsupported {
		t.Errorf("Solve(KindUnsupported) error = %v, want a RefusalUnsupported", err)
	}
	if called {
		t.Error("Solve(KindUnsupported) reached the network instead of refusing first")
	}
}

func TestTwoCaptchaSolverNoKeyConfigured(t *testing.T) {
	s := NewTwoCaptchaSolver("")
	if _, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", "")); err == nil {
		t.Error("Solve() with no API key = nil error, want one")
	}
}

// The createTask request carries the fields ImageToTextTask documents.
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
	if _, err := s.Solve(context.Background(), imageChallenge(KindImage, "aGVsbG8=", "enter red text")); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if gotReq.ClientKey != "the-key" || gotReq.Task.Type != "ImageToTextTask" ||
		gotReq.Task.Body != "aGVsbG8=" || gotReq.Task.Comment != "enter red text" {
		t.Errorf("createTask request = %+v, want the fields set from the challenge", gotReq)
	}
}
