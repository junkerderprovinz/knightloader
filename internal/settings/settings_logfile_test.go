package settings

import (
	"encoding/json"
	"testing"
)

// TestLogFileShipsOff is the shipping decision, pinned. An update that started
// writing files nobody asked for would be a behaviour change nobody agreed to,
// and on an Unraid box appdata is usually on the array: a write per log line is
// a spinning disk that never gets to sleep.
func TestLogFileShipsOff(t *testing.T) {
	d := DefaultLogFile()
	if d.Enabled {
		t.Error("the log file ships switched ON")
	}
	if d.MaxMB != DefaultLogMaxMB || d.Keep != DefaultLogKeep {
		t.Errorf("DefaultLogFile() = %+v, want the two numbers written out rather than left at zero", d)
	}
}

// TestAnUpgradeReadsAsOffWithTheDefaultsIntact is what every existing install
// gets. settings.Load unmarshals the stored document OVER Defaults(), so a
// settings.json written before this key existed leaves the whole block at
// whatever Defaults put there - off, with two usable numbers behind it, so that
// switching it on later does something sensible without a second trip.
func TestAnUpgradeReadsAsOffWithTheDefaultsIntact(t *testing.T) {
	got := DefaultLogFile()
	// Exactly what Load does with a document that has no "logFile" key at all.
	if err := json.Unmarshal([]byte(`{"maxConcurrent":4}`), &struct {
		LogFile *LogFile `json:"logFile"`
	}{LogFile: &got}); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("an upgrade switched the log file on by itself")
	}
	if got.MaxMB != DefaultLogMaxMB || got.Keep != DefaultLogKeep {
		t.Errorf("an upgrade read as %+v, want the defaults untouched", got)
	}
}

// TestASavedDocumentKeepsWhatWasSaved. The mirror of the test above: once the
// key IS in the document, the stored values win over the defaults, including a
// Keep of zero - which is a real answer and must not be read as "nothing was
// stored, use three".
func TestASavedDocumentKeepsWhatWasSaved(t *testing.T) {
	got := DefaultLogFile()
	if err := json.Unmarshal([]byte(`{"enabled":true,"maxMb":64,"keep":0}`), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.MaxMB != 64 || got.Keep != 0 {
		t.Errorf("a saved block read back as %+v, want enabled, 64 MB, keep 0", got)
	}
	if clean := got.Sanitized(); clean.Keep != 0 {
		t.Errorf("Sanitized() turned a deliberate Keep of 0 into %d", clean.Keep)
	}
}

// TestSanitizeClampsAndNeverRefuses is this package's posture everywhere: a
// figure it cannot use is the absence of one, not an error the user has to
// dismiss before the rest of their edits will save.
func TestSanitizeClampsAndNeverRefuses(t *testing.T) {
	cases := []struct {
		name string
		in   LogFile
		want LogFile
	}{
		{
			// A cleared box is "I did not type a number", not "one megabyte":
			// a file that rotates every few minutes is not what clearing it
			// meant.
			"a cleared size falls back to the default",
			LogFile{MaxMB: 0, Keep: 3},
			LogFile{MaxMB: DefaultLogMaxMB, Keep: 3},
		},
		{
			"a negative size falls back to the default rather than refusing the save",
			LogFile{MaxMB: -20, Keep: 3},
			LogFile{MaxMB: DefaultLogMaxMB, Keep: 3},
		},
		{
			"a size past the typo guard is clamped, not refused",
			LogFile{MaxMB: 999999, Keep: 3},
			LogFile{MaxMB: maxLogMB, Keep: 3},
		},
		{
			"a negative generation count is zero, which is a real setting",
			LogFile{MaxMB: 8, Keep: -1},
			LogFile{MaxMB: 8, Keep: 0},
		},
		{
			"too many generations is clamped to the ceiling",
			LogFile{MaxMB: 8, Keep: 500},
			LogFile{MaxMB: 8, Keep: maxLogKeep},
		},
		{
			"a sane pair is left exactly alone",
			LogFile{Enabled: true, MaxMB: 32, Keep: 5},
			LogFile{Enabled: true, MaxMB: 32, Keep: 5},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.Sanitized(); got != c.want {
				t.Errorf("%+v.Sanitized() = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

// TestSanitizeNeverTouchesTheSwitch. Clamping a number must never be able to
// switch a feature on or off - that is the user's own decision and the one
// thing on this block that has no sensible fallback.
func TestSanitizeNeverTouchesTheSwitch(t *testing.T) {
	for _, on := range []bool{true, false} {
		if got := (LogFile{Enabled: on, MaxMB: -5, Keep: -5}).Sanitized(); got.Enabled != on {
			t.Errorf("Sanitized() changed Enabled from %v to %v while clamping two numbers", on, got.Enabled)
		}
	}
}

// TestMaxBytesConvertsOnce. The megabyte-to-byte conversion happens in one
// place so that a caller cannot hand internal/logring a figure in the wrong
// unit - a cap of 8 read as 8 bytes rotates on every single line.
func TestMaxBytesConvertsOnce(t *testing.T) {
	if got := (LogFile{MaxMB: 8}).MaxBytes(); got != 8<<20 {
		t.Errorf("MaxBytes() = %d, want %d", got, 8<<20)
	}
	// Sanitised on the way through, so an unsanitised document straight off
	// disk still hands the sink a usable cap rather than zero.
	if got := (LogFile{MaxMB: 0}).MaxBytes(); got != DefaultLogMaxMB<<20 {
		t.Errorf("MaxBytes() on a cleared size = %d, want the default in bytes", got)
	}
}
