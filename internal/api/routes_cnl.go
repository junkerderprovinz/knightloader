package api

import (
	"context"
	"errors"
	"log"
	"net/http"
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
	holder, err := a.CnL.Holder(ctx)
	switch {
	case errors.Is(err, cnl.ErrNotKnightLoader):
		return &cnlRefusal{code: "cnlHeldByOther", params: map[string]string{"address": a.CnL.Address()},
			text: "another program holds " + a.CnL.Address() + ", and only a KnightLoader can be asked to let go of it"}
	case err != nil:
		return &cnlRefusal{code: "cnlUnavailable", params: map[string]string{"address": a.CnL.Address()},
			text: "cannot listen on " + a.CnL.Address() + ": " + err.Error()}
	}
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
			return &cnlRefusal{code: "cnlUnavailable", params: map[string]string{"address": a.CnL.Address()},
				text: "cannot listen on " + a.CnL.Address() + ": " + err.Error()}
		}
		time.Sleep(100 * time.Millisecond)
	}
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
