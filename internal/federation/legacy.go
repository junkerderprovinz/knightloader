package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// Instances on a release before kit/group share the group's phones and
// browsers through GET /api/group/roster and pass every decision on over the
// /api/connect routes. Both stay for one release, so a group can be updated
// one instance at a time.

// LegacyRosterPath is where an instance answers with its LegacyRoster.
const LegacyRosterPath = "/api/group/roster"

// LegacyApp is one phone or browser as an instance before kit/group tells
// the others of it.
type LegacyApp struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Deployment string    `json:"deployment"`
	LastSeen   time.Time `json:"lastSeen"`
	// Key is the device's key once it was approved there.
	Key []byte `json:"key,omitempty"`
}

// LegacyRoster is the phones and browsers an instance before kit/group knows,
// and the ones taken out, by id and when.
type LegacyRoster struct {
	Apps    []LegacyApp          `json:"apps"`
	Removed map[string]time.Time `json:"removed,omitempty"`
}

// clientKinds are the members that join a group without being an instance.
var clientKinds = map[member.Kind]bool{member.KindMobile: true, member.KindExtension: true}

// LegacyRosterOf is r as an instance before kit/group reads it: its phones
// and browsers, each with its key once approved, and the ones taken out.
func LegacyRosterOf(r group.Roster) LegacyRoster {
	out := LegacyRoster{Apps: []LegacyApp{}, Removed: map[string]time.Time{}}
	for _, mb := range r.Members {
		if !clientKinds[mb.Kind] {
			continue
		}
		app := LegacyApp{ID: mb.ID, Name: mb.Name, Deployment: string(mb.Kind), LastSeen: mb.LastSeen}
		if mb.State == group.StateApproved {
			app.Key = mb.Key
		}
		out.Apps = append(out.Apps, app)
	}
	for id, at := range r.Removed {
		if clientKinds[KindOfID(id)] {
			out.Removed[id] = at
		}
	}
	return out
}

// Roster is r as group.Group.Import takes it. A device without a key was
// never approved there and leaves nothing to take.
func (r LegacyRoster) Roster() group.Roster {
	out := group.Roster{Removed: r.Removed}
	for _, app := range r.Apps {
		if len(app.Key) == 0 {
			continue
		}
		out.Members = append(out.Members, group.Member{ID: app.ID, Kind: KindOfID(app.ID), Key: app.Key,
			State: group.StateApproved, Name: app.Name, Deployment: app.Deployment, LastSeen: app.LastSeen})
	}
	return out
}

// KindOfID is the kind of member whose ids have id's shape, "" for none. The
// shapes never overlap, so no phone can be taken out under an instance's id,
// nor an instance under a phone's.
func KindOfID(id string) member.Kind {
	switch {
	case relay.ValidInstanceID(id):
		return member.KindInstance
	case hashAfter(id, "phone-"):
		return member.KindMobile
	case hashAfter(id, "browser-"):
		return member.KindExtension
	}
	return ""
}

// hashAfter reports whether id is prefix and then a SHA-256 in lowercase hex,
// as member.ID writes it.
func hashAfter(id, prefix string) bool {
	rest, ok := strings.CutPrefix(id, prefix)
	if !ok || len(rest) != 2*sha256.Size {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil && strings.ToLower(rest) == rest
}

// shareEvery is how often SyncLegacy asks one instance for its roster.
const shareEvery = time.Minute

// SyncLegacy asks every approved instance of the group reachable now for its
// roster on the route of releases before kit/group, each at most once every
// shareEvery, and takes what this instance lacks. It returns when the calls
// are done or ctx ends.
func (m *Manager) SyncLegacy(ctx context.Context, now time.Time) {
	g := m.Group()
	if g == nil {
		return
	}
	var due []string
	m.mu.Lock()
	for _, mb := range g.Members() {
		if mb.Kind != member.KindInstance || mb.State != group.StateApproved || !mb.Online() || now.Sub(m.shared[mb.ID]) < shareEvery {
			continue
		}
		if m.shared == nil {
			m.shared = map[string]time.Time{}
		}
		m.shared[mb.ID] = now
		due = append(due, mb.ID)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range due {
		wg.Go(func() {
			status, body, err := g.Call(ctx, id, http.MethodGet, LegacyRosterPath, nil)
			var r LegacyRoster
			if err != nil || status != http.StatusOK || json.Unmarshal(body, &r) != nil {
				return
			}
			_ = g.Import(r.Roster())
		})
	}
	wg.Wait()
}
