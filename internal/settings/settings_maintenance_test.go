package settings

import "testing"

// TestMaintenanceShipsOff is the one assertion in this file that is about a
// person's install rather than about arithmetic. Both fields have to be at
// their zero value out of the box, because an int absent from an older
// settings.json unmarshals to exactly that - which is what makes the update
// that introduces database maintenance a no-op on every instance already
// running. A default of anything else would have somebody's database freezing
// every write at four in the morning because they installed a new version.
func TestMaintenanceShipsOff(t *testing.T) {
	d := Defaults()
	if d.MaintenanceIntervalDays != 0 {
		t.Errorf("maintenanceIntervalDays ships at %d, want 0 - an update must not start doing anything by itself", d.MaintenanceIntervalDays)
	}
	if d.MaintenanceCompactOnSchedule {
		t.Error("maintenanceCompactOnSchedule ships on; compacting needs room for a second copy of the database on a volume nobody has checked")
	}
	if DefaultMaintenanceIntervalDays != 0 {
		t.Errorf("DefaultMaintenanceIntervalDays = %d, want 0", DefaultMaintenanceIntervalDays)
	}
}

// TestSanitizeMaintenanceClampsTheInterval covers the two ends. The negative
// case is not tidiness: the due check compares now against the armed-at stamp
// plus the interval, so a negative one puts that moment in the past and the
// database would compact itself on the next tick, and the tick after that, for
// ever.
func TestSanitizeMaintenanceClampsTheInterval(t *testing.T) {
	for _, c := range []struct {
		in, want int
		why      string
	}{
		{in: -1, want: 0, why: "a negative interval is always due, so it would run on every tick"},
		{in: -3650, want: 0, why: "the same, from the other end of a spinner"},
		{in: 0, want: 0, why: "off is a real answer and must survive untouched"},
		{in: 30, want: 30, why: "an offered value passes through"},
		{in: 365, want: 365, why: "the ceiling itself is allowed"},
		{in: 100000, want: maxMaintenanceIntervalDays, why: "a five-digit typo would make \"on\" indistinguishable from off"},
	} {
		got := sanitize(Settings{MaintenanceIntervalDays: c.in}).MaintenanceIntervalDays
		if got != c.want {
			t.Errorf("an interval of %d sanitised to %d, want %d: %s", c.in, got, c.want, c.why)
		}
	}
}

// TestCompactOnScheduleSurvivesTheScheduleBeingOff pins a deliberate
// non-behaviour. Somebody who switches the schedule off for a fortnight and on
// again should find their answer to "and compact too" where they left it; the
// interface dims that switch rather than hiding it for the same reason.
func TestCompactOnScheduleSurvivesTheScheduleBeingOff(t *testing.T) {
	out := sanitize(Settings{MaintenanceIntervalDays: 0, MaintenanceCompactOnSchedule: true})
	if !out.MaintenanceCompactOnSchedule {
		t.Error("switching the schedule off silently cleared the compact answer; it would come back off when the schedule came back on")
	}
}

// TestSanitizeMaintenanceIsInTheChain is what makes the two tests above mean
// anything for a real save: the hooks are only run because sanitize's own list
// names them, and a field cleaned by a function nothing calls is a field that
// is never cleaned. Asserted through sanitize rather than through
// sanitizeMaintenance directly for exactly that reason.
func TestSanitizeMaintenanceIsInTheChain(t *testing.T) {
	if got := sanitize(Settings{MaintenanceIntervalDays: -5}).MaintenanceIntervalDays; got != 0 {
		t.Errorf("sanitize left the interval at %d; sanitizeMaintenance is not in sanitize's list", got)
	}
}
