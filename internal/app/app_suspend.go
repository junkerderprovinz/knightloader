package app

// Suspending the timetable: the rows stay as they are, and the queue does what
// the user set by hand until the suspension ends, by itself or by hand.
//
// A suspension survives a restart. It is a promise about the wall clock, such
// as "no pause window until midnight", and a restart in between (an update, a
// crash, the desktop rebooting) would otherwise bring back the very window the
// user set aside, most likely at night with nobody watching. It is kept in an
// interface-state bucket of its own rather than in the settings: it is not
// configuration, so a settings export, an import on another box or a stale
// settings save must not carry it along.

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/schedule"
)

const suspendBucket = "schedule.suspend"

// ErrSuspendEnded refuses a suspension whose end has already passed, which
// would end before it began.
var ErrSuspendEnded = errors.New("the end of the suspension has already passed")

// suspendRecord is the stored form. An empty bucket is no suspension, and a
// record without Until is one that lasts until it is lifted.
type suspendRecord struct {
	Until *time.Time `json:"until,omitempty"`
}

// storedSuspension reads the suspension a restart carried over. One that ran
// out while the box was down, or a record that cannot be read, is none.
func (a *App) storedSuspension(now time.Time) schedule.Suspension {
	raw, err := a.Store.UIState(suspendBucket)
	if err != nil || raw == "" {
		return schedule.Suspension{}
	}
	var rec suspendRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		log.Printf("ignoring the stored schedule suspension: %v", err)
		return schedule.Suspension{}
	}
	sp := schedule.Suspension{On: true}
	if rec.Until != nil {
		sp.Until = *rec.Until
	}
	if !sp.Covers(now) {
		return schedule.Suspension{}
	}
	return sp
}

// SuspendSchedule sets the timetable aside until `until`, or until
// ResumeSchedule when until is zero. It is written down before it applies, so
// a store that refuses it is reported instead of the suspension ending at the
// next restart with nobody told.
func (a *App) SuspendSchedule(until time.Time) error {
	var rec suspendRecord
	if !until.IsZero() {
		if !until.After(time.Now()) {
			return ErrSuspendEnded
		}
		rec.Until = &until
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := a.Store.SetUIState(suspendBucket, string(b)); err != nil {
		return err
	}
	a.sched.Suspend(schedule.Suspension{On: true, Until: until})
	return nil
}

// ResumeSchedule lets the timetable apply again at once.
func (a *App) ResumeSchedule() error {
	if err := a.Store.SetUIState(suspendBucket, ""); err != nil {
		return err
	}
	a.sched.Suspend(schedule.Suspension{})
	return nil
}
