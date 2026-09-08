package main

// Putting THIS MACHINE to sleep, which is the desktop build's half of the
// end-of-queue action set (internal/idleaction.ActionSuspend).
//
// It lives here, in the desktop module, for the same reason DesktopFiles does
// one file over: a browser cannot do this and neither can the container
// build. internal/app.App owns no power state of its own - exactly as it owns
// no *http.Server (RequestExit) and no process lifecycle
// (RequestUpdateInstall) - so the server side of this is a nil function field
// that only this module fills in, and every other deployment reads that nil
// as "not supported here" and never offers the action at all.
//
// WHY THE CONTAINER DOES NOT GET THIS, ever, whatever it is asked for: its
// process is PID 1 in its own namespace, and the machine underneath it is not
// something anything inside the container can reach. Wiring a power call to a
// container's PID 1 is the bug internal/idleaction spent two waves refusing
// to ship, and it is still refused - the honest container answer is
// ActionCommand pointed at something that reaches the host.
//
// suspend is implemented once per OS in power_windows.go, power_darwin.go and
// power_linux.go - the three platforms desktop.yml actually builds - with
// power_other.go covering anything else so this module still compiles there,
// the same arrangement tray_probe_linux.go and tray_probe_other.go already
// use.

import "fmt"

// requestSuspend is what main.go assigns to app.App.RequestSuspend.
//
// It is the one seam, and it deliberately does not rewrite what the operating
// system said. On Linux a refusal from the policy manager reads "Interactive
// authentication required", and that single sentence is the only thing that
// tells the operator what to fix; a tidier message of our own would throw the
// one useful string away and leave "sleeping failed" behind. So the prefix is
// added and the original is carried through underneath it, all the way to the
// last-run report on the settings page (internal/app.IdleRun.Output).
func requestSuspend() error {
	if err := suspend(); err != nil {
		return fmt.Errorf("could not put this machine to sleep: %w", err)
	}
	return nil
}
