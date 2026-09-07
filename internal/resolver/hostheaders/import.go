package hostheaders

// import.go: the paste box.
//
// Nobody types a session cookie. It arrives on the clipboard, and it arrives
// in one of exactly three shapes, because those are the three a browser's
// developer tools and the surrounding folklore produce:
//
//   - a curl command line, from "Copy as cURL" in Chrome, Firefox and Edge -
//     the richest of the three, because it carries the URL as well, so the
//     origin the headers are scoped to does not have to be typed a second
//     time;
//   - a header block, from "Copy request headers", which on HTTP/2 arrives
//     with :authority and :scheme pseudo-headers that name the origin just as
//     well;
//   - a bare cookie block, "a=1; b=2", from document.cookie or from the
//     Cookie row of the same panel, which names nothing but the cookies.
//
// Parse takes all three and does not ask which one it is being given. A person
// pasting a login into a download manager has already done the hard part; a
// format picker above the box is one more thing to get wrong, and getting it
// wrong produces a profile that silently never matches.

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// dropped names the headers a pasted block must never contribute, whatever it
// says.
//
// Range and Accept-Encoding are the two that matter and they are not about
// secrecy at all: the engine downloads with several connections, which it does
// by issuing its own Range requests, and a Range copied out of a browser's
// media request would pin every chunk of every download to the same few bytes.
// Accept-Encoding the same way round - a compressed transfer defeats the byte
// counting the whole progress display is built on.
//
// Host, Content-Length, Connection and Transfer-Encoding are refused because
// they describe the request the download library is about to build rather than
// the credential it should carry, and a paste that overrides them produces a
// request the server answers with a 400 far away from the box the text was
// pasted into. Host doubles as a way to point one origin's credential at
// another server's socket, which is the thing this package exists to prevent.
var dropped = map[string]bool{
	"Host":              true,
	"Content-Length":    true,
	"Connection":        true,
	"Transfer-Encoding": true,
	"Range":             true,
	"Accept-Encoding":   true,
	"Te":                true,
	"Upgrade":           true,
	"Expect":            true,
}

// Parse reads a pasted cookie block, header block or curl command line into a
// Set. The Origin is filled in when the paste names one and left empty when it
// does not, which the caller (Store.Import) reads as "use the URL the user
// typed beside the box".
//
// The result is NOT normalised: Normalize is the caller's step, so that the
// origin from the paste and the origin from the form go through the same
// single check rather than two that can drift.
func Parse(text string) (Set, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Set{}, fmt.Errorf("hostheaders: nothing to import")
	}
	if isCurl(trimmed) {
		return parseCurl(trimmed)
	}
	return parseBlock(trimmed)
}

// isCurl looks for the command word at the start of the paste rather than for
// a -H anywhere in it. A header block whose first header happened to be named
// "Curl-Something" is not a command line, and a cookie value can contain any
// text at all.
func isCurl(s string) bool {
	first := s
	if i := strings.IndexAny(first, " \t\r\n"); i >= 0 {
		first = first[:i]
	}
	first = strings.ToLower(strings.Trim(first, "\"'"))
	// "curl.exe" is what a Windows "Copy as cURL (cmd)" paste starts with.
	return first == "curl" || first == "curl.exe" || strings.HasSuffix(first, "/curl")
}

// parseCurl reads the flags this package can act on and ignores every other
// one. Ignoring rather than refusing is deliberate: a real "Copy as cURL"
// paste is full of --compressed, --insecure, -X POST and a body, none of which
// says anything about the credential, and refusing the paste over one of them
// would send the user back to hand-editing a shell command.
func parseCurl(text string) (Set, error) {
	args := shellSplit(text)
	var (
		set     Set
		cookies []string
		user    string
	)
	// The index is declared outside the loop and advanced by next(), because a
	// flag and its value are one step: -H and the header it carries must not
	// be examined twice, and the value must never be read as the URL.
	i := 0
	next := func() string {
		if i+1 < len(args) {
			i++
			return args[i]
		}
		return ""
	}
	for ; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-H" || arg == "--header":
			name, value, ok := splitHeader(next())
			switch {
			case !ok || dropped[name]:
			case name == "Cookie":
				// Folded in with the -b cookies rather than stored straight,
				// so that a paste carrying both ends up with one Cookie header
				// instead of two that Normalize would silently reduce to
				// whichever came last.
				cookies = append(cookies, value)
			default:
				set.Headers = append(set.Headers, Header{Name: name, Value: value})
			}
		case arg == "-b" || arg == "--cookie":
			v := next()
			// curl reads a -b argument with no "=" in it as a FILE NAME, and a
			// file name is not a cookie. Storing it would produce a Cookie
			// header holding a path.
			if strings.Contains(v, "=") {
				cookies = append(cookies, v)
			}
		case arg == "-e" || arg == "--referer":
			v := next()
			// curl's ";auto" suffix asks it to re-send the previous URL as the
			// referer, which is a statement about a chain of requests this
			// package does not make.
			if v = strings.TrimSuffix(v, ";auto"); v != "" {
				set.Headers = append(set.Headers, Header{Name: "Referer", Value: v})
			}
		case arg == "-A" || arg == "--user-agent":
			if v := next(); v != "" {
				set.Headers = append(set.Headers, Header{Name: "User-Agent", Value: v})
			}
		case arg == "-u" || arg == "--user":
			user = next()
		case arg == "--url":
			if o := OriginOf(next()); o != "" && set.Origin == "" {
				set.Origin = o
			}
		case takesValue[arg]:
			// A flag whose value would otherwise be mistaken for the URL.
			next()
		case strings.HasPrefix(arg, "-"):
			// Every other flag, including the bundled short ones. Skipped
			// whole rather than examined: this parser only has to find the
			// credential, and a flag it does not know cannot carry one.
		default:
			if o := OriginOf(arg); o != "" && set.Origin == "" {
				set.Origin = o
			}
		}
	}
	if user != "" {
		// -u without a colon means curl would prompt for the password. There
		// is nobody here to prompt, and sending the username with an empty
		// password is a login attempt that fails in a way that looks like a
		// wrong password rather than like a half-finished paste.
		if !strings.Contains(user, ":") {
			return Set{}, fmt.Errorf("hostheaders: the -u login has no password; paste it as user:password")
		}
		set.Headers = append(set.Headers, Header{
			Name:  "Authorization",
			Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(user)),
		})
	}
	if len(cookies) > 0 {
		set.Headers = append(set.Headers, Header{Name: "Cookie", Value: joinCookies(cookies)})
	}
	if len(set.Headers) == 0 {
		return Set{}, fmt.Errorf("hostheaders: this curl command carries no headers, cookies or login")
	}
	return set, nil
}

// takesValue is the curl flags whose next argument must be stepped over so it
// is not mistaken for the URL. Only the ones a real "Copy as cURL" paste
// actually contains; an unknown flag that takes a value costs at worst a
// candidate URL that OriginOf then refuses anyway.
var takesValue = map[string]bool{
	"-X": true, "--request": true,
	"-d": true, "--data": true, "--data-raw": true, "--data-binary": true,
	"--data-urlencode": true, "-F": true, "--form": true,
	"-o": true, "--output": true, "-x": true, "--proxy": true,
	"--connect-timeout": true, "-m": true, "--max-time": true,
	"--retry": true, "-w": true, "--write-out": true,
}

// parseBlock reads a header block, a cookie block, or the two mixed - which is
// what a paste out of a browser panel usually is.
func parseBlock(text string) (Set, error) {
	var (
		set       Set
		cookies   []string
		authority string
		scheme    string
		host      string
	)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// HTTP/2 pseudo-headers. They are not headers a request may carry -
		// net/http builds them from the URL - but :scheme and :authority
		// together are the origin, which is the one thing a header block
		// otherwise cannot say.
		if strings.HasPrefix(line, ":") {
			name, value, ok := splitHeader(line[1:])
			if !ok {
				continue
			}
			switch strings.ToLower(name) {
			case "authority":
				authority = value
			case "scheme":
				scheme = strings.ToLower(value)
			}
			continue
		}
		name, value, ok := splitHeader(line)
		if !ok {
			// Not a header line. A bare "a=1; b=2" is the cookie block, which
			// is the whole point of accepting this shape; anything else - a
			// "GET /x HTTP/1.1" request line, a stray word - is skipped rather
			// than refused, because a paste that is 95% right should not be
			// rejected over the one line the panel put at the top of it.
			if strings.Contains(line, "=") && !strings.ContainsAny(line, " \t") {
				cookies = append(cookies, line)
			} else if looksLikeCookieBlock(line) {
				cookies = append(cookies, line)
			}
			continue
		}
		switch {
		case name == "Host":
			// Read here and dropped from the headers below by the same list
			// that refuses it in a curl paste: Host may not be SENT, and it is
			// still the only thing an HTTP/1.1 header block says about which
			// server the block came from.
			host = value
		case name == "Cookie":
			cookies = append(cookies, value)
		case dropped[name]:
		default:
			set.Headers = append(set.Headers, Header{Name: name, Value: value})
		}
	}
	if len(cookies) > 0 {
		set.Headers = append(set.Headers, Header{Name: "Cookie", Value: joinCookies(cookies)})
	}
	if len(set.Headers) == 0 {
		return Set{}, fmt.Errorf("hostheaders: this text holds no headers and no cookies")
	}
	switch {
	case scheme != "" && authority != "":
		set.Origin = OriginOf(scheme + "://" + authority)
	case authority != "":
		set.Origin = OriginOf("https://" + authority)
	case host != "":
		// https and not http, and the guess is stated here rather than hidden:
		// a Host header names the server and says nothing about the scheme, and
		// a plaintext guess would attach a credential to an unencrypted hop.
		// Guessing the safe one costs a profile that does not match when the
		// site really is http, which the user then corrects in the origin box;
		// guessing the other way costs the credential.
		set.Origin = OriginOf("https://" + host)
	}
	return set, nil
}

// looksLikeCookieBlock recognises the "a=1; b=2" shape when it has spaces in
// it, which the plain "contains =" test above deliberately does not cover:
// "GET /x?a=1 HTTP/1.1" contains an "=" and a space too, and filing that as
// cookies would produce a Cookie header made of a request line.
func looksLikeCookieBlock(line string) bool {
	parts := strings.Split(line, ";")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name, _, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(strings.TrimSpace(name), " \t") {
			return false
		}
	}
	return true
}

// splitHeader cuts "Name: value" and canonicalises the name. It reports false
// for anything that is not a header line, which is what lets parseBlock tell a
// header from a cookie block from a request line.
//
// It deliberately does NOT apply the dropped list. Host is in that list and is
// also the one line an HTTP/1.1 block carries the origin in, so the two
// callers read the name first and decide what to do with it afterwards - a
// splitter that had already swallowed Host would leave parseBlock nothing to
// scope the profile with.
func splitHeader(raw string) (string, string, bool) {
	name, value, ok := strings.Cut(raw, ":")
	if !ok {
		return "", "", false
	}
	name = canonicalName(name)
	if name == "" {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	return name, value, true
}

// joinCookies folds every cookie source in a paste into the one Cookie header
// a request may carry, dropping repeats of the same cookie name.
//
// Repeats happen for real: a "Copy as cURL" paste from Chrome carries the
// session both in -H 'Cookie: ...' and, on some versions, in -b, and a header
// holding the same cookie twice is answered differently by different servers -
// some take the first, some the last, some refuse the request.
func joinCookies(sources []string) string {
	seen := map[string]bool{}
	var out []string
	for _, src := range sources {
		for _, part := range strings.Split(src, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, _, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, part)
		}
	}
	return strings.Join(out, "; ")
}

// shellSplit breaks a pasted command line into arguments the way a shell
// would, far enough to find the flags above.
//
// It is not a shell and does not try to be: no variable expansion, no globbing,
// no operators. What it does handle is what a copied curl line actually
// contains - single quotes (Chrome and Firefox on Linux and macOS), double
// quotes with backslash escapes (Windows), and the three line continuations
// the three shells use, because a pasted multi-line command arrives with them
// in it and a parser that stops at the first newline finds one header out of
// nine.
func shellSplit(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, cont := range []string{"\\\n", "^\n", "`\n"} {
		s = strings.ReplaceAll(s, cont, " ")
	}
	var (
		args []string
		cur  strings.Builder
		open bool // a token has been started, so "" is a real empty argument
		q    byte // 0, '\'' or '"'
	)
	flush := func() {
		if open {
			args = append(args, cur.String())
			cur.Reset()
			open = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '\'':
			if c == '\'' {
				q = 0
				continue
			}
			cur.WriteByte(c)
		case q == '"':
			if c == '\\' && i+1 < len(s) {
				// Only the four characters a double-quoted shell string
				// actually escapes. Anything else keeps its backslash, which
				// matters because a Windows paste is full of \" inside JSON
				// bodies this parser walks past.
				if n := s[i+1]; n == '"' || n == '\\' || n == '$' || n == '`' {
					cur.WriteByte(n)
					i++
					continue
				}
			}
			if c == '"' {
				q = 0
				continue
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			q = c
			open = true
		case c == '\\' && i+1 < len(s):
			cur.WriteByte(s[i+1])
			i++
			open = true
		case c == ' ' || c == '\t' || c == '\n':
			flush()
		default:
			cur.WriteByte(c)
			open = true
		}
	}
	flush()
	return args
}
