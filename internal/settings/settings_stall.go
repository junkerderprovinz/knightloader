package settings

// Standing still: how long a running download may move no bytes before the
// list is allowed to say so, and what happens to it once it does.

import "encoding/json"

// DefaultStallTimeout is the stall timeout an install starts with, in seconds.
//
// Two minutes is past the floor with room to spare, so a chunk handover or a
// reconnect is not marked, and short enough that a hung transfer is picked up
// again while the queue is still being watched. It is on by default because
// what follows the mark by default is a reconnect, which costs the transfer
// nothing it has fetched.
const DefaultStallTimeout = 120

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
// Not unlimited: a transfer that stands still four times in a row is being
// refused rather than unlucky, and an uncapped restart loop is the hammering
// the retry backoff exists to prevent, worse because every restart throws away
// the bytes the previous attempt fetched.
const DefaultStallRestarts = 3

// maxStallRestarts matches the ceiling sanitizeQueue puts on MaxRetries, for
// the same reason: this is a count of attempts against somebody else's server.
const maxStallRestarts = 20

// migrateStall switches the stall watcher on for an install that stored the
// old default. It runs on the raw bytes at load, like migrateAutoStart.
//
// A 0 written by an earlier build is the default that build shipped with,
// when the watcher could only mark a row or throw its bytes away. It says
// nothing about what the install wants from a watcher that reconnects without
// losing anything, so it becomes DefaultStallTimeout. The absence of
// stallReconnect, a key no earlier build wrote, is what marks such a
// document. Once it is present, even false, a 0 was chosen with the reconnect
// on offer and is left alone, or switching the watcher off would not survive
// the next load.
func migrateStall(raw []byte, n Settings) Settings {
	var old struct {
		StallTimeout   *int  `json:"stallTimeout"`
		StallReconnect *bool `json:"stallReconnect"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return n
	}
	if old.StallReconnect != nil || old.StallTimeout == nil || *old.StallTimeout != 0 {
		return n
	}
	n.StallTimeout = DefaultStallTimeout
	return n
}

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
