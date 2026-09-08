package settings

// The way out of this machine: which connections downloads are spread across,
// how many sockets one download opens, and the reconnect that fetches a new
// public address. Two of the three carry secrets, which is why the redaction
// lives here as well - anything added to those two has to answer the same
// question first: does a browser ever need to see it?

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

// Redacted returns a copy safe to hand to a browser. Three secrets live in
// here now - the router password, every proxy password, and the end-of-queue
// command line - and the endpoint that serves the settings must use nothing
// but this: the moment a client is shown them, the merge machinery in Set is
// protecting a value it already holds.
//
// The packages disagree about how to hide a secret, deliberately. reconnect
// masks it with a placeholder that WithSecretsFrom reads back, so an empty
// string can keep meaning "clear it"; proxycfg drops it and lets Merge put it
// back when the row still describes the same connection. Neither is wrapped or
// normalised here, because each is one half of a round trip its own package
// owns.
//
// THE COMMAND LINE IS THE THIRD, and it is here rather than only in the
// diagnostics bundle on jdp's call. It follows reconnect's shape (placeholder
// plus WithSecretsFrom, merged back in Store.setLocked). The exposure it
// closes is the one that cannot be taken back: internal/api/routes_diagnostics.go
// puts THIS function's output straight into the bundle a user attaches to a
// public GitHub issue, and a command line is a secret store nobody declared -
// `wget --header=Authorization:\ Bearer\ abc123 http://nas/suspend` is the
// obvious first thing somebody writes there. The same document also refuses
// to carry paths for its own store, in so many words, because a desktop path
// contains a person's real name.
//
// ArchivePasswords is NOT redacted here and that is not an inconsistency: it
// is ordinary, visible config on the Archives page where the user is editing
// their own passwords and needs to see them, and routes_diagnostics.go clears
// it on its own side instead. The command line went the other way because it
// has a live route that answers "what would actually run" without printing it
// back into a document (POST /api/idle-action/check), so redacting it costs
// the page nothing it cannot get honestly.
// EVERY EVENT TARGET HEADER VALUE IS THE FOURTH, and it is the one where the
// browser is also the thing that types the destination - see notify.Merge,
// which is why the carry-back on save is bound to the address and not only to
// the row id. Redaction is deliberately whole-map rather than per-header: there
// is no way to look at a header called X-Anything and decide whether it holds a
// token without reading it, and a rule that guesses hands out a Matrix access
// token the first time somebody names their header something this file did not
// anticipate.
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
