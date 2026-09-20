package api

// Quitting and restarting the process, and telling the interface which label
// fits the build it is talking to. App owns no server or signal loop, so both
// action routes ask whatever embeds it to drain and exit (app.App.RequestExit).

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/update"
)

// DeploymentInfo is what the interface needs to label a quit or restart
// control correctly for this build.
type DeploymentInfo struct {
	// Deployment is "container" or "desktop"; see buildinfo.Deployment.
	Deployment string `json:"deployment"`
	// CanQuit and CanRestart report whether RequestExit is wired at all. They
	// are separate fields so a build could offer only one without changing the
	// response shape.
	CanQuit    bool `json:"canQuit"`
	CanRestart bool `json:"canRestart"`
	// Note is untranslated English about what quit and restart do in this
	// build, like Feature.Reason.
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

	reg.Add(http.MethodGet, "/api/system/update-check",
		"whether a newer release exists on GitHub than this build's own version",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, update.Check(r.Context(), buildinfo.Version))
		})

	// Desktop only: RequestUpdateInstall is nil on the container build. The
	// request blocks until the install fails or the process is about to
	// relaunch, in which case losing the response is expected.
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

// requestExit is the shared body of quit and restart. The flag only reaches
// the caller's log line; the drain-and-exit sequence is the same.
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
