package cnl

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// Process is a program on this computer as the system describes it. Path is
// its executable, or only the executable's name where the system gives no
// path, and Cmdline is empty where the system does not say.
type Process struct {
	PID     int
	Path    string
	Cmdline string
}

// HolderKind is what sort of program holds the Click'n'Load port.
type HolderKind string

const (
	HeldByKnightLoader HolderKind = "knightloader"
	HeldByJDownloader  HolderKind = "jdownloader"
	HeldByProgram      HolderKind = "program"
	// HeldByUnknown is a port something holds that the system does not name.
	HeldByUnknown HolderKind = "unknown"
)

// Holding is what holds the Click'n'Load port. Its Kind is empty where nothing
// is known to hold it and the port failed to open for another reason. Name is
// the program as a reader knows it, or the instance name of a KnightLoader
// that answered for itself, whose answer is then in Identity.
type Holding struct {
	Kind     HolderKind
	Name     string
	Identity Identity
}

// Answered reports whether a KnightLoader on the port named itself and can be
// asked through its group to let go of it.
func (h Holding) Answered() bool {
	return h.Identity.App != ""
}

// jdownloaderJar is JDownloader as java runs it, from its own launcher script
// or a plain java -jar.
var jdownloaderJar = regexp.MustCompile(`(?i)jdownloader[^\\/]*\.jar`)

// classify says what kind of program p is and what to call it. A path from
// another system's separators is split too, so a Windows path reads the same
// wherever this runs.
func classify(p Process) (HolderKind, string) {
	base := p.Path[strings.LastIndexAny(p.Path, `/\`)+1:]
	if len(base) > 4 && strings.EqualFold(base[len(base)-4:], ".exe") {
		base = base[:len(base)-4]
	}
	lower := strings.ToLower(base)
	switch {
	case base == "":
		return HeldByUnknown, ""
	case strings.HasPrefix(lower, "knightloader"):
		return HeldByKnightLoader, "KnightLoader"
	case strings.HasPrefix(lower, "jdownloader"),
		(lower == "java" || lower == "javaw") && jdownloaderJar.MatchString(p.Cmdline):
		return HeldByJDownloader, "JDownloader 2"
	}
	return HeldByProgram, base
}

// judge puts together what the bind, the question at identityPath and the
// system said about the port. A KnightLoader that answers wins, since it can
// be asked to let go; then the program the system names; and a port that is
// taken by something nobody names is still taken.
func judge(bindErr error, id Identity, idErr error, owner *Process) Holding {
	if idErr == nil {
		return Holding{Kind: HeldByKnightLoader, Name: id.Name, Identity: id}
	}
	if owner != nil {
		if kind, name := classify(*owner); kind != HeldByUnknown {
			return Holding{Kind: kind, Name: name}
		}
	}
	if owner != nil || errors.Is(idErr, ErrNotKnightLoader) || addrInUse(bindErr) {
		return Holding{Kind: HeldByUnknown}
	}
	return Holding{}
}

// Holding says what holds this listener's port after a start failed.
func (l *Listener) Holding(ctx context.Context) Holding {
	id, idErr := Holder(ctx, l.port)
	if idErr == nil {
		return judge(nil, id, nil, nil)
	}
	var owner *Process
	if p, ok := portOwner(ctx, l.port); ok {
		owner = &p
	}
	return judge(l.Err(), id, idErr, owner)
}

// BindPort is the port the listener binds, whether or not it is up.
func (l *Listener) BindPort() int {
	return l.port
}
