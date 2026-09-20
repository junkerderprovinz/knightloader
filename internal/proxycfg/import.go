package proxycfg

// Reading a pasted proxy list: one proxy per line, in the only universal form
//
//	socks5://user:pass@host:port
//
// A line that cannot be read is refused by number with a reason, never
// skipped. The lines a parser would drop are the ones worth explaining: the
// other list format, a SOCKS4 line with a password, an unbracketed IPv6
// address.

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Rejection is one line that did not become an entry, and why. It carries the
// line number, not the text, because the text may hold a password in the clear
// and responses get logged and screenshotted; the client can find line n
// itself.
type Rejection struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Import is what a pasted list turned into. Both slices are never nil.
type Import struct {
	Entries  []Entry     `json:"entries"`
	Rejected []Rejection `json:"rejected"`
}

// ParseList reads a pasted proxy list. A line naming a connection already in
// existing, or earlier in the paste, is refused: the picker walks the list in
// order, so a duplicate takes twice its share of the queue. Nothing is saved;
// the caller shows the refusals before committing.
func ParseList(text string, existing []Entry) Import {
	out := Import{Entries: []Entry{}, Rejected: []Rejection{}}

	// Line 0 stands for "already in the stored list".
	claimed := make(map[string]int, len(existing))
	for _, e := range existing {
		claimed[connKey(e)] = 0
	}

	// \r\n is folded so the numbers match the client splitting its textarea
	// on \n.
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		n := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			// Blank lines and comments are normal in a hand-edited list.
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
// reached as the same user. Filter and cap are left out, since two rows that
// differ only there are still one proxy carrying two shares.
func connKey(e Entry) string {
	k, _ := kindOf(e.Kind)
	return string(k) + "\x00" + normalizeHost(e.Host) + "\x00" +
		strconv.Itoa(e.Port) + "\x00" + strings.TrimSpace(e.Username)
}

// ParseLine reads one line of a proxy list into an enabled entry. The error is
// meant for whoever pasted the line and says what to write instead where
// possible.
func ParseLine(line string) (Entry, error) {
	s := strings.TrimSpace(line)

	i := strings.Index(s, "://")
	if i < 0 {
		// Not guessed: a SOCKS5 proxy addressed as http fails every download,
		// and the error shows up on the hoster instead of the proxy.
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
		// clean would drop the password silently, leaving the user believing
		// the proxy has credentials it can never send.
		return Entry{}, fmt.Errorf("%s carries a user id and has no password field at all: "+
			"write %s://%s@%s:%d", kind, kind, user, host, port)
	}

	e := Entry{Kind: kind, Host: host, Port: port, Username: user, Password: pass, Enabled: true}
	// The save path's validator, so Sanitize cannot drop an accepted line.
	if err := Validate(e); err != nil {
		return Entry{}, err
	}
	return clean(e), nil
}

// importKind folds the scheme of a pasted line into a Kind. Only proxy kinds
// are accepted; "none" and "direct" are rows added by hand and would sit inert
// in an import.
func importKind(raw string) (Kind, error) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(raw))); k {
	case KindHTTP, KindHTTPS, KindSOCKS4, KindSOCKS4A, KindSOCKS5:
		return k, nil
	case "socks5h":
		// Every socks5 entry lets the proxy resolve the host anyway (see
		// Entry.scheme).
		return KindSOCKS5, nil
	case "socks":
		return "", errors.New(`"socks" is not a version: write socks4:// or socks5://`)
	case KindNone, KindDirect:
		return "", fmt.Errorf("%q is a row you add by hand, not a proxy an import can name", string(k))
	default:
		return "", fmt.Errorf("%q is not a connection type: use http, https, socks4, socks4a or socks5", string(k))
	}
}

// splitCredentials separates user:pass@ from the endpoint. The last @ is the
// separator, since passwords may contain one and hosts cannot; the first colon
// splits user from password, since passwords may contain colons. Nothing is
// percent-decoded: lists are literal text, and a literal % in a password is
// far more likely than an encoded one.
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

// splitEndpoint reads host:port and names the common mistakes separately,
// since each has its own fix.
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

// endpointProblem names the shapes SplitHostPort refuses. user and pass are
// needed to write out the corrected line for the list format that has a
// mechanical fix.
func endpointProblem(endpoint string, kind Kind, user, pass string) error {
	fields := strings.Split(endpoint, ":")
	// The host:port:user:pass format many marketplaces hand out, the most
	// common reason a paste fails.
	if len(fields) == 4 && isPort(fields[1]) && user == "" && pass == "" {
		return fmt.Errorf("this is the host:port:user:pass list format: write it as %s://%s:%s@%s:%s",
			kind, fields[2], fields[3], fields[0], fields[1])
	}
	if len(fields) == 1 {
		return fmt.Errorf("no port: write %s:PORT", endpoint)
	}
	// After the single-field case, so bare IPv4 is reported as a missing port.
	if net.ParseIP(endpoint) != nil {
		return fmt.Errorf("%q is an IPv6 address with no port; bracket it, as in [::1]:1080", endpoint)
	}
	return fmt.Errorf("%q is not host:port", endpoint)
}

func isPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}
