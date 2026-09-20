package settings

// The optional copy of this process's own log output on disk: whether it is
// written at all, how big one file may get, and how many renamed ones stay
// beside it. The field itself is documented on the Settings struct; what lives
// here is the type, the defaults, the clamps, and the two decisions that are
// not obvious from the field names.
//
// There is no path field. routes_features.go reflects over the whole Settings
// struct to build the Advanced key table, so every string field on it appears
// there as a free text box with no validation of its own. A log path typed into
// that box that does not exist, or that the account inside the container may
// not write, would stop the log with nothing on screen connecting the two. The
// directory is fixed at <dataDir>/logs, and KL_LOG_DIR, read at boot the way
// KL_DATA, KL_JD and KL_CNL are, puts it on another disk. An environment
// variable is set once by somebody standing at the machine, and a wrong one is
// visible in the container's configuration rather than in a settings document.
//
// The cleaning is a method on the type rather than the sanitizeX(Settings)
// shape its neighbours use, so the whole of this feature's settings logic sits
// in one file that compiles on its own. It costs sanitize() the same line.

// LogFile is the optional copy of this process's own log output on disk.
//
// Every field is inert while Enabled is false, which is what an install that
// upgrades into this key gets: settings.Load unmarshals over Defaults, so a
// document written before the key existed reads as off. On an Unraid box
// appdata is usually on the array, and an unbuffered write per log line keeps a
// spinning disk awake all night.
//
// It is unbuffered per record once it is on: buffering loses the last lines
// before a crash, which are the lines this exists to keep.
type LogFile struct {
	// Enabled is the whole switch. False ships, and false is what an upgrade
	// reads out of a settings.json written before this key existed.
	Enabled bool `json:"enabled"`

	// MaxMB is how large the file being written may get before it is renamed
	// and a fresh one started.
	MaxMB int `json:"maxMb"`

	// Keep is how many renamed files stay beside the one being written.
	//
	// Zero is a real answer rather than off: keep only the file currently being
	// written, which is what somebody with a small volume wants. Sanitized
	// therefore clamps this to 0..20 rather than flooring it at 1, since a
	// floor would hand back a generation the user gave up, and switching the
	// log off is the control one field up.
	Keep int `json:"keep"`
}

const (
	// DefaultLogMaxMB is eight megabytes, which is a good deal more than a day
	// of an ordinary instance's own chatter and small enough that four
	// generations of it disappear next to a single episode of anything.
	DefaultLogMaxMB = 8

	// DefaultLogKeep is three renamed files beside the live one, so that "what
	// did it say the night before last" has an answer without anybody having
	// had to plan ahead for the question.
	DefaultLogKeep = 3

	// maxLogMB is a typo guard rather than a policy. A gigabyte in one file is
	// already past what any editor opens comfortably, and the likeliest way to
	// end up above it is somebody typing a byte count into a box labelled MB.
	maxLogMB = 1024

	// maxLogKeep bounds what the whole feature can occupy at
	// maxLogMB * (maxLogKeep + 1). Twenty generations is an archive; past it is
	// a typo that fills a volume.
	maxLogKeep = 20
)

// DefaultLogFile is what a fresh install starts with, and what an install that
// upgrades into this key keeps.
//
// Written out rather than left at the zero value for the reason Defaults()
// gives for ReclaimTrust and VolumeCapResetDay: the Advanced key table serves
// Defaults() unsanitised, so a factory reading of "0 MB, keep 0" that exists
// only because nobody typed anything cannot be told there from a field somebody
// forgot to fill in.
func DefaultLogFile() LogFile {
	return LogFile{MaxMB: DefaultLogMaxMB, Keep: DefaultLogKeep}
}

// Sanitized clamps, and never refuses.
//
// A figure this cannot use is read as the absence of one rather than as an
// error to dismiss before the rest of the edits will save, the posture
// sanitizeDiskSpace takes for a byte count and sanitizeQuiet for a slot count.
// Zero and negative MaxMB both come back as the default rather than as a floor
// of 1: a cleared box means no number was typed, and a one-megabyte file that
// rotates every few minutes is not what clearing it meant. Keep has no such
// reading, because zero there is a number somebody can mean.
func (l LogFile) Sanitized() LogFile {
	if l.MaxMB <= 0 {
		l.MaxMB = DefaultLogMaxMB
	}
	if l.MaxMB > maxLogMB {
		l.MaxMB = maxLogMB
	}
	if l.Keep < 0 {
		l.Keep = 0
	}
	if l.Keep > maxLogKeep {
		l.Keep = maxLogKeep
	}
	return l
}

// MaxBytes is MaxMB as the byte count the sink actually compares against, so
// that the conversion happens in one place rather than at every call site that
// wants to hand the figure to internal/logring.
func (l LogFile) MaxBytes() int64 {
	return int64(l.Sanitized().MaxMB) << 20
}
