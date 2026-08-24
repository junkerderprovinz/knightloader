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
	"github.com/junkerderprovinz/knightloader/internal/update"
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

	// Both deployments call this now (see update.Check's own package doc for
	// why what differs by deployment is only what "update available" tells
	// you to do, never whether the check runs) - registered unconditionally
	// like every other route here regardless, so the frontend gates the UI
	// on GET /api/system/deployment instead of this route refusing to exist
	// on the wrong build.
	reg.Add(http.MethodGet, "/api/system/update-check",
		"whether a newer release exists on GitHub than this build's own version",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, update.Check(r.Context(), buildinfo.Version))
		})

	// Desktop only - App.RequestUpdateInstall is nil on the container build
	// (registered unconditionally like every route here regardless, same
	// convention as update-check just above: the frontend gates the UI on
	// GET /api/system/deployment rather than this route refusing to exist).
	// Long-running (download + swap can take a while over a slow link), so
	// this blocks the request until it either succeeds - in which case the
	// process is about to exit and relaunch, and the response race with
	// that exit is expected and harmless - or fails with a reason the UI
	// can show.
	reg.Add(http.MethodPost, "/api/system/update-install",
		"download and apply the latest release, then relaunch - desktop only",
		func(w http.ResponseWriter, r *http.Request) {
			if a.RequestUpdateInstall == nil {
				http.Error(w, "this build cannot install updates from here", http.StatusNotImplemented)
				return
			}
			if err := a.RequestUpdateInstall(r.Context()); err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "installing"})
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
