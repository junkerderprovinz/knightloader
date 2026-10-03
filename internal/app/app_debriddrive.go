package app

import (
	"sort"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/debriddrive"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

func (a *App) newDebridDrive() *debriddrive.Drive {
	refresh := func() time.Duration {
		return time.Duration(a.Settings.Get().DebridDrive.RefreshMinutes) * time.Minute
	}
	// A player holds a read open for as long as it plays, so there is no
	// ceiling on the request as a whole.
	return debriddrive.New(a.driveAccounts, refresh, httpx.New(httpx.Options{Timeout: httpx.NoTimeout}))
}

// driveAccounts is every account the debrid drive shows: the wired ones whose
// service can list what is on them, the same ones the import can follow.
func (a *App) driveAccounts() []debriddrive.Account {
	a.bmu.RLock()
	defer a.bmu.RUnlock()
	var out []debriddrive.Account
	for slot, be := range a.debrid {
		tb, ok := be.(*debrid.TorrentBackend)
		if !ok {
			continue
		}
		if src, ok := tb.Service().(debriddrive.Source); ok {
			out = append(out, debriddrive.Account{Slot: slot, Name: driveFolder(slot), Source: src})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DriveFolders names the account folders the debrid drive shows.
func (a *App) DriveFolders() []string {
	return a.DebridDrive.Folders()
}

// driveFolder names an account's folder: the service, and the account id for
// a further one, which a person typed and which stays put when the label
// changes.
func driveFolder(slot string) string {
	service, account := resolver.SplitSlot(slot)
	name := service
	if svc, ok := accounts.Lookup(service); ok {
		name = svc.Label
	}
	if account != "" {
		name += " (" + account + ")"
	}
	return name
}
