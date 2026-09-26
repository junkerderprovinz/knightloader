// Package eventprog starts a program the operator configured when one of the
// events internal/script publishes happens, like pyLoad's ExternalScripts and
// rdt-client's run-on-completion.
//
// It is a package of its own rather than a binding in the script sandbox: a
// script comes from a browser editor and has no way to start a process (see
// internal/script), while a program row is a path in the settings, which only
// whoever may change the settings can set.
//
// A program gets the event twice: as %%placeholders%% in its arguments, with
// the event targets' vocabulary plus a few names of its own, and as KL_*
// environment variables. It runs without a shell, so a file name with "$(...)"
// or ";" in it reaches the program as those characters. Somebody who wants a
// shell configures one as the program and reads the variables there.
package eventprog

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/idleaction"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// How many runs of one program may be under way at once. One by default, so
// a program sees its events in the order they happened; a program that only
// ever looks at the file it was handed can be allowed more.
const (
	DefaultParallel = 1
	MaxParallel     = 4
)

// Program is one stored row: what runs, on which events, and how many at once.
//
// The zero value runs nothing, and a settings.json written before this feature
// existed decodes to nil and starts no goroutine.
type Program struct {
	// ID is assigned by Sanitize and ties a row to its health row and to the
	// command line Redacted hid, so an edit elsewhere in the list cannot move
	// either onto another row.
	ID string `json:"id"`
	// Name is what the row is called in the list and in the log. The log
	// never names the program itself, see Redacted.
	Name string `json:"name"`
	// Enabled is false on a new row and for an absent key, as with an event
	// target: a program is written and checked before it runs on anything.
	Enabled bool `json:"enabled"`
	// Command is the program and its arguments, the shape and the bounds the
	// end-of-queue command has, with placeholders allowed in the arguments.
	Command idleaction.CommandSpec `json:"command"`
	// Triggers is which events start the program. An empty list means none.
	Triggers []script.Trigger `json:"triggers,omitempty"`
	// Parallel is how many runs may be under way at once. 0 resolves to
	// DefaultParallel.
	Parallel int `json:"parallel"`
}

// ResolvedParallel is how many runs this row may have going, with the zero
// resolved.
func (p Program) ResolvedParallel() int {
	switch {
	case p.Parallel <= 0:
		return DefaultParallel
	case p.Parallel > MaxParallel:
		return MaxParallel
	default:
		return p.Parallel
	}
}

// Wants reports whether an event starts this program. A disabled row wants
// nothing.
func (p Program) Wants(tr script.Trigger) bool {
	if !p.Enabled {
		return false
	}
	for _, want := range p.Triggers {
		if want == tr {
			return true
		}
	}
	return false
}

// Runnable reports whether the row can start anything at all. A row that
// cannot gets no worker.
func (p Program) Runnable() bool {
	return p.ID != "" && p.Enabled && p.Command.Configured() && len(p.Triggers) > 0
}

// Sanitize normalises a list that came from settings.json or from a save. It
// always succeeds, the rule every settings hook follows.
//
// A row with a name and no program yet is kept, since that is somebody halfway
// through typing one. A row with neither is what an untouched Add button leaves
// behind and goes.
func Sanitize(in []Program) []Program {
	if len(in) == 0 {
		return nil
	}
	out := make([]Program, 0, len(in))
	for _, p := range in {
		p.Name = strings.TrimSpace(p.Name)
		p.Command = p.Command.Sanitize()
		if p.Name == "" && !p.Command.Configured() && len(p.Command.Args) == 0 {
			continue
		}
		p.Triggers = sanitizeTriggers(p.Triggers)
		if p.Parallel < 0 {
			p.Parallel = 0
		} else if p.Parallel > MaxParallel {
			p.Parallel = MaxParallel
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	identify(out)
	return out
}

// sanitizeTriggers drops duplicates and anything this build does not fire,
// keeping the operator's order, as notify does for its targets.
func sanitizeTriggers(in []script.Trigger) []script.Trigger {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[script.Trigger]bool, len(in))
	out := make([]script.Trigger, 0, len(in))
	for _, tr := range in {
		if !tr.Valid() || seen[tr] {
			continue
		}
		seen[tr] = true
		out = append(out, tr)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// identify gives every row an ID, the first claim winning and every other row
// a new random one. An ID has to survive an edit elsewhere in the list, or the
// health table and the command line Merge puts back would follow the position
// instead of the row.
//
// Random rather than the lowest free number, because Merge hands a stored
// command line to whichever incoming row carries its ID. With numbers, a row
// still shown in a second tab after it was deleted in the first, or a row from
// another instance's export, would carry "1" and pick up the program of
// whatever row holds "1" by then. For the same reason an ID a client made up
// in any other shape is replaced: an API client that numbers its rows would
// bring the numbers back.
func identify(out []Program) {
	taken := make(map[string]bool, len(out))
	for i := range out {
		id := out[i].ID
		if !wellFormed(id) || taken[id] {
			id = newID()
		}
		out[i].ID = id
		taken[id] = true
	}
}

const idBytes = 8

func newID() string {
	b := make([]byte, idBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// wellFormed reports whether id has the shape newID gives it.
func wellFormed(id string) bool {
	return len(id) == 2*idBytes && strings.Trim(id, "0123456789abcdef") == ""
}

// Redacted returns a copy safe to hand to a browser or to put into the
// diagnostics bundle: the whole command line is replaced, for the reasons
// idleaction.CommandSpec.Redacted gives. An argument can carry a token, and a
// path names the person whose home folder it is in.
func Redacted(p Program) Program {
	p.Command = p.Command.Redacted()
	return p
}

// Merge puts back the command lines Redacted removed, matched by row ID, so a
// settings page that was shown the placeholder and sent it straight back does
// not wipe what is stored.
//
// The carry-back is not bound to anything beyond the ID, unlike an event
// target's header, which follows only while the address is unchanged. A client
// that may set the program may run anything it likes on this machine, the
// stored arguments included, so there is nothing a stricter rule would keep
// from it. What the rule has to prevent is an accident, a row landing on
// another row's program, and the random IDs identify hands out do that.
//
// A placeholder with nothing to restore, on a row with no stored counterpart or
// beside a changed number of arguments, is cleared rather than kept: eight
// stars would be run as a program called "********".
//
// Call it before Sanitize, since it matches on the IDs Sanitize handed out.
func Merge(next, prev []Program) []Program {
	if len(next) == 0 {
		return next
	}
	old := make(map[string]Program, len(prev))
	for _, p := range prev {
		if p.ID != "" {
			old[p.ID] = p
		}
	}
	out := make([]Program, len(next))
	copy(out, next)
	for i := range out {
		cmd := out[i].Command.WithSecretsFrom(old[out[i].ID].Command)
		if cmd.Program == idleaction.RedactedCommand {
			cmd.Program = ""
		}
		if len(cmd.Args) > 0 {
			args := make([]string, 0, len(cmd.Args))
			for _, a := range cmd.Args {
				if a != idleaction.RedactedCommand {
					args = append(args, a)
				}
			}
			cmd.Args = args
		}
		out[i].Command = cmd
	}
	return out
}
