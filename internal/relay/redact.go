package relay

import (
	"io"
	"regexp"
)

// addrPattern matches the two shapes a client address takes in the standard
// library's server log lines: an IPv4 address, and an IPv6 address in
// brackets, each with or without a port. The bracketed form may carry a zone,
// which for a link-local peer is the interface name ([fe80::1%eth0]), so the
// zone is any text up to the bracket. Bare IPv6 without brackets is left
// alone on purpose: net/http always brackets it, and a looser pattern would
// eat timestamps like 12:00:00.
var addrPattern = regexp.MustCompile(`\[[0-9A-Fa-f:.]+(?:%[^\]\s]+)?\](?::\d+)?|\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`)

// RedactAddrs wraps w so that client IP addresses never reach it.
//
// The relay writes no log of its own about who connects, but the standard
// library's http.Server does, through its ErrorLog: every failed or abandoned
// TLS handshake, some HTTP/2 protocol errors and any handler panic are logged
// with the client's address. Left at the default, that put the IP addresses of
// scanners and of ordinary users (a popup closed mid-handshake is enough) into
// the system journal with no retention, while the privacy policy said the
// relay keeps no record.
//
// Redacted rather than discarded: autocert reports a failed certificate
// issuance or renewal only as one of these handshake-error lines, and throwing
// the log away would hide that until the certificate expired.
func RedactAddrs(w io.Writer) io.Writer {
	return redactWriter{w: w}
}

type redactWriter struct{ w io.Writer }

// Write redacts and forwards one write. log.Logger hands over a whole line per
// call, so an address is never split across two writes. The byte count
// returned is the caller's, not the shorter or longer redacted one, which is
// what io.Writer promises the caller.
func (r redactWriter) Write(p []byte) (int, error) {
	if _, err := r.w.Write(addrPattern.ReplaceAll(p, []byte("[address]"))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// SweepLimiter drops the rate-limit records that have run out. The request
// path already does this at most once a minute; the public relay also calls it
// on a timer, so a relay that goes quiet forgets addresses as well.
func (s *Server) SweepLimiter() {
	s.limiter.sweep()
}
