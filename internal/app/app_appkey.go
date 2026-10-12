package app

import (
	"fmt"
	"log"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// keyCopiesSweep is how often the copies made before the first sealing are
// checked for removal while the instance runs.
const keyCopiesSweep = time.Hour

// keepCopiesBeforeSealing copies the files a first start with an app key is
// about to rewrite sealed. An auth.json still in the clear is what marks that
// first start; a settings.json in the clear on an install already sealed came
// from a restored backup, whose secrets the owner holds anyway.
func keepCopiesBeforeSealing(dataDir string, key *appkey.Key) error {
	if key == nil || !auth.NeedsSealing(dataDir) {
		return nil
	}
	dir, err := appkey.KeepCopies(dataDir, []string{auth.FileName, accounts.KeyringName, settings.FileName}, time.Now())
	if err != nil {
		return fmt.Errorf("app key: %w", err)
	}
	if dir != "" {
		log.Printf("app key: sealing the secrets in %s for the first time. The files as they were are kept in %s "+
			"and removed after %s without a sealing error", dataDir, dir, appkey.CopiesKept)
	}
	return nil
}

// noteKeyFailures keeps the copies for longer whenever a value of this
// install does not seal or open, including a start that fails on one.
func noteKeyFailures(dataDir string, key *appkey.Key) {
	if key == nil {
		return
	}
	key.OnFailure(func(err error) {
		if nerr := appkey.NoteFailure(dataDir, time.Now()); nerr != nil {
			log.Printf("app key: %v, and the copies from before the key could not be told: %v", err, nerr)
		}
	})
}

// sweepKeyCopies removes the copies once the key has worked for
// appkey.CopiesKept, at start and then on keyCopiesSweep until Close.
func (a *App) sweepKeyCopies() {
	t := time.NewTicker(keyCopiesSweep)
	defer t.Stop()
	for {
		removed, err := appkey.SweepCopies(a.DataDir, time.Now())
		for _, dir := range removed {
			log.Printf("app key: removed %s, the copies of the files from before the key", dir)
		}
		if err != nil {
			log.Printf("app key: the copies from before the key could not be removed: %v", err)
		}
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// KeyCopiesUntil reports when the copies from before the app key go, and
// false when there are none.
func (a *App) KeyCopiesUntil() (time.Time, bool) {
	if a.AppKey == nil {
		return time.Time{}, false
	}
	return appkey.CopiesUntil(a.DataDir, time.Now())
}
