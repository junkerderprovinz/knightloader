package settings

// The optional copy of this process's own log output on disk: whether it is
// written at all, how big one file may get, and how many renamed ones stay
// beside it. The field itself is documented on the Settings struct; what lives
// here is the type, the defaults, the clamps, and the two decisions that are
// not obvious from the field names.
//
// THERE IS NO PATH FIELD, AND THAT IS THE DESIGN. routes_features.go reflects
// over the whole Settings struct to build the Advanced key table, so every
// string field on it appears there as a free text box with no validation of its
// own. A log path typed into that box that does not exist, or that the account
// inside the container may not write, would stop the log with nothing anywhere
// on screen connecting the two - the same failure sanitizeStaging already
// refuses to ship for a relative WorkDir, one file over. The directory is
// therefore fixed at <dataDir>/logs, and the escape hatch for "put it on
// another disk" is the KL_LOG_DIR environment variable read at boot, exactly
// the way KL_DATA, KL_JD and KL_CNL already are. An environment variable is set
// once by somebody standing at the machine, and a wrong one is visible in the
// container's own configuration rather than buried in a settings document.
//
// WHY THE CLEANING IS A METHOD ON THE TYPE and not the sanitizeX(Settings)
// Settings shape its neighbours in this package use: this file is written in a
// wave where settings.go itself belongs to somebody else, and a method on
// LogFile is the whole of this feature's settings logic in one file that
// compiles on its own. The single line it costs sanitize() is the same single
// line the sanitizeX form costs.

// LogFile is the optional copy of this process's own log output on disk.
//
// EVERY FIELD IS INERT WHILE Enabled IS FALSE, which is what every install that
// upgrades into this key gets: settings.Load unmarshals over Defaults, so a
// document written before this key existed reads as "off" and the instance
// behaves exactly as it did. An update must not start writing files somebody
// did not ask for - on an Unraid box appdata is usually on the array, and an
// unbuffered write per log line keeps a spinning disk awake all night.
//
// Unbuffered per record anyway, once it IS on, and that is not an oversight:
// buffering loses exactly the last lines before a crash, and those are the
// lines this whole feature exists to preserve.
type LogFile struct {
	// Enabled is the whole switch. False ships, and false is what an upgrade
	// reads out of a settings.json written before this key existed.
	Enabled bool `json:"enabled"`

	// MaxMB is how large the file being written may get before it is renamed
	// and a fresh one started.
	MaxMB int `json:"maxMb"`

	// Keep is how many renamed files stay beside the one being written.
	//
	// ZERO IS A REAL ANSWER and not "off": it means keep only the file
	// currently being written, which is what somebody with a small volume
	// actually wants. That is why Sanitized clamps this to 0..20 rather than
	// flooring it at 1 - a floor would silently hand back a generation the
	// user deliberately gave up, and switching the log off entirely is a
	// different control one field up.
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
	// maxLogMB * (maxLogKeep + 1). Twenty generations is already a deliberate
	// archive; anything past it is a typo that fills a volume.
	maxLogKeep = 20
)

// DefaultLogFile is what a fresh install starts with, and what every install
// that upgrades into this key keeps.
//
// Written out rather than left at the zero value for the reason Defaults()
// already gives for ReclaimTrust and VolumeCapResetDay: the Advanced key table
// serves Defaults() UNSANITISED, so a factory reading of "0 MB, keep 0" that
// exists only because nobody typed anything is indistinguishable there from a
// field somebody forgot to fill in. Off with two real numbers behind it reads
// as the decision it is.
func DefaultLogFile() LogFile {
	return LogFile{MaxMB: DefaultLogMaxMB, Keep: DefaultLogKeep}
}

// Sanitized clamps, and never refuses.
//
// A figure this cannot use is read as the absence of one rather than as an
// error the user has to dismiss before the rest of their edits will save - the
// same posture sanitizeDiskSpace takes for a byte count and sanitizeQuiet takes
// for a slot count. Zero and negative MaxMB both come back as the DEFAULT
// rather than as the floor of 1: a cleared box means "I did not type a number",
// and a one-megabyte file that rotates every few minutes is not what anybody
// meant by clearing it. Keep has no such reading, because zero there is a
// number somebody can mean.
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
