package settings

// The way out of this machine: which connections downloads are spread across,
// how many sockets one download opens, and the reconnect that fetches a new
// public address. Two of the three carry secrets, which is why the redaction
// lives here as well - anything added to those two has to answer the same
// question first: does a browser ever need to see it?

import (
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
	// Cut to the rule engine's bound rather than to a number chosen here, because
	// the dispatcher cuts it there too: a page reading 32 while every download
	// opens 16 is a control that lies about what saving it did.
	//
	// Below zero is not "unlimited" the way the speed limit's zero is - it is
	// nonsense, and it is filed as the same "no opinion" a fresh install has
	// rather than refused, since nothing a user can type in a spinner should cost
	// them the rest of the page.
	if n.Chunks < 0 {
		n.Chunks = 0
	}
	if n.Chunks > rules.MaxChunks {
		n.Chunks = rules.MaxChunks
	}
	return n
}

// Redacted returns a copy safe to hand to a browser. Two secrets live in here
// now — the router password and every proxy password — and the endpoint that
// serves the settings must use nothing but this: the moment a client is shown
// them, the merge machinery in Set is protecting a value it already holds.
//
// The two packages disagree about how to hide a password, deliberately.
// reconnect masks it with a placeholder that WithSecretsFrom reads back, so an
// empty string can keep meaning "clear it"; proxycfg drops it and lets Merge put
// it back when the row still describes the same connection. Neither is wrapped
// or normalised here, because each is one half of a round trip its own package
// owns.
func (s Settings) Redacted() Settings {
	s.Reconnect = s.Reconnect.Redacted()
	if len(s.Connections) > 0 {
		out := make([]proxycfg.Entry, len(s.Connections))
		for i, e := range s.Connections {
			out[i] = e.Redacted()
		}
		s.Connections = out
	}
	return s
}
