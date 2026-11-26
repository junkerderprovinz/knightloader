package app

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// PausedAccount is one bench running on an account that is switched on, as a
// member of the group reads it. The account's id, its label and the failure's
// own words stay on the accounts page, which a member does not reach.
type PausedAccount struct {
	// ID is the same for the same bench from one look to the next, a hash
	// of the account and the hoster rather than either.
	ID      string `json:"id"`
	Service string `json:"service"`
	// Host is the hoster the bench covers, "" for every hoster.
	Host   string `json:"host,omitempty"`
	Volume bool   `json:"volume,omitempty"`
	// Until is RFC3339.
	Until string `json:"until"`
}

// PausedAccounts lists every bench running on an account that is switched on.
func (a *App) PausedAccounts() []PausedAccount {
	out := []PausedAccount{}
	for _, st := range a.AccountStates() {
		if !st.Enabled {
			continue
		}
		name := st.Service
		if svc, ok := accounts.Lookup(st.Service); ok {
			name = svc.Label
		}
		for _, p := range st.Paused {
			sum := sha256.Sum256([]byte(st.ID + "\x00" + p.Host))
			out = append(out, PausedAccount{
				ID:      hex.EncodeToString(sum[:8]),
				Service: name,
				Host:    p.Host,
				Volume:  p.Volume,
				Until:   p.Until,
			})
		}
	}
	return out
}
