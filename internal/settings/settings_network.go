package settings

// The way out of this machine: which connections downloads are spread across,
// how many sockets one download opens, and the reconnect that fetches a new
// public address. Two of the three carry secrets, which is why the redaction
// lives here as well. Anything added to those two answers the same question
// first: does a browser ever need to see it?

import (
	"github.com/junkerderprovinz/knightloader/internal/notify"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

func sanitizeNetwork(n Settings) Settings {
	// Sanitize assigns stable IDs, compacts the order and drops rows that could
	// never be used. A half-configured proxy row kept and enabled would either
	// fail every download routed through it or be read as no proxy at all, and
	// send the traffic the user was hiding out over their own connection.
	n.Connections = proxycfg.Sanitize(n.Connections)
	n.Reconnect = reconnect.Sanitize(n.Reconnect)
	// Cut to the rule engine's bound rather than to a number chosen here,
	// because the dispatcher cuts it there too: a page reading 32 while every
	// download opens 16 is a control that lies about what saving it did.
	//
	// Below zero is not "unlimited" the way the speed limit's zero is. It is
	// filed as the same "no opinion" a fresh install has rather than refused,
	// since nothing typed in a spinner should cost somebody the rest of the
	// page.
	if n.Chunks < 0 {
		n.Chunks = 0
	}
	if n.Chunks > rules.MaxChunks {
		n.Chunks = rules.MaxChunks
	}
	return n
}

// Redacted returns a copy safe to hand to a browser. Four secrets live in here:
// the router password, every proxy password, the end-of-queue command line and
// every event target header value. The endpoint that serves the settings uses
// nothing but this, because the moment a client is shown one of them the merge
// machinery in Set is protecting a value the client already holds.
//
// The packages disagree about how to hide a secret, and neither is wrapped or
// normalised here, because each is one half of a round trip its own package
// owns. reconnect masks it with a placeholder that WithSecretsFrom reads back,
// so an empty string keeps meaning "clear it"; proxycfg drops it and lets Merge
// put it back when the row still describes the same connection.
//
// The command line follows reconnect's shape and is merged back in
// Store.setLocked. It is redacted because routes_diagnostics.go puts this
// function's output into the bundle people attach to public GitHub issues, and
// a command line is an undeclared secret store: `wget
// --header=Authorization:\ Bearer\ abc123 http://nas/suspend`.
//
// ArchivePasswords is not redacted here: it is ordinary visible config on the
// Archives page, where somebody is editing their own passwords, and
// routes_diagnostics.go clears it on its own side.
//
// Event target headers are redacted whole-map rather than per header. There is
// no way to look at a header called X-Anything and decide whether it holds a
// token without reading it, and a rule that guesses hands out a Matrix access
// token the first time somebody names a header something unforeseen. The
// browser also types the destination, which is why notify.Merge binds the
// carry-back on save to the address and not only to the row id.
func (s Settings) Redacted() Settings {
	s.Reconnect = s.Reconnect.Redacted()
	s.IdleAction = s.IdleAction.Redacted()
	if len(s.Connections) > 0 {
		out := make([]proxycfg.Entry, len(s.Connections))
		for i, e := range s.Connections {
			out[i] = e.Redacted()
		}
		s.Connections = out
	}
	if len(s.EventTargets) > 0 {
		out := make([]notify.Target, len(s.EventTargets))
		for i, t := range s.EventTargets {
			out[i] = notify.Redacted(t)
		}
		s.EventTargets = out
	}
	return s
}
