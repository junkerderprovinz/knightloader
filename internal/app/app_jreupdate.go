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
	// javaWaits is the newer Java the last question found, until JD runs on
	// it. JDStatus passes it on to the Components card.
	javaWaits string
	// javaRefused is a Java that did not bring JD up, which this run of the
	// program does not fetch again.
	javaRefused string
}

// ErrJavaRefused is what SwitchJava wraps when JD did not start on the new
// runtime and runs on the old one again.
var ErrJavaRefused = errors.New("JD did not start on the new Java runtime")

// The Java question starts later than the JD one, so the two never restart
// JD at the same moment.
const javaAutoFirstRun = 20 * time.Minute

// StartJavaAutoUpdate starts the weekly question for a newer Java runtime for
// the desktop build's own JD. The desktop build calls it once it has set
// SwitchJava. Close stops it.
func (a *App) StartJavaAutoUpdate() {
	a.spawn(func() { a.tickLoop(javaAutoFirstRun, componentTick, a.autoUpdateJava) })
}

// autoUpdateJava asks after Java once it is due.
func (a *App) autoUpdateJava() {
	if a.SwitchJava != nil && a.componentDue(componentJava, false) {
		a.asked(componentJava, a.updateJavaComponent(a.ctx))
	}
}

// updateJavaComponent asks Adoptium for a newer Temurin 21, fetches it and
// moves JD onto it. The move waits for a JD download, which the restart would
// break off, and for a JD setup under way.
func (a *App) updateJavaComponent(ctx context.Context) ComponentResult {
	if a.SwitchJava == nil {
		return ComponentResult{State: ComponentSkipped}
	}
	cur, err := jre.Load(a.DataDir)
	if err != nil || cur == nil {
		// Without a runtime of its own JD is still being set up, which
		// fetches the pinned one.
		return ComponentResult{State: ComponentSkipped, again: true}
	}
	next, err := jre.Pending(a.DataDir)
	if err != nil {
		log.Printf("Java: the update could not read the runtime it fetched: %v", err)
	}
	if next == nil {
		rel, err := a.latestJava(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("Java: the update could not ask Adoptium for a newer runtime: %v", err)
			}
			return ComponentResult{State: ComponentFailed, Version: cur.Version, Detail: err.Error()}
		}
		if jre.Compare(rel.Version, cur.Version) <= 0 {
			a.setJavaWaits("")
			return ComponentResult{State: ComponentCurrent, Version: cur.Version}
		}
		a.setJavaWaits(rel.Version)
		if rel.Version == a.refusedJava() {
			return javaRefusedResult(cur.Version, rel.Version)
		}
		if err := a.stageJava(ctx, rel); err != nil {
			if ctx.Err() == nil {
				log.Printf("Java: the update could not fetch Java %s: %v", rel.Version, err)
			}
			return ComponentResult{State: ComponentFailed, Version: cur.Version, Detail: err.Error()}
		}
		log.Printf("Java: the update fetched Java %s, and JD moves onto it once none of its downloads runs", rel.Version)
		next = &jre.Record{Version: rel.Version}
	}
	a.setJavaWaits(next.Version)
	if next.Version == a.refusedJava() {
		return javaRefusedResult(cur.Version, next.Version)
	}
	if a.jdDownloading() {
		return ComponentResult{State: ComponentWaits, Version: cur.Version, again: true}
	}
	err = a.SwitchJava(ctx)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return ComponentResult{State: ComponentFailed, Version: cur.Version, Detail: ctx.Err().Error()}
	case errors.Is(err, ErrJavaRefused):
		log.Printf("Java: JD stays on Java %s: %v", cur.Version, err)
		a.javaMu.Lock()
		a.javaRefused = next.Version
		a.javaMu.Unlock()
		return javaRefusedResult(cur.Version, next.Version)
	default:
		log.Printf("Java: the update could not move JD onto Java %s yet: %v", next.Version, err)
		return ComponentResult{State: ComponentWaits, Version: cur.Version, Detail: err.Error(), again: true}
	}
	a.setJavaWaits("")
	return ComponentResult{State: ComponentUpdated, Version: next.Version}
}

func javaRefusedResult(cur, refused string) ComponentResult {
	return ComponentResult{State: ComponentFailed, Version: cur, Detail: "JD did not start on Java " + refused}
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
