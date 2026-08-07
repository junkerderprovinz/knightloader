package proxycfg

// Reading a pasted proxy list.
//
// A bought or scraped proxy list arrives as text, one proxy per line, and the
// only universally written form is
//
//	socks5://user:pass@host:port
//
// The rule this file is built around: a line that cannot be read is REFUSED BY
// NUMBER WITH A REASON, never quietly skipped. A parser that drops what it does
// not understand turns "I pasted forty proxies and got thirty-one" into a
// silent, unattributable loss, and the seven that went missing are exactly the
// ones written in a shape worth telling the user about — the wrong list format,
// a SOCKS4 line carrying a password the protocol cannot send, an IPv6 address
// with no brackets.

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Rejection is one line that did not become an entry, and why.
//
// It carries the line NUMBER and not the line. A rejected line is the one place
// a password can still be sitting in plain text, and a response body is logged,
// screenshotted and pasted into bug reports; the client already holds what the
// user typed and can point at line n itself.
type Rejection struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Import is what a pasted list turned into. Both halves are always present and
// never nil, so a client can count them without checking for null first.
type Import struct {
	Entries  []Entry     `json:"entries"`
	Rejected []Rejection `json:"rejected"`
}

// ParseList reads a pasted proxy list.
//
// existing is the list already configured, and it is consulted for one thing
// only: a line naming a connection that is already there is refused rather than
// added twice. A duplicate is not harmless — the picker walks the list in order
// and a connection appearing twice takes twice its share of the queue, which
// looks like the round-robin being broken rather than like the list being wrong.
//
// Nothing is saved here. The caller gets the rows and decides, because the whole
// point of naming the refusals is that somebody reads them before committing.
func ParseList(text string, existing []Entry) Import {
	out := Import{Entries: []Entry{}, Rejected: []Rejection{}}

	// Line 0 stands for "already in the stored list", which reads differently
	// from "line 4 already added this" and has a different fix.
	claimed := make(map[string]int, len(existing))
	for _, e := range existing {
		claimed[connKey(e)] = 0
	}

	// \r\n is folded rather than trimmed per line so the numbers here index the
	// same lines the client gets from splitting its textarea on \n. An off-by-one
	// between the two points the user at the wrong line, which is worse than not
	// pointing at all.
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		n := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			// Blank lines and comments are not failures: every list that was ever
			// edited by hand has both, and reporting them would bury the real
			// refusals under noise.
			continue
		}
		e, err := ParseLine(line)
		if err != nil {
			out.Rejected = append(out.Rejected, Rejection{Line: n, Reason: err.Error()})
			continue
		}
		key := connKey(e)
		if at, dup := claimed[key]; dup {
			out.Rejected = append(out.Rejected, Rejection{Line: n, Reason: duplicateReason(at)})
			continue
		}
		claimed[key] = n
		out.Entries = append(out.Entries, e)
	}
	return out
}

func duplicateReason(at int) string {
	if at == 0 {
		return "this connection is already in the list"
	}
	return fmt.Sprintf("line %d already added this connection", at)
}

// connKey identifies a connection for the duplicate check: the same endpoint
// reached as the same user. The filter and the cap are deliberately not part of
// it — two rows that differ only in which hosts they claim are still one proxy
// being asked to carry two shares of the queue.
func connKey(e Entry) string {
	k, _ := kindOf(e.Kind)
	return string(k) + "\x00" + normalizeHost(e.Host) + "\x00" +
		strconv.Itoa(e.Port) + "\x00" + strings.TrimSpace(e.Username)
}

// ParseLine reads one line of a proxy list into an entry, enabled and ready to
// be added. The returned error is written to be shown to whoever pasted the
// line: it says what is wrong and, wherever there is one, what to write instead.
func ParseLine(line string) (Entry, error) {
	s := strings.TrimSpace(line)

	i := strings.Index(s, "://")
	if i < 0 {
		// Deliberately not guessed. A SOCKS5 proxy addressed as http fails every
		// download it is given, and the failure surfaces on the hoster rather than
		// on the proxy, so the user spends the evening on the wrong page.
		return Entry{}, errors.New("no connection type: write the line as http://user:pass@host:port, " +
			"and socks5:// or socks4:// for a SOCKS proxy")
	}
	kind, err := importKind(s[:i])
	if err != nil {
		return Entry{}, err
	}

	rest := strings.TrimSuffix(s[i+3:], "/")
	if j := strings.Index(rest, "/"); j >= 0 {
		return Entry{}, fmt.Errorf("a proxy is a host and a port, not an address: remove %q from the end", rest[j:])
	}

	user, pass, endpoint, err := splitCredentials(rest)
	if err != nil {
		return Entry{}, err
	}
	host, port, err := splitEndpoint(endpoint, kind, user, pass)
	if err != nil {
		return Entry{}, err
	}

	if pass != "" && (kind == KindSOCKS4 || kind == KindSOCKS4A) {
		// clean would drop the password silently, which is the right thing to
		// persist and the wrong thing to do to a line somebody just pasted: they
		// would go on believing the proxy has credentials it can never send.
		return Entry{}, fmt.Errorf("%s carries a user id and has no password field at all: "+
			"write %s://%s@%s:%d", kind, kind, user, host, port)
	}

	e := Entry{Kind: kind, Host: host, Port: port, Username: user, Password: pass, Enabled: true}
	// The same validator the save path runs, so a line accepted here cannot be
	// dropped by Sanitize afterwards.
	if err := Validate(e); err != nil {
		return Entry{}, err
	}
	return clean(e), nil
}

// importKind folds the scheme of a pasted line into a Kind.
//
// Only the kinds that name a proxy are accepted. "none" and "direct" are rows a
// user adds by hand to say something about their own setup; a list of proxies
// cannot contain either, and reading one would put an inert row into the middle
// of somebody's import with nothing to say why it does nothing.
func importKind(raw string) (Kind, error) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(raw))); k {
	case KindHTTP, KindHTTPS, KindSOCKS4, KindSOCKS4A, KindSOCKS5:
		return k, nil
	case "socks5h":
		// The h spells out that the proxy resolves the host name. That is what
		// this app does with every socks5 entry anyway (see Entry.scheme), so the
		// spelling is folded rather than refused: it is the same proxy.
		return KindSOCKS5, nil
	case "socks":
		return "", errors.New(`"socks" is not a version: write socks4:// or socks5://`)
	case KindNone, KindDirect:
		return "", fmt.Errorf("%q is a row you add by hand, not a proxy an import can name", string(k))
	default:
		return "", fmt.Errorf("%q is not a connection type: use http, https, socks4, socks4a or socks5", string(k))
	}
}

// splitCredentials separates user:pass@ from the endpoint.
//
// The LAST @ is the separator, because a password containing one is ordinary
// and a host name containing one is impossible. The first : inside the
// credentials is the separator for the same reason in reverse: a password may
// contain colons, a user name of these protocols may not usefully.
//
// Nothing is percent-decoded. Proxy lists are handed out as literal text, and a
// password containing a % is far more likely than one that was deliberately
// encoded — decoding would corrupt the first and only occasionally repair the
// second.
func splitCredentials(rest string) (user, pass, endpoint string, err error) {
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "", "", rest, nil
	}
	creds := rest[:at]
	endpoint = rest[at+1:]
	if creds == "" {
		return "", "", "", errors.New("no user name before the @: drop the @ if this proxy needs no credentials")
	}
	if c := strings.Index(creds, ":"); c >= 0 {
		user, pass = creds[:c], creds[c+1:]
	} else {
		user = creds
	}
	if user == "" {
		return "", "", "", errors.New("a password with no user name cannot be sent: write user:pass@host:port")
	}
	return user, pass, endpoint, nil
}

// splitEndpoint reads host:port, and spends its length on the three ways it is
// written wrongly, because each one has a different fix and a bare "not
// host:port" sends the user back to guessing.
func splitEndpoint(endpoint string, kind Kind, user, pass string) (string, int, error) {
	if endpoint == "" {
		return "", 0, errors.New("no host")
	}
	host, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, endpointProblem(endpoint, kind, user, pass)
	}
	port, convErr := strconv.Atoi(rawPort)
	if convErr != nil {
		return "", 0, fmt.Errorf("port %q is not a number", rawPort)
	}
	return host, port, nil
}

// endpointProblem names the shapes SplitHostPort refuses, in the order they turn
// up, because each has its own fix. user and pass are wanted only to write the
// corrected line out in full for the one that has a mechanical fix.
func endpointProblem(endpoint string, kind Kind, user, pass string) error {
	fields := strings.Split(endpoint, ":")
	// The other list format in circulation, and by some distance the most common
	// reason a paste fails: whole marketplaces hand out host:port:user:pass.
	if len(fields) == 4 && isPort(fields[1]) && user == "" && pass == "" {
		return fmt.Errorf("this is the host:port:user:pass list format: write it as %s://%s:%s@%s:%s",
			kind, fields[2], fields[3], fields[0], fields[1])
	}
	if len(fields) == 1 {
		return fmt.Errorf("no port: write %s:PORT", endpoint)
	}
	// Checked after the single-field case, so a bare IPv4 is reported as a
	// missing port rather than sent looking for brackets it does not need.
	if net.ParseIP(endpoint) != nil {
		return fmt.Errorf("%q is an IPv6 address with no port; bracket it, as in [::1]:1080", endpoint)
	}
	return fmt.Errorf("%q is not host:port", endpoint)
}

func isPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}
