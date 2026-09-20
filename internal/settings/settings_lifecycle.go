package settings

// What a restart does and what the list keeps: the two questions nobody asks
// until the answer has already cost them something.

import "strings"

// What the app does on boot with the downloads that were in flight when the
// process last stopped. Anything else falls back to ResumeNever, because an
// unrecognised value must never be read as "start downloading".
const (
	// ResumeNever leaves everything that was in flight paused, and is the
	// default, because a restarted transfer starts from the beginning. No
	// backend's handle on a running download survives this process, so a
	// resumed task is fetched afresh, and since the partial is still at the
	// destination the collision policy applies to it: under the default,
	// rename, the bytes land beside it in a second file. Unattended, on a box
	// that reboots at four in the morning, that spends a metered line on bytes
	// somebody already had and leaves the half file behind.
	ResumeNever = "never"
	// ResumeRunning starts again what was actually running, and only if
	// something was: a queue that was already idle or halted stays that way.
	// This is JDownloader's default and the option most people mean.
	ResumeRunning = "running"
	// ResumeAll starts everything that had not finished, whether it was running
	// or still waiting for a slot.
	ResumeAll = "all"
)

// ResumeModes lists the three in the order an interface should offer them,
// cautious first. Built fresh per call so a caller cannot reorder the menu for
// everybody else.
func ResumeModes() []string { return []string{ResumeNever, ResumeRunning, ResumeAll} }

// DefaultKeepFinishedDays is how long a finished download stays in the list.
//
// A month rather than forever, because the list otherwise only grows and the
// ten thousandth row is what makes the table take a second to sort. Nothing is
// lost: what was downloaded stays in the history table, which retention never
// touches, and the file on disk is not in question here.
const DefaultKeepFinishedDays = 30

// DefaultHistoryMax is how many finished downloads the history keeps. Roughly a
// year of heavy use, and small enough that the table stays a few megabytes on a
// database that sits on the same disk the downloads land on.
const DefaultHistoryMax = 10000

// maxKeepFinishedDays is ten years, which is not a real retention period but a
// guard: the value is turned into a cutoff instant, and a number large enough to
// overflow that arithmetic would produce a cutoff in the past and delete the
// list it was meant to protect.
const maxKeepFinishedDays = 3650

func sanitizeLifecycle(n Settings) Settings {
	switch n.ResumeOnStart {
	case ResumeNever, ResumeRunning, ResumeAll:
	default:
		n.ResumeOnStart = ResumeNever
	}
	// Zero is a real answer for both of these, keep forever, so only a negative
	// one is corrected. A negative retention would be a cutoff in the future,
	// which is every finished download at once.
	if n.KeepFinishedDays < 0 {
		n.KeepFinishedDays = 0
	}
	if n.KeepFinishedDays > maxKeepFinishedDays {
		n.KeepFinishedDays = maxKeepFinishedDays
	}
	if n.HistoryMax < 0 {
		n.HistoryMax = 0
	}
	return n
}

// ParseResumeOnStart reads a stored value as one of the three modes, folding
// anything it does not recognise onto ResumeNever. It exists so the app has one
// answer to "what does this string mean" rather than a switch of its own that
// can disagree with the sanitiser.
func ParseResumeOnStart(s string) string {
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case ResumeRunning, ResumeAll:
		return v
	}
	return ResumeNever
}
