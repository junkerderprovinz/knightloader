package settings

import "testing"

// A new install watches for downloads that stop moving, and reconnects them.
func TestAFreshInstallWatchesForStalls(t *testing.T) {
	got := loadFrom(t, `{}`)
	if got.StallTimeout != DefaultStallTimeout {
		t.Errorf("StallTimeout = %d on a fresh install, want %d", got.StallTimeout, DefaultStallTimeout)
	}
	if !got.StallReconnect {
		t.Error("a fresh install does not reconnect a stalled download")
	}
	if got.StallRestart {
		t.Error("a fresh install restarts stalled downloads from the top, throwing their bytes away")
	}
}

// The 0 an earlier build saved is the default it shipped with, not a choice,
// so an existing install gets the watcher too.
func TestAnEarlierBuildsStoredZeroSwitchesTheWatcherOn(t *testing.T) {
	got := loadFrom(t, `{"stallTimeout":0,"stallRestart":false,"stallMaxRestarts":0}`)
	if got.StallTimeout != DefaultStallTimeout {
		t.Errorf("StallTimeout = %d, want %d", got.StallTimeout, DefaultStallTimeout)
	}
	if !got.StallReconnect {
		t.Error("the migrated install does not reconnect")
	}
}

// A timeout somebody typed is kept, and so is a restart they switched on.
func TestAnEarlierBuildsOwnTimeoutIsKept(t *testing.T) {
	got := loadFrom(t, `{"stallTimeout":600,"stallRestart":true}`)
	if got.StallTimeout != 600 {
		t.Errorf("StallTimeout = %d, want the 600 that was set", got.StallTimeout)
	}
	if !got.StallRestart {
		t.Error("the restart that was switched on is off")
	}
}

// Once the document carries stallReconnect, a 0 was chosen with the reconnect
// on offer, and switching the watcher off survives the next load.
func TestAWatcherSwitchedOffStaysOff(t *testing.T) {
	for _, doc := range []string{
		`{"stallTimeout":0,"stallReconnect":true}`,
		`{"stallTimeout":0,"stallReconnect":false}`,
	} {
		if got := loadFrom(t, doc).StallTimeout; got != 0 {
			t.Errorf("%s: StallTimeout = %d, want 0", doc, got)
		}
	}
}
