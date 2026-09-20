package settings

// This instance's own identity: a stable random id that never changes once
// minted, an optional name that stands in for os.Hostname() wherever this
// instance names itself to another one (routes_pairing.go's pairingSelf), and
// the external hostnames it is known to be reachable through, either because a
// request arrived on one (routes_remote.go remembers it) or because somebody
// typed it on the Access tab before this instance was ever visited through it.
//
// Settings fields rather than something computed fresh: a domain seen once has
// to stay listed even when every later request arrives over the LAN IP, a name
// typed once should survive a restart, and an id has to be the same on every
// restart or nothing that learned it, a relay's group or a peer's
// federation.Instance.RelayID, could keep addressing this instance by it.

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// maxKnownDomains caps the remembered list so that a build behind a rotating
// set of throwaway subdomains, dynamic-DNS churn or a half-finished proxy
// config does not grow this file forever. The addresses that matter are the
// ones in current use, and eight is more than a single-instance setup needs.
const maxKnownDomains = 8

func sanitizeIdentity(n Settings) Settings {
	n.InstanceName = strings.TrimSpace(n.InstanceName)
	n.InstanceID = strings.TrimSpace(n.InstanceID)
	if n.InstanceID == "" {
		n.InstanceID = newInstanceID()
	}
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
	// No omitempty on KnownDomains, see CrawlInclude in settings.go: the key is
	// always present, so the frontend never has to treat it as optional.
	n.KnownDomains = out
	return n
}

// newInstanceID mints a fresh id the way routes_pairing.go's
// pairingCodes.issue() mints a token: 160 random bits, hex-encoded. Unlike
// InstanceName it carries no meaning anybody would choose, so there is nothing
// to validate about an existing one, only whether one exists.
func newInstanceID() string {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		// A broken machine, and there is no error return to propagate it
		// through. The blank id lands in the field sanitizeIdentity just set,
		// so the next load mints one again.
		return ""
	}
	return hex.EncodeToString(raw)
}
