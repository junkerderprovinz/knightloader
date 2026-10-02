package settings

import "testing"

func TestTheDriveRefreshStaysBetweenAMinuteAndADay(t *testing.T) {
	cases := map[int]int{-3: DefaultDriveRefresh, 0: DefaultDriveRefresh, 1: 1, 30: 30, 100000: maxDriveRefresh}
	for typed, want := range cases {
		n := Defaults()
		n.DebridDrive.RefreshMinutes = typed
		if got := sanitize(n).DebridDrive.RefreshMinutes; got != want {
			t.Errorf("a refresh of %d minutes was saved as %d, want %d", typed, got, want)
		}
	}
	if d := Defaults().DebridDrive; d.Enabled || d.RefreshMinutes != DefaultDriveRefresh {
		t.Errorf("a fresh install starts with %+v, want the drive off and the default refresh", d)
	}
}
