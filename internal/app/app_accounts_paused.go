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
	ID string `json:"id"`
	// Account is the hash the account goes by in PausedAccounts.On.
	Account string `json:"account"`
	Service string `json:"service"`
	// Host is the hoster the bench covers, "" for every hoster.
	Host   string `json:"host,omitempty"`
	Volume bool   `json:"volume,omitempty"`
	// Until is RFC3339.
	Until string `json:"until"`
}

// PausedAccounts is every bench running on an account that is switched on,
// and every account that is, so a bench that ends can be told from an
// account that was switched off or removed while it stood.
type PausedAccounts struct {
	Benches []PausedAccount `json:"benches"`
	On      []string        `json:"on"`
}

// PausedAccounts lists the benches and the accounts in use.
func (a *App) PausedAccounts() PausedAccounts {
	out := PausedAccounts{Benches: []PausedAccount{}, On: []string{}}
	for _, st := range a.AccountStates() {
		if !st.Enabled {
			continue
		}
		account := opaque(st.ID)
		out.On = append(out.On, account)
		name := st.Service
		if svc, ok := accounts.Lookup(st.Service); ok {
			name = svc.Label
		}
		for _, p := range st.Paused {
			out.Benches = append(out.Benches, PausedAccount{
				ID:      opaque(st.ID + "\x00" + p.Host),
				Account: account,
				Service: name,
				Host:    p.Host,
				Volume:  p.Volume,
				Until:   p.Until,
			})
		}
	}
	return out
}

func opaque(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
