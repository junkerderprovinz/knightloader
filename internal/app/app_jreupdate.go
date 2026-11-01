package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/jre"
)

// javaUpdateState is embedded on App so its fields stay in this file.
type javaUpdateState struct {
	// latestJava and stageJava are jre.Latest against Adoptium and jre.Stage,
	// which the tests replace.
	latestJava func(ctx context.Context) (*jre.Release, error)
	stageJava  func(ctx context.Context, rel *jre.Release) error

	javaMu sync.Mutex
	// javaWaits is the newer Java the weekly question found, until JD runs on
	// it. JDStatus passes it on to the Components card.
	javaWaits string
	// javaRefused is a Java that did not bring JD up, which this run of the
	// program does not fetch again.
	javaRefused string
}

// ErrJavaRefused is what SwitchJava wraps when JD did not start on the new
// runtime and runs on the old one again.
var ErrJavaRefused = errors.New("JD did not start on the new Java runtime")

// The weekly Java question starts later than the JD one, so the two never
// restart JD at the same moment, and a switch held back by a running JD
// download, or by a JD setup under way, is tried again within the hour.
const (
	javaAutoFirstRun = 20 * time.Minute
	javaAutoEvery    = 7 * 24 * time.Hour
	javaAutoRetry    = time.Hour
)

// StartJavaAutoUpdate starts the weekly question for a newer Java runtime for
// the desktop build's own JD. The desktop build calls it once it has set
// SwitchJava. Close stops it.
func (a *App) StartJavaAutoUpdate() {
	a.spawn(func() { a.javaAutoLoop(javaAutoFirstRun, javaAutoEvery, javaAutoRetry) })
}

// javaAutoLoop reads the switches on every run, as jdAutoLoop does.
func (a *App) javaAutoLoop(first, every, retry time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		if a.autoUpdateJava() {
			timer.Reset(retry)
		} else {
			timer.Reset(every)
		}
	}
}

// autoUpdateJava is one weekly run. It shares JDownloader's two switches:
// either one asks Adoptium for a newer Temurin 21, and JDAutoUpdate also
// fetches it and moves JD onto it. It reports true when the move has to wait,
// for a JD download, which the restart would break off, or for a JD setup
// under way.
func (a *App) autoUpdateJava() (held bool) {
	s := a.Settings.Get()
	if !s.JDVersionCheck && !s.JDAutoUpdate || a.SwitchJava == nil {
		return false
	}
	cur, err := jre.Load(a.DataDir)
	if err != nil || cur == nil {
		// Without a runtime of its own JD is still being set up, which
		// fetches the pinned one.
		return false
	}
	next, err := jre.Pending(a.DataDir)
	if err != nil {
		log.Printf("Java: the weekly update could not read the runtime it fetched: %v", err)
	}
	if next == nil {
		rel, err := a.latestJava(a.ctx)
		if err != nil {
			if a.ctx.Err() == nil {
				log.Printf("Java: the weekly update could not ask Adoptium for a newer runtime: %v", err)
			}
			return false
		}
		if jre.Compare(rel.Version, cur.Version) <= 0 {
			a.setJavaWaits("")
			return false
		}
		a.setJavaWaits(rel.Version)
		if !s.JDAutoUpdate || rel.Version == a.refusedJava() {
			return false
		}
		if err := a.stageJava(a.ctx, rel); err != nil {
			if a.ctx.Err() == nil {
				log.Printf("Java: the weekly update could not fetch Java %s: %v", rel.Version, err)
			}
			return false
		}
		log.Printf("Java: the weekly update fetched Java %s, and JD moves onto it once none of its downloads runs", rel.Version)
		next = &jre.Record{Version: rel.Version}
	}
	a.setJavaWaits(next.Version)
	if !s.JDAutoUpdate || next.Version == a.refusedJava() {
		return false
	}
	if a.jdDownloading() {
		return true
	}
	err = a.SwitchJava(a.ctx)
	switch {
	case err == nil:
	case a.ctx.Err() != nil:
		return false
	case errors.Is(err, ErrJavaRefused):
		log.Printf("Java: JD stays on Java %s: %v", cur.Version, err)
		a.javaMu.Lock()
		a.javaRefused = next.Version
		a.javaMu.Unlock()
		return false
	default:
		log.Printf("Java: the weekly update could not move JD onto Java %s yet: %v", next.Version, err)
		return true
	}
	a.setJavaWaits("")
	return false
}

func (a *App) setJavaWaits(version string) {
	a.javaMu.Lock()
	a.javaWaits = version
	a.javaMu.Unlock()
}

func (a *App) javaUpdateWaits() string {
	a.javaMu.Lock()
	defer a.javaMu.Unlock()
	return a.javaWaits
}

func (a *App) refusedJava() string {
	a.javaMu.Lock()
	defer a.javaMu.Unlock()
	return a.javaRefused
}
