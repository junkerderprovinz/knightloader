package api

// Quitting and restarting the process, and telling the interface which of
// the two labels actually fits the build it is talking to.
//
// App owns no *http.Server and no signal loop of its own — only the state
// Close already knows how to drain — so both action routes below do nothing
// but ask whatever embeds this App to run that drain and then exit. See
// app.App.RequestExit's own doc comment for why that is a field on App
// rather than a second thing threaded through Handler, and
// cmd/knightloader/main.go for the signal loop that answers it.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// DeploymentInfo is what the interface needs to label a quit/restart control
// honestly instead of offering the same two buttons everywhere.
type DeploymentInfo struct {
	// Deployment is "container" (cmd/knightloader, the default) or
	// "desktop" (the Wails build) — see buildinfo.Deployment.
	Deployment string `json:"deployment"`
	// CanQuit and CanRestart report whether this process actually has a way
	// to act on the two routes below at all — false wherever RequestExit was
	// never wired. Both read the same field today because the mechanism is
	// identical either way (see RequestExit's own doc comment); they are two
	// fields rather than one so a build that ever wants to offer only one of
	// the two can say so without changing the response shape.
	CanQuit    bool `json:"canQuit"`
	CanRestart bool `json:"canRestart"`
	// Note is plain English from the server about what quit and restart
	// actually do in THIS build — the same "a fact about this build, not a
	// translation key" convention Feature.Reason already uses (routes_
	// features.go), for the same reason: what matters here (does something
	// outside this process bring it back) is true or false about the
	// deployment, not about the button, and 38 locale files should not have
	// to gain a key every time that sentence is reworded.
	Note string `json:"note"`
}

func registerLifecycle(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/system/deployment",
		"which build this is, and what quitting or restarting it actually does here",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, deploymentInfo(a))
		})

	reg.Add(http.MethodPost, "/api/system/quit",
		"drain in-flight work and exit; whether the process comes back is decided entirely by whatever runs it",
		func(w http.ResponseWriter, r *http.Request) {
			requestExit(w, a, false)
		})

	reg.Add(http.MethodPost, "/api/system/restart",
		"the identical action as quit, under the name that fits a supervised deployment",
		func(w http.ResponseWriter, r *http.Request) {
			requestExit(w, a, true)
		})
}

func deploymentInfo(a *app.App) DeploymentInfo {
	info := DeploymentInfo{
		Deployment: buildinfo.Deployment,
		CanQuit:    a.RequestExit != nil,
		CanRestart: a.RequestExit != nil,
	}
	if buildinfo.Deployment == "desktop" {
		info.Note = "this is the desktop build; quit and restart drain the same way closing the window does"
	} else {
		info.Note = "this is a container build; whether the process comes back after Quit or Restart is decided " +
			"entirely by your container's own restart policy, not by which of the two you press; both do the same thing"
	}
	return info
}

// requestExit is quit and restart's shared body: they differ only in the
// bool RequestExit is called with, which the caller (cmd/knightloader's
// signal loop) reads only for its own log line — see RequestExit's own doc
// comment for why the drain-and-exit sequence itself is not allowed to
// differ.
func requestExit(w http.ResponseWriter, a *app.App, restart bool) {
	if a.RequestExit == nil {
		http.Error(w, "this build has no way to stop the process from an API call", http.StatusNotImplemented)
		return
	}
	if !a.RequestExit(restart) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"status": "a shutdown is already in progress"})
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "shutting down"})
}
