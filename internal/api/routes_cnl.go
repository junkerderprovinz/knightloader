package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
)

// One computer has one Click'n'Load port. A second KnightLoader on it, the
// desktop app beside a server run as a plain program for instance, finds the
// port taken; these routes let it name the one holding it and take it over
// through their group.

// takeoverWait is how long a take-over waits for the holder's port to come
// free once the holder has agreed to let go of it.
const takeoverWait = 3 * time.Second

func registerCnL(reg *Registry, a *app.App) {
	// The routes exist without a listener too, so the group's list of what a
	// member may call holds in every build.
	if a.CnL != nil {
		a.CnL.SetIdentity(func() cnl.Identity {
			return cnl.Identity{Name: instanceDisplayName(a), Instance: groupID(a), Kind: instanceKind()}
		})
	}

	reg.Add(http.MethodPost, "/api/cnl/takeover", "ask the KnightLoader holding the Click'n'Load port on this computer to let go of it, through the group, and listen here",
		func(w http.ResponseWriter, r *http.Request) {
			if err := takeOverCnL(r.Context(), a); err != nil {
				writeSwitchRefusal(w, err)
				return
			}
			a.Hub.Broadcast("settings", nil)
			writeJSON(w, featureState(a, requestBasePath(r)))
		})

	// A group member calls this to take the port over. It only ever closes
	// this instance's own listener, which the switch here can open again.
	reg.Add(http.MethodPost, "/api/cnl/release", "let go of the Click'n'Load port so another KnightLoader on this computer can listen",
		func(w http.ResponseWriter, r *http.Request) {
			if a.CnL == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			a.CnL.Stop()
			log.Printf("Click'n'Load: handed %s over to another KnightLoader on this computer", a.CnL.Address())
			a.Hub.Broadcast("settings", nil)
			w.WriteHeader(http.StatusNoContent)
		})
}

// cnlRefusal is a take-over that could not happen, with the code and values
// the switch's line is worded by.
type cnlRefusal struct {
	code   string
	params map[string]string
	text   string
}

func (e *cnlRefusal) Error() string { return e.text }

// takeOverCnL asks whatever holds the port to let go of it and listens here.
func takeOverCnL(ctx context.Context, a *app.App) error {
	if a.CnL == nil {
		return errNoSwitch
	}
	if a.CnL.Start() == nil {
		return nil
	}
	held := a.CnL.Holding(ctx)
	if !held.Answered() {
		return cnlBusy(a.CnL, held)
	}
	holder := held.Identity
	member, ok := cnlHolderMember(a, holder.Instance)
	if !ok {
		return &cnlRefusal{code: "cnlHolderOutsideGroup", params: map[string]string{"name": holder.Name},
			text: "KnightLoader " + holder.Name + " holds the port and is not in this instance's group, so it cannot be asked; switch Click'n'Load off there"}
	}
	if _, code, err := a.Federation.Proxy(ctx, member, http.MethodPost, "/api/cnl/release", nil); err != nil || code >= 300 {
		return &cnlRefusal{code: "cnlReleaseFailed", params: map[string]string{"name": holder.Name},
			text: "KnightLoader " + holder.Name + " did not let go of the port"}
	}
	deadline := time.Now().Add(takeoverWait)
	for {
		if err := a.CnL.Start(); err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return cnlBusy(a.CnL, a.CnL.Holding(ctx))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// cnlHeldLine words what holds the port, in the line the switch's row and a
// refused switch share. The system's own error never reaches the reader: it
// names neither the program nor what to do about it.
func cnlHeldLine(h cnl.Holding, address string, port int) line {
	p := strconv.Itoa(port)
	switch h.Kind {
	case cnl.HeldByKnightLoader:
		if h.Answered() {
			return line{
				text: "KnightLoader " + h.Name + " on this computer holds " + address,
				code: "cnlHeldBy", args: map[string]string{"name": h.Name, "address": address},
			}
		}
		return line{text: "another KnightLoader on this computer holds port " + p, code: "cnlHeldByKnightLoader", args: map[string]string{"port": p}}
	case cnl.HeldByJDownloader:
		return line{text: h.Name + " holds port " + p, code: "cnlHeldByJDownloader", args: map[string]string{"port": p}}
	case cnl.HeldByProgram:
		return line{text: h.Name + " holds port " + p, code: "cnlHeldByProgram", args: map[string]string{"port": p, "name": h.Name}}
	case cnl.HeldByUnknown:
		return line{text: "port " + p + " is already taken", code: "cnlHeldByOther", args: map[string]string{"port": p, "address": address}}
	}
	return line{text: "cannot listen on " + address, code: "cnlUnavailable", args: map[string]string{"address": address}}
}

// cnlBusy is the refusal for a start that failed, worded by what holds the port.
func cnlBusy(l *cnl.Listener, h cnl.Holding) error {
	ln := cnlHeldLine(h, l.Address(), l.BindPort())
	return &cnlRefusal{code: ln.code, params: ln.args, text: ln.text}
}

// cnlHolderMember is the name the federation addresses the group member with id
// by, when it is in the group and reachable.
func cnlHolderMember(a *app.App, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	for _, in := range a.Federation.List() {
		if in.RelayID == id {
			return in.Name, true
		}
	}
	return "", false
}
