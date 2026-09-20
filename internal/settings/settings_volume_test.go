package settings

import "testing"

// Two different mistakes with two different right answers. Below 1 is an
// install that has never seen this key and gets the default; above 31 is a
// typo, and the end of the month is what was meant, since the 1st would move
// the allowance to the other end of it.
func TestTheResetDayIsClampedIntoAMonth(t *testing.T) {
	if got := sanitize(Settings{}).VolumeCapResetDay; got != DefaultVolumeCapResetDay {
		t.Errorf("reset day = %d on a document that has never carried the key, want the default %d",
			got, DefaultVolumeCapResetDay)
	}
	if got := sanitize(Settings{VolumeCapResetDay: -3}).VolumeCapResetDay; got != DefaultVolumeCapResetDay {
		t.Errorf("reset day = %d for a negative, want the default %d", got, DefaultVolumeCapResetDay)
	}
	if got := sanitize(Settings{VolumeCapResetDay: 45}).VolumeCapResetDay; got != 31 {
		t.Errorf("reset day = %d for 45, want 31: somebody typing past the end of the month meant the end of the month", got)
	}
	// 31 survives. What it means in February is the period arithmetic's job,
	// see internal/app/app_volumecap.go.
	if got := sanitize(Settings{VolumeCapResetDay: 31}).VolumeCapResetDay; got != 31 {
		t.Errorf("reset day = %d, want the 31 that was typed", got)
	}
}

// A byte count typed as gigabytes would pause the queue on its first download.
// A negative reads as off rather than refusing the whole save.
func TestTheCapFiguresAreClamped(t *testing.T) {
	got := sanitize(Settings{VolumeCap: -1, VolumeCapThrottle: -1})
	if got.VolumeCap != 0 || got.VolumeCapThrottle != 0 {
		t.Errorf("negatives survived sanitize: cap=%d throttle=%d", got.VolumeCap, got.VolumeCapThrottle)
	}
	if got := sanitize(Settings{VolumeCap: maxVolumeCapBytes * 10}).VolumeCap; got != maxVolumeCapBytes {
		t.Errorf("cap = %d, want it clamped to %d", got, maxVolumeCapBytes)
	}
}

// There is no "off" action, the off state being a cap of 0, so every string
// that is not one of the three acting words becomes the one that changes
// nothing, including the empty string an install upgrading into this key
// carries.
func TestAnUnknownActionFoldsOntoReport(t *testing.T) {
	for _, in := range []string{"", "off", "disabled", "Pause", "stop"} {
		if got := sanitize(Settings{VolumeCapAction: in}).VolumeCapAction; got != VolumeCapReport {
			t.Errorf("action %q sanitised to %q, want %q", in, got, VolumeCapReport)
		}
	}
	for _, in := range []string{VolumeCapPause, VolumeCapThrottle, VolumeCapReport} {
		if got := sanitize(Settings{VolumeCapAction: in}).VolumeCapAction; got != in {
			t.Errorf("action %q sanitised to %q, want it left alone", in, got)
		}
	}
}

// Nobody's queue changes because an update shipped a default, so the cap is 0.
// The two answers that mean nothing without one are written out anyway, because
// the advanced table serves Defaults() unsanitised and a reset day of 0 shown
// as the factory setting would be a value the app never uses.
func TestAFreshInstallShipsNoCapAtAll(t *testing.T) {
	d := Defaults()
	if d.VolumeCap != 0 {
		t.Errorf("a fresh install ships a cap of %d, want none", d.VolumeCap)
	}
	if d.VolumeCapResetDay != DefaultVolumeCapResetDay {
		t.Errorf("reset day = %d before sanitize, want %d", d.VolumeCapResetDay, DefaultVolumeCapResetDay)
	}
	if d.VolumeCapAction != VolumeCapReport {
		t.Errorf("action = %q before sanitize, want %q", d.VolumeCapAction, VolumeCapReport)
	}
}
