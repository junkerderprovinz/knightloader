package settings

// This instance's own identity: an optional human-chosen name that stands
// in for os.Hostname() wherever this instance names itself to another one
// (see internal/api/routes_pairing.go's pairingSelf), and the external
// hostnames it is known to be reachable through - either because a request
// actually arrived on one (internal/api/routes_remote.go remembers it the
// moment that happens) or because someone typed it in by hand on the Access
// tab, for the case where a domain is configured but this instance has
// never actually been visited through it yet.
//
// Deliberately settings fields, not something computed fresh every time: a
// domain seen once has to stay listed even when every later request comes
// in over the LAN IP instead, and a name typed once should not have to be
// retyped after every restart.

import "strings"

// maxKnownDomains caps the remembered list so a build behind a rotating set
// of throwaway subdomains (a dynamic-DNS churn, a proxy config left
// half-finished) does not grow this file forever - the addresses that
// matter are the ones actually in current use, and eight is generously more
// than any real single-instance setup needs at once.
const maxKnownDomains = 8

func sanitizeIdentity(n Settings) Settings {
	n.InstanceName = strings.TrimSpace(n.InstanceName)

	seen := make(map[string]bool, len(n.KnownDomains))
	out := make([]string, 0, len(n.KnownDomains))
	for _, d := range n.KnownDomains {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
		if len(out) >= maxKnownDomains {
			break
		}
	}
	// No omitempty on KnownDomains (see CaptchaSolverOrder's own comment in
	// settings.go for why): a nil slice still has to serialise as `[]`/JSON
	// null consistently, never be silently absent, so the frontend's own
	// type never has to treat this field as optional.
	n.KnownDomains = out
	return n
}
