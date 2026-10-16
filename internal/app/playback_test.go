package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const (
	quietUpload = 64 << 10
	loudUpload  = 4 << 20
)

func withPlayback() settings.Settings {
	s := loudAndQuiet()
	s.Quiet.UploadLimit = quietUpload
	s.Torrent.UploadLimit = loudUpload
	s.Playback.HoldExtraction = true
	return s
}

func TestAStreamPutsQuietModeInForceAndItsEndLiftsIt(t *testing.T) {
	a := newTurtleApp(t, withPlayback())

	a.setPlayback(true)
	q := a.Queue()
	if !q.Quiet || !q.Playback {
		t.Fatalf("a counting stream left the queue loud: %+v", q)
	}
	if got := limitInForce(t, a); got != quietTurtle {
		t.Errorf("the limit in force is %d during a stream, want the quiet %d", got, quietTurtle)
	}
	if q.UploadLimit != quietUpload {
		t.Errorf("the upload cap is %d during a stream, want the quiet %d", q.UploadLimit, quietUpload)
	}

	a.setPlayback(false)
	q = a.Queue()
	if q.Quiet || q.Playback {
		t.Fatalf("the queue stayed quiet after the stream ended: %+v", q)
	}
	if got := limitInForce(t, a); got != quietDaytime {
		t.Errorf("the limit in force is %d after the stream, want the ordinary %d", got, quietDaytime)
	}
	if q.UploadLimit != loudUpload {
		t.Errorf("the upload cap is %d after the stream, want the ordinary %d", q.UploadLimit, loudUpload)
	}
}

// The turtle has to answer while a film runs, or it sits dead until the credits.
func TestSwitchingOffDuringAStreamWaivesItUntilTheNextOne(t *testing.T) {
	a := newTurtleApp(t, withPlayback())

	a.setPlayback(true)
	a.SetQuiet(false)
	if q := a.Queue(); q.Quiet || q.Playback {
		t.Fatalf("the press did nothing while a stream held the mode: %+v", q)
	}

	a.setPlayback(false)
	a.setPlayback(true)
	if !a.Queue().Quiet {
		t.Error("the next stream did not put the mode in force again")
	}
}

// A stream can only switch the mode on; its end must not take away a mode the
// user switched on by hand.
func TestAStreamEndingLeavesAManualQuietModeAlone(t *testing.T) {
	a := newTurtleApp(t, withPlayback())

	a.SetQuiet(true)
	a.setPlayback(true)
	a.setPlayback(false)
	if !a.Queue().Quiet {
		t.Error("the end of a stream switched off a mode pressed on by hand")
	}
}

func TestUnpackingWaitsForAStream(t *testing.T) {
	a := newTurtleApp(t, withPlayback())

	a.mu.Lock()
	st := a.unpackLocked()
	st.jobs["j"] = &extractJob{ExtractJob: ExtractJob{ID: "j", Status: ExtractQueued}}
	st.order = append(st.order, "j")
	// Taken, so nothing below starts a real worker on a job without a task.
	st.busy = true
	a.mu.Unlock()

	a.setPlayback(true)
	if jobs := a.ExtractJobs(); len(jobs) != 1 || !jobs[0].Held {
		t.Fatalf("a waiting job is not marked as held by the stream: %+v", jobs)
	}

	a.SetQuiet(false)
	if jobs := a.ExtractJobs(); jobs[0].Held {
		t.Error("a waived stream still holds the unpacking queue")
	}
}

func TestUnpackingIsNotHeldWhenTheSettingIsOff(t *testing.T) {
	s := withPlayback()
	s.Playback.HoldExtraction = false
	a := newTurtleApp(t, s)

	a.setPlayback(true)
	a.mu.Lock()
	held := a.extractHeldLocked()
	a.mu.Unlock()
	if held {
		t.Error("the unpacking queue waits for a stream with the setting off")
	}
	if !a.Queue().Quiet {
		t.Error("fixture broken: the stream should still put quiet mode in force")
	}
}

// The timetable runner applies only when its own answer changes, which a new
// quiet figure does not do, so a save has to put the figure in force itself.
func TestANewQuietLimitAppliesWhileTheModeIsOn(t *testing.T) {
	s := withPlayback()
	a := newTurtleApp(t, s)

	a.SetQuiet(true)
	s.Quiet.SpeedLimit = quietTurtle / 2
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	if got := limitInForce(t, a); got != quietTurtle/2 {
		t.Errorf("the limit in force is %d after the save, want the new quiet %d", got, quietTurtle/2)
	}
}
