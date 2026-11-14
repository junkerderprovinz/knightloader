package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// ErrJDUnreachable is returned when no JDownloader is configured or the
// configured one does not answer.
var ErrJDUnreachable = errors.New("no JDownloader is configured, or it does not answer")

// jdUpdateWait is how long CheckJDUpdate waits for JD to report an update
// after starting its check, since JD cannot say when a check has finished.
var (
	jdUpdateWait = 15 * time.Second
	jdUpdatePoll = time.Second
)

// reachableJD returns a client for the JD in KL_JD once it has answered.
func reachableJD() (*jd.Client, error) {
	base := strings.TrimSpace(jdBaseEnv())
	if base == "" {
		return nil, fmt.Errorf("%w: KL_JD is not set", ErrJDUnreachable)
	}
	c := jd.NewClient(base)
	if err := c.Ping(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJDUnreachable, err)
	}
	return c, nil
}

// CheckJDUpdate has JDownloader look for an update and reports whether one is
// ready to install. An error wraps ErrJDUnreachable when JD did not answer at
// all.
func (a *App) CheckJDUpdate(ctx context.Context) (bool, error) {
	c, err := reachableJD()
	if err != nil {
		return false, err
	}
	if err := c.RunUpdateCheck(); err != nil {
		return false, err
	}
	deadline := time.Now().Add(jdUpdateWait)
	for {
		ok, err := c.UpdateAvailable()
		if err != nil || ok {
			return ok, err
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(jdUpdatePoll):
		}
	}
}

// StartJDUpdate asks JDownloader to restart and install its update. It
// returns once JD has taken the request; JD is gone for a while after that,
// and JDStatus shows the new revision once it is back.
func (a *App) StartJDUpdate() error {
	if err := restartJDForUpdate(); err != nil {
		return err
	}
	a.jdUpdateWaits.Store(false)
	log.Printf("JD restarts to install its update")
	return nil
}

// restartJDForUpdate is StartJDUpdate without the log line, which the
// automatic update writes in words of its own.
func restartJDForUpdate() error {
	c, err := reachableJD()
	if err != nil {
		return err
	}
	return c.RestartAndUpdate()
}

// jdUpdateState is embedded on App so its fields stay in this file.
type jdUpdateState struct {
	// checkJD and restartJD are CheckJDUpdate and restartJDForUpdate, which
	// the tests replace so that no JDownloader is needed.
	checkJD   func(ctx context.Context) (bool, error)
	restartJD func() error
	// jdUpdateWaits is what the last automatic question found, until JD
	// restarts to install it. JDStatus passes it on to the Components card.
	jdUpdateWaits atomic.Bool
}

// The JD question waits after start for the reason the yt-dlp update does.
const jdAutoFirstRun = 10 * time.Minute

// StartJDAutoUpdate starts the automatic JDownloader question and update. The
// binaries call it rather than New, which every test calls. Close stops it.
func (a *App) StartJDAutoUpdate() {
	a.spawn(func() { a.tickLoop(jdAutoFirstRun, componentTick, a.autoUpdateJD) })
}

// autoUpdateJD asks after JD once it is due.
func (a *App) autoUpdateJD() {
	if a.componentDue(componentJD, false) {
		a.asked(componentJD, a.updateJDComponent(a.ctx))
	}
}

// updateJDComponent asks JD whether an update waits and has JD install it.
// While one of JD's downloads runs the update waits, since JD restarts to
// install it, which would break off every download it carries.
func (a *App) updateJDComponent(ctx context.Context) ComponentResult {
	available, err := a.checkJD(ctx)
	if err == nil {
		a.jdUpdateWaits.Store(available)
	}
	switch {
	case ctx.Err() != nil:
		return ComponentResult{State: ComponentFailed, Detail: ctx.Err().Error()}
	case errors.Is(err, ErrJDUnreachable):
		// Without a JD there is nothing to update, and one that does not
		// answer is the health page's to report.
		return ComponentResult{State: ComponentSkipped, Detail: err.Error(), again: true}
	case err != nil:
		log.Printf("JD: the update could not ask JD for an update: %v", err)
		return ComponentResult{State: ComponentFailed, Detail: err.Error()}
	case !available:
		return ComponentResult{State: ComponentCurrent}
	case a.jdDownloading():
		return ComponentResult{State: ComponentWaits, again: true}
	}
	if err := a.restartJD(); err != nil {
		log.Printf("JD: the update found an update, but JD did not take the request to install it: %v", err)
		return ComponentResult{State: ComponentFailed, Detail: err.Error()}
	}
	a.jdUpdateWaits.Store(false)
	log.Print("JD: the update found an update, and JD restarts to install it")
	return ComponentResult{State: ComponentUpdated}
}

// jdDownloading reports whether a task JD carries is running.
func (a *App) jdDownloading() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		if t.Resolver == "jd" && t.Status == core.StatusRunning {
			return true
		}
	}
	return false
}
