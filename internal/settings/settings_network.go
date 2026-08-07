package settings

// The two field groups that carry secrets — outbound connections and reconnect —
// and the redaction that keeps those secrets off the wire. Anything added here
// has to answer the same question first: does a browser ever need to see it?

import (
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

func sanitizeNetwork(n Settings) Settings {
	// Sanitize assigns stable IDs, compacts the order and drops rows that could
	// never be used. A half-configured proxy row kept and enabled would either
	// fail every download routed through it or be read as no proxy at all, and
	// send the traffic the user was hiding out over their own connection.
	n.Connections = proxycfg.Sanitize(n.Connections)
	n.Reconnect = reconnect.Sanitize(n.Reconnect)
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
