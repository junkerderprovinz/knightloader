package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/fortress"
)

// logoSVG is the KnightLoader mark the other apps of the fortress show.
//
//go:embed logo.svg
var logoSVG []byte

// Icon is that mark as the status report and the fortress carry it.
var Icon, _ = status.IconFromSVG(logoSVG)

// fortressLogSize is how many events the fortress log keeps.
const fortressLogSize = 500

// needsDoneField is where the overview keeps what the person set aside, in
// the default bucket of the interface state, so a dashboard leaves out what the
// overview leaves out.
const needsDoneField = "overview.needsDone"

// FortressSnapshot is this instance as the fortress status reports it.
func (a *App) FortressSnapshot(ctx context.Context) (fortress.Snapshot, error) {
	s := a.Settings.Get()
	health := a.HealthReport()

	name := s.InstanceName
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = "KnightLoader"
	}
	id := s.InstanceID
	if id == "" {
		id = name
	}

	// Members and dashboards read this, so a task carries no password and its
	// names and addresses no credentials, as a member reading /api/tasks gets them.
	var tasks []*core.Task
	for _, t := range a.Tasks() {
		clean := t.WithoutCredentials()
		tasks = append(tasks, &clean)
	}
	in := fortress.Inputs{
		Now:      time.Now(),
		Tasks:    tasks,
		Captchas: a.CaptchaChallenges(),
		Logins:   a.HosterLogins(),
		DiskLow:  s.DiskLowSpace,
		DiskStop: s.DiskCriticalSpace,
		Done:     a.needsDone(),
	}
	for _, acc := range a.AccountStates() {
		in.Accounts = append(in.Accounts, fortress.Account{
			ID: acc.ID, Name: accountName(acc), Enabled: acc.Enabled, Configured: acc.Configured,
			OK: acc.OK, Detail: acc.Detail, Expiry: acc.Expiry,
		})
	}
	for _, p := range health.Subsystems {
		in.Parts = append(in.Parts, fortress.Part{ID: p.ID, State: string(p.State), Remedy: p.Remedy, Since: p.Since})
	}
	for _, v := range health.Volumes {
		in.Volumes = append(in.Volumes, fortress.Volume{Dir: v.Dir, Free: v.Free, Known: v.Known})
	}
	if u := a.VolumeUsage(); u.Cap > 0 && u.Reached {
		in.Cap = fortress.Cap{Reached: true, PeriodStart: u.PeriodStart}
	}

	return fortress.Snapshot{
		App:      status.App{ID: "knightloader", Name: "KnightLoader", Version: buildinfo.Version, Commit: buildinfo.Revision(), Icon: Icon},
		Instance: status.Instance{ID: id, Name: name, Container: buildinfo.ContainerID()},
		Health:   string(health.Status),
		Inputs:   in,
	}, ctx.Err()
}

// accountName is the service's name with the account's own label, as the
// overview writes it. A seedbox is named by its address.
func accountName(acc AccountState) string {
	svc, ok := accounts.Lookup(acc.Service)
	if ok && svc.Group == accounts.GroupRemoteServer {
		return acc.Account
	}
	name := acc.Service
	if ok {
		name = svc.Label
	}
	if acc.Label != "" {
		return name + " (" + acc.Label + ")"
	}
	return name
}

// needsDone reads what the overview set aside. A state that cannot be read
// sets nothing aside, which shows a need twice rather than hiding one.
func (a *App) needsDone() map[string]bool {
	raw, err := a.UIState("default")
	if err != nil || raw == "" {
		return nil
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return nil
	}
	var keys []string
	if json.Unmarshal(doc[needsDoneField], &keys) != nil {
		return nil
	}
	done := make(map[string]bool, len(keys))
	for _, k := range keys {
		done[k] = true
	}
	return done
}
