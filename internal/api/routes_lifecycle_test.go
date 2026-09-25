package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// lifecycleServer is the lifecycle routes on a throwaway app. It restores the
// package variable buildinfo.Deployment when the test ends.
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

func TestDeploymentReportsContainerByDefault(t *testing.T) {
	_, srv := lifecycleServer(t)
	buildinfo.Deployment = "container"

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

func TestDeploymentReflectsDesktop(t *testing.T) {
	_, srv := lifecycleServer(t)
	buildinfo.Deployment = "desktop"

	var info DeploymentInfo
	if code := getJSON(t, srv.URL+"/api/system/deployment", &info); code != http.StatusOK {
		t.Fatalf("GET deployment: %d", code)
	}
	if info.Deployment != "desktop" {
		t.Errorf("deployment = %q, want desktop", info.Deployment)
	}
}

// TestQuitAndRestartAreNotImplementedWithNoRequestExit checks that an
// embedding without RequestExit answers 501 instead of a silent 200.
func TestQuitAndRestartAreNotImplementedWithNoRequestExit(t *testing.T) {
	_, srv := lifecycleServer(t)
	for _, path := range []string{"/api/system/quit", "/api/system/restart"} {
		code, _ := postJSON(t, http.MethodPost, srv.URL+path, nil)
		if code != http.StatusNotImplemented {
			t.Errorf("POST %s = %d, want %d", path, code, http.StatusNotImplemented)
		}
	}
}

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

// TestQuitReportsConflictWhenAlreadyShuttingDown checks that a false from
// RequestExit, meaning a shutdown is already pending, answers 409.
func TestQuitReportsConflictWhenAlreadyShuttingDown(t *testing.T) {
	a, srv := lifecycleServer(t)
	a.RequestExit = func(restart bool) bool { return false }

	code, _ := postJSON(t, http.MethodPost, srv.URL+"/api/system/quit", nil)
	if code != http.StatusConflict {
		t.Fatalf("POST quit = %d, want %d", code, http.StatusConflict)
	}
}

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
