package settings

import "testing"

// Both fields are at their zero value out of the box, because an int absent
// from an older settings.json unmarshals to exactly that, which is what makes
// the update that introduces database maintenance a no-op on every instance
// already running. Anything else would freeze somebody's database writes at
// four in the morning because they installed a new version.
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

// Both ends. The due check compares now against the armed-at stamp plus the
// interval, so a negative one puts that moment in the past and the database
// compacts itself on every tick.
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

// Somebody who switches the schedule off for a fortnight and on again finds
// their answer to "and compact too" where they left it, which is why the
// interface dims that switch rather than hiding it.
func TestCompactOnScheduleSurvivesTheScheduleBeingOff(t *testing.T) {
	out := sanitize(Settings{MaintenanceIntervalDays: 0, MaintenanceCompactOnSchedule: true})
	if !out.MaintenanceCompactOnSchedule {
		t.Error("switching the schedule off silently cleared the compact answer; it would come back off when the schedule came back on")
	}
}

// The hooks run only because sanitize's own list names them, and a field
// cleaned by a function nothing calls is never cleaned. Asserted through
// sanitize rather than sanitizeMaintenance for that reason.
func TestSanitizeMaintenanceIsInTheChain(t *testing.T) {
	if got := sanitize(Settings{MaintenanceIntervalDays: -5}).MaintenanceIntervalDays; got != 0 {
		t.Errorf("sanitize left the interval at %d; sanitizeMaintenance is not in sanitize's list", got)
	}
}
