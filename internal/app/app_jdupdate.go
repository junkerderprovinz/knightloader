package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
	log.Printf("JD restarts to install its update")
	return nil
}

// restartJDForUpdate is StartJDUpdate without the log line, which the daily
// update writes in words of its own.
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
}

// The daily JD update waits after start for the reason the yt-dlp one does.
// An update held back by a running JD download is tried again within the
// hour rather than the next day.
const (
	jdAutoFirstRun = 10 * time.Minute
	jdAutoEvery    = 24 * time.Hour
	jdAutoRetry    = time.Hour
)

// StartJDAutoUpdate starts the daily JDownloader update. The binaries call it
// rather than New, which every test calls. Close stops it.
func (a *App) StartJDAutoUpdate() {
	a.spawn(func() { a.jdAutoLoop(jdAutoFirstRun, jdAutoEvery, jdAutoRetry) })
}

// jdAutoLoop reads the switch on every run, so turning it on or off takes
// effect without a restart.
func (a *App) jdAutoLoop(first, every, retry time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		if a.autoUpdateJD() {
			timer.Reset(retry)
		} else {
			timer.Reset(every)
		}
	}
}

// autoUpdateJD is one daily run. It reports true when JD has an update that
// waits because JD is downloading: JD restarts to install it, which would
// break off every download it carries.
func (a *App) autoUpdateJD() (held bool) {
	if !a.Settings.Get().JDAutoUpdate {
		return false
	}
	available, err := a.checkJD(a.ctx)
	switch {
	case a.ctx.Err() != nil, errors.Is(err, ErrJDUnreachable):
		// Without a JD there is nothing to update, and one that does not
		// answer is the health page's to report.
		return false
	case err != nil:
		log.Printf("JD: the daily update could not ask JD for an update: %v", err)
		return false
	case !available:
		return false
	case a.jdDownloading():
		return true
	}
	if err := a.restartJD(); err != nil {
		log.Printf("JD: the daily update found an update, but JD did not take the request to install it: %v", err)
		return false
	}
	log.Print("JD: the daily update found an update, and JD restarts to install it")
	return false
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
