package settings

import "testing"

// TestTheResetDayIsClampedIntoAMonth guards the two ends separately, because
// they are two different mistakes with two different right answers. Below 1 is
// an install that has never seen this key and gets the default; above 31 is a
// typo, and the end of the month is what the person meant - handing them the
// 1st would move their allowance to the other end of it.
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
	// 31 survives. What it means in February is the period arithmetic's job and
	// is solved there, once - see internal/app/app_volumecap.go.
	if got := sanitize(Settings{VolumeCapResetDay: 31}).VolumeCapResetDay; got != 31 {
		t.Errorf("reset day = %d, want the 31 that was typed", got)
	}
}

// TestTheCapFiguresAreClamped keeps a byte count typed as gigabytes out of a
// queue that pauses on its first download, and reads a negative as "off"
// rather than refusing the whole save.
func TestTheCapFiguresAreClamped(t *testing.T) {
	got := sanitize(Settings{VolumeCap: -1, VolumeCapThrottle: -1})
	if got.VolumeCap != 0 || got.VolumeCapThrottle != 0 {
		t.Errorf("negatives survived sanitize: cap=%d throttle=%d", got.VolumeCap, got.VolumeCapThrottle)
	}
	if got := sanitize(Settings{VolumeCap: maxVolumeCapBytes * 10}).VolumeCap; got != maxVolumeCapBytes {
		t.Errorf("cap = %d, want it clamped to %d", got, maxVolumeCapBytes)
	}
}

// TestAnUnknownActionFoldsOntoReport is the guard on the value that does not
// exist. There is no "off" action - the off state is a cap of 0 - so every
// string that is not one of the three acting words has to become the one that
// changes nothing, including the empty string every install upgrading into
// this key carries.
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

// TestAFreshInstallShipsNoCapAtAll is the shipping decision written down.
// Nobody's queue may change because an update shipped a default, so the cap is
// 0 and the two answers that mean nothing without one are written out anyway -
// the advanced table serves Defaults() unsanitised, and a reset day of 0 shown
// as the factory setting would be a value the app never actually uses.
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
