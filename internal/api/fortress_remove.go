package api

import (
	"cmp"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
)

// fortressRemoved is a member this instance took out of the fortress since
// the phrase last changed. Until it changes, the phrase lets it back in.
type fortressRemoved struct {
	App    string `json:"app"`
	Name   string `json:"name"`
	Device string `json:"device"`
}

var errNoMember = errors.New("no other member of the fortress has that id")

// removing keeps two removals from writing the list over each other.
var removing sync.Mutex

// removeFortressMember takes another member out of the fortress, here and at
// every member in reach, and keeps its name until the phrase changes.
func removeFortressMember(a *app.App, id string) error {
	g := fortressOf(a)
	if g == nil {
		return errNoFortress
	}
	for _, m := range g.f.Members() {
		if m.ID != id {
			continue
		}
		if err := g.f.Remove(id); err != nil {
			return err
		}
		removing.Lock()
		defer removing.Unlock()
		list := append(removedOf(a), fortressRemoved{App: m.App.ID, Name: cmp.Or(m.App.Name, m.App.ID), Device: m.Device})
		raw, err := json.Marshal(list)
		if err != nil {
			return err
		}
		return a.Accounts.Set(federation.FortressRemovedAccount, string(raw))
	}
	return errNoMember
}

// removedOf is the members this instance took out since the phrase last
// changed.
func removedOf(a *app.App) []fortressRemoved {
	list := []fortressRemoved{}
	raw, err := a.Accounts.Get(federation.FortressRemovedAccount)
	if err != nil || raw == "" {
		return list
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		log.Printf("fortress: read the removed members: %v", err)
		return []fortressRemoved{}
	}
	return list
}

// takenOut reports whether another member took this instance out of the
// fortress, which the empty place says until it joins again.
func takenOut(a *app.App) bool {
	v, err := a.Accounts.Get(federation.FortressRemovedHereAccount)
	return err == nil && v != ""
}
