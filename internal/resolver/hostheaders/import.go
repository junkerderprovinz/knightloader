package hostheaders

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// dropped names the headers a pasted block never contributes. The engine
// issues its own Range requests and counts raw bytes, so a copied Range or
// Accept-Encoding would break chunked downloads and progress. Host,
// Content-Length, Connection and the like describe the request itself, and a
// pasted Host could point a credential at another server.
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

// Parse reads what a browser's developer tools put on the clipboard into a
// Set: a "Copy as cURL" command line, a request header block (with HTTP/2
// pseudo-headers or a Host line), or a bare "a=1; b=2" cookie block. It works
// out the shape itself.
//
// Origin is filled in when the paste names one; otherwise Store.Import uses
// the URL typed beside the box. The result is not normalised, so both origins
// go through the same Normalize call in the caller.
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

// isCurl looks for the command word at the start of the paste, since a header
// or cookie value can contain any text.
func isCurl(s string) bool {
	first := s
	if i := strings.IndexAny(first, " \t\r\n"); i >= 0 {
		first = first[:i]
	}
	first = strings.ToLower(strings.Trim(first, "\"'"))
	// "curl.exe" is what a Windows "Copy as cURL (cmd)" paste starts with.
	return first == "curl" || first == "curl.exe" || strings.HasSuffix(first, "/curl")
}

// parseCurl reads the flags that carry a credential and ignores the rest, such
// as --compressed, -X or a request body.
func parseCurl(text string) (Set, error) {
	args := shellSplit(text)
	var (
		set     Set
		cookies []string
		user    string
	)
	// next consumes a flag's value so it is never read as the URL.
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
				// Merged with the -b cookies into one Cookie header.
				cookies = append(cookies, value)
			default:
				set.Headers = append(set.Headers, Header{Name: name, Value: value})
			}
		case arg == "-b" || arg == "--cookie":
			v := next()
			// curl reads a -b argument without "=" as a file name.
			if strings.Contains(v, "=") {
				cookies = append(cookies, v)
			}
		case arg == "-e" || arg == "--referer":
			v := next()
			// ";auto" is about curl following redirects, which does not
			// apply here.
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
			next()
		case strings.HasPrefix(arg, "-"):
		default:
			if o := OriginOf(arg); o != "" && set.Origin == "" {
				set.Origin = o
			}
		}
	}
	if user != "" {
		// Without a colon curl would prompt for the password, and an empty
		// one would fail like a wrong password.
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

// takesValue lists the curl flags from "Copy as cURL" pastes whose value must
// be skipped so it is not taken for the URL.
var takesValue = map[string]bool{
	"-X": true, "--request": true,
	"-d": true, "--data": true, "--data-raw": true, "--data-binary": true,
	"--data-urlencode": true, "-F": true, "--form": true,
	"-o": true, "--output": true, "-x": true, "--proxy": true,
	"--connect-timeout": true, "-m": true, "--max-time": true,
	"--retry": true, "-w": true, "--write-out": true,
}

// parseBlock reads a header block, a cookie block, or the two mixed.
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
		// HTTP/2 pseudo-headers are never sent, but :scheme and :authority
		// give the origin.
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
			// A bare "a=1; b=2" is a cookie block. Anything else, such as a
			// request line, is skipped so the rest of the paste still counts.
			if strings.Contains(line, "=") && !strings.ContainsAny(line, " \t") {
				cookies = append(cookies, line)
			} else if looksLikeCookieBlock(line) {
				cookies = append(cookies, line)
			}
			continue
		}
		switch {
		case name == "Host":
			// Never sent, but the only origin an HTTP/1.1 block names.
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
		// Host says nothing about the scheme. Guessing https at worst gives a
		// profile the user corrects; guessing http could leak the credential.
		set.Origin = OriginOf("https://" + host)
	}
	return set, nil
}

// looksLikeCookieBlock recognises the "a=1; b=2" shape with spaces in it
// without mistaking "GET /x?a=1 HTTP/1.1" for cookies.
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

// splitHeader cuts "Name: value" and canonicalises the name, reporting false
// for anything that is not a header line. The callers apply the dropped list
// themselves because parseBlock still needs to read Host.
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

// joinCookies folds every cookie source in a paste into one Cookie header,
// keeping the first of repeated names. Chrome can put the same session in both
// -H 'Cookie: ...' and -b, and servers disagree on how to read duplicates.
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

// shellSplit breaks a pasted command line into arguments. It handles what a
// copied curl line contains (single quotes, double quotes with backslash
// escapes, and the line continuations of sh, cmd and PowerShell) and nothing
// else a shell does.
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
				// Only the characters a double-quoted shell string escapes;
				// any other backslash is kept.
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
