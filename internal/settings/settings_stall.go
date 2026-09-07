package settings

// Standing still: how long a running download may move no bytes before the
// list is allowed to say so, and what happens to it once it does.

// MinStallTimeout is the shortest stall timeout that can be configured, in
// seconds.
//
// A healthy download stops moving bytes all the time: a hoster's free-user
// countdown, a chunk handover, a reconnect, a backend that has just been asked
// for a fresh link. Under a minute those all look identical to a dead
// connection, so a ten-second timeout would not detect stalls, it would mark
// the queue. The floor is applied rather than the value refused, because a
// number typed into a spinner is somebody asking for the smallest useful
// answer, not somebody making a mistake worth an error message.
const MinStallTimeout = 60

// maxStallTimeout is a day, in seconds. Anything past it is a typo: a
// connection that has moved nothing for twenty-four hours needed saying much
// earlier, and a timeout of a year is a switch that reads as on and is off.
const maxStallTimeout = 24 * 60 * 60

// DefaultStallRestarts is how many automatic restarts one task gets when the
// restart is switched on and no cap was typed.
//
// It is deliberately NOT unlimited. A transfer that stands still four times in
// a row is being refused, not unlucky, and an uncapped restart loop is the
// same hammering the retry backoff exists to prevent - only worse, because
// every restart throws away the bytes the previous attempt did fetch.
const DefaultStallRestarts = 3

// maxStallRestarts matches the ceiling sanitizeQueue puts on MaxRetries, for
// the same reason: this is a count of attempts against somebody else's server.
const maxStallRestarts = 20

func sanitizeStall(n Settings) Settings {
	if n.StallTimeout < 0 {
		n.StallTimeout = 0
	}
	if n.StallTimeout > 0 && n.StallTimeout < MinStallTimeout {
		n.StallTimeout = MinStallTimeout
	}
	if n.StallTimeout > maxStallTimeout {
		n.StallTimeout = maxStallTimeout
	}
	if n.StallMaxRestarts < 0 {
		n.StallMaxRestarts = 0
	}
	if n.StallMaxRestarts > maxStallRestarts {
		n.StallMaxRestarts = maxStallRestarts
	}
	return n
}
