package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

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
	c, err := reachableJD()
	if err != nil {
		return err
	}
	if err := c.RestartAndUpdate(); err != nil {
		return err
	}
	log.Printf("JD restarts to install its update")
	return nil
}
