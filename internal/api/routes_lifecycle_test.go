package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// lifecycleServer is the lifecycle routes on a throwaway app, with
// buildinfo.Deployment reset once the test ends — it is a shared package
// variable, and a test that leaves it changed would leak into whichever
// test in this package happens to run next.
func lifecycleServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	prev := buildinfo.Deployment
	t.Cleanup(func() { buildinfo.Deployment = prev })

	a := testApp(t)
	reg := newRegistry()
	registerLifecycle(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// TestDeploymentReportsContainerByDefault pins the zero-value case: nothing
// in cmd/knightloader ever sets buildinfo.Deployment, so an unset App —
// exactly like the one every test in this package builds — has to read as
// "container", not as some third, unlabelled state.
func TestDeploymentReportsContainerByDefault(t *testing.T) {
	buildinfo.Deployment = "container"
	_, srv := lifecycleServer(t)

	var info DeploymentInfo
	if code := getJSON(t, srv.URL+"/api/system/deployment", &info); code != http.StatusOK {
		t.Fatalf("GET deployment: %d", code)
	}
	if info.Deployment != "container" {
		t.Errorf("deployment = %q, want container", info.Deployment)
	}
	if info.CanQuit || info.CanRestart {
		t.Errorf("CanQuit=%v CanRestart=%v, want both false with RequestExit unset", info.CanQuit, info.CanRestart)
	}
	if info.Note == "" {
		t.Error("no note explaining what quit/restart do in this build")
	}
}

// TestDeploymentReflectsDesktop is the other label buildinfo.Deployment can
// carry, and the different note that comes with it — the two must not share
// one sentence, since the container caveat ("your restart policy decides")
// is actively wrong advice on a desktop build.
func TestDeploymentReflectsDesktop(t *testing.T) {
	buildinfo.Deployment = "desktop"
	_, srv := lifecycleServer(t)

	var info DeploymentInfo
	if code := getJSON(t, srv.URL+"/api/system/deployment", &info); code != http.StatusOK {
		t.Fatalf("GET deployment: %d", code)
	}
	if info.Deployment != "desktop" {
		t.Errorf("deployment = %q, want desktop", info.Deployment)
	}
}

// TestQuitAndRestartAreNotImplementedWithNoRequestExit is the desktop
// build's current honest state (see App.RequestExit's own doc comment) and
// any embedding that never wires it: the route must say so, not silently
// answer 200 and do nothing.
func TestQuitAndRestartAreNotImplementedWithNoRequestExit(t *testing.T) {
	_, srv := lifecycleServer(t)
	for _, path := range []string{"/api/system/quit", "/api/system/restart"} {
		code, _ := postJSON(t, http.MethodPost, srv.URL+path, nil)
		if code != http.StatusNotImplemented {
			t.Errorf("POST %s = %d, want %d", path, code, http.StatusNotImplemented)
		}
	}
}

// TestQuitCallsRequestExitWithFalse and TestRestartCallsRequestExitWithTrue
// are the one fact the whole feature rests on: the two routes must pass the
// right bool through, because that bool is the only thing distinguishing
// them once they reach main.go's signal loop.
func TestQuitCallsRequestExitWithFalse(t *testing.T) {
	a, srv := lifecycleServer(t)
	var got []bool
	a.RequestExit = func(restart bool) bool { got = append(got, restart); return true }

	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/system/quit", nil)
	if code != http.StatusAccepted {
		t.Fatalf("POST quit = %d, want 202", code)
	}
	if len(got) != 1 || got[0] != false {
		t.Errorf("RequestExit calls = %v, want exactly one call with false", got)
	}
}

func TestRestartCallsRequestExitWithTrue(t *testing.T) {
	a, srv := lifecycleServer(t)
	var got []bool
	a.RequestExit = func(restart bool) bool { got = append(got, restart); return true }

	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/system/restart", nil)
	if code != http.StatusAccepted {
		t.Fatalf("POST restart = %d, want 202", code)
	}
	if len(got) != 1 || got[0] != true {
		t.Errorf("RequestExit calls = %v, want exactly one call with true", got)
	}
}

// TestQuitReportsConflictWhenAlreadyShuttingDown is what RequestExit
// returning false means: a shutdown is already pending, and the caller
// (main.go's non-blocking channel send) has nothing more to do with a
// second request — the route has to say that rather than claim a fresh 202
// for a request that changed nothing.
func TestQuitReportsConflictWhenAlreadyShuttingDown(t *testing.T) {
	a, srv := lifecycleServer(t)
	a.RequestExit = func(restart bool) bool { return false }

	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/system/quit", nil)
	if code != http.StatusConflict {
		t.Fatalf("POST quit = %d, want %d", code, http.StatusConflict)
	}
}

// TestDeploymentCanQuitFollowsRequestExit confirms the GET route's own
// capability flags are read live off the same field the action routes
// check, rather than a separate answer that could disagree with them.
func TestDeploymentCanQuitFollowsRequestExit(t *testing.T) {
	a, srv := lifecycleServer(t)
	a.RequestExit = func(restart bool) bool { return true }

	var info DeploymentInfo
	if code := getJSON(t, srv.URL+"/api/system/deployment", &info); code != http.StatusOK {
		t.Fatalf("GET deployment: %d", code)
	}
	if !info.CanQuit || !info.CanRestart {
		t.Errorf("CanQuit=%v CanRestart=%v, want both true once RequestExit is set", info.CanQuit, info.CanRestart)
	}
}
