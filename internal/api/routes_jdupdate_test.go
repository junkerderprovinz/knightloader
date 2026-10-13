package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeJD answers /help and the update namespace the way a live JD does.
type fakeJD struct {
	available bool
	checkFail bool
	checks    atomic.Int32
	restarts  atomic.Int32
}

func (f *fakeJD) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/help":
		_, _ = w.Write([]byte("<html></html>"))
	case "/update/runUpdateCheck":
		f.checks.Add(1)
		if f.checkFail {
			http.Error(w, "update server unreachable", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"data":""}`))
	case "/update/isUpdateAvailable":
		if f.available {
			_, _ = w.Write([]byte(`{"data":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":false}`))
	case "/update/restartAndUpdate":
		f.restarts.Add(1)
		_, _ = w.Write([]byte(`{"data":""}`))
	default:
		http.NotFound(w, r)
	}
}

// jdUpdateServer serves the JD update routes against the JD at jdURL, which
// is set only after the app is up so app.New does not wire a backend to it.
func jdUpdateServer(t *testing.T, jdURL string) *httptest.Server {
	t.Helper()
	t.Setenv("KL_JD", "")
	a := testApp(t)
	t.Setenv("KL_JD", jdURL)
	reg := newRegistry()
	registerResolvers(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func callJDUpdate(t *testing.T, srv *httptest.Server, method string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+"/api/jd/update", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s /api/jd/update answered no JSON: %v", method, err)
	}
	return resp.StatusCode, body
}

func TestJDUpdateCheckReportsAnUpdate(t *testing.T) {
	jd := &fakeJD{available: true}
	fake := httptest.NewServer(jd)
	t.Cleanup(fake.Close)
	srv := jdUpdateServer(t, fake.URL)

	status, body := callJDUpdate(t, srv, http.MethodGet)
	if status != http.StatusOK || body["available"] != true || body["checked"] != true {
		t.Errorf("GET = %d %v, want 200 with available and checked true", status, body)
	}
	if jd.checks.Load() != 1 {
		t.Errorf("JD ran %d update checks, want 1", jd.checks.Load())
	}
}

func TestJDUpdateIsRefusedWithoutAJD(t *testing.T) {
	srv := jdUpdateServer(t, "")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		status, body := callJDUpdate(t, srv, method)
		if status != http.StatusConflict || body["code"] != "jdUnreachable" {
			t.Errorf("%s = %d %v, want 409 jdUnreachable", method, status, body)
		}
	}
}

func TestJDUpdateIsRefusedWhenJDDoesNotAnswer(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	srv := jdUpdateServer(t, gone.URL)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		status, body := callJDUpdate(t, srv, method)
		if status != http.StatusConflict || body["code"] != "jdUnreachable" {
			t.Errorf("%s = %d %v, want 409 jdUnreachable", method, status, body)
		}
	}
}

func TestJDUpdateCheckThatJDFailsIsABadGateway(t *testing.T) {
	fake := httptest.NewServer(&fakeJD{checkFail: true})
	t.Cleanup(fake.Close)
	srv := jdUpdateServer(t, fake.URL)

	status, body := callJDUpdate(t, srv, http.MethodGet)
	if status != http.StatusBadGateway || body["code"] != "jdUpdateCheckFailed" {
		t.Errorf("GET = %d %v, want 502 jdUpdateCheckFailed", status, body)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Error("the refusal carries no error text")
	}
}

func TestJDUpdateStartsTheRestart(t *testing.T) {
	jd := &fakeJD{available: true}
	fake := httptest.NewServer(jd)
	t.Cleanup(fake.Close)
	srv := jdUpdateServer(t, fake.URL)

	status, body := callJDUpdate(t, srv, http.MethodPost)
	if status != http.StatusAccepted || body["started"] != true {
		t.Errorf("POST = %d %v, want 202 with started true", status, body)
	}
	if jd.restarts.Load() != 1 {
		t.Errorf("JD was asked to restart %d times, want 1", jd.restarts.Load())
	}
}
