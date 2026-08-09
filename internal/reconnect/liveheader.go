package reconnect

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrImport is what every failed import is, so a caller can tell an unusable
// script apart from an unusable configuration without reading the message.
var ErrImport = errors.New("reconnect: the script could not be imported")

// Problem is one line an import could not map, in words meant for the user.
//
// The line number is data rather than only part of the sentence, because the
// editor has to be able to put the marker on the right row: a message under a
// forty-line script still leaves somebody counting lines by hand.
type Problem struct {
	Line int    `json:"line"` // one-based, counting the script's own lines
	Text string `json:"text"` // the line verbatim, trimmed
	Why  string `json:"why"`
}

func (p Problem) Error() string { return fmt.Sprintf("line %d: %s", p.Line, p.Why) }

// Import is what one script turned into.
//
// Requests is filled in as far as the import got, so an editor can show what did
// map next to what did not. It is for showing, not for saving: a non-nil error
// from ImportScript means the script must not be stored. Half a router script is
// a login with no reboot, or a reboot with no login, and either one is a
// reconnect that hangs at three in the morning and reports "the address did not
// change" while the router sits there waiting for the rest of the conversation.
type Import struct {
	Requests []Request `json:"requests"`
	Problems []Problem `json:"problems,omitempty"`

	// Variables are the names the script used, after translation, so the form
	// can say which of the username, password and router fields it now needs.
	// A script that references none of them needs no credentials at all, and
	// asking for them anyway is how a working import gets abandoned.
	Variables []string `json:"variables,omitempty"`
}

// jdVariables translates JDownloader's variable names into this package's.
//
// JD writes three percent signs, this package writes two, and the names differ
// in the one place it matters: JD's %%%routerip%%% is the gateway on the LAN
// while this package's %%ip%% is the public address the box had before the run.
// Mapping routerip onto ip would point a login request carrying the router
// password at the public internet, so the two are kept apart here and a router
// address becomes its own variable and its own field.
var jdVariables = map[string]string{
	"routerip": VarRouter,
	"router":   VarRouter,
	"host":     VarRouter,
	"ip":       VarIP,
	"username": VarUsername,
	"user":     VarUsername,
	"password": VarPassword,
	"pass":     VarPassword,
}

// The block markers. JD wraps a LiveHeader recording in HSRC and a curl
// recording in CURL, and writes both in upper case; the comparison is
// case-insensitive anyway because hand-edited scripts are not consistent.
const (
	markerHSRCOpen  = "[[[HSRC]]]"
	markerHSRCClose = "[[[/HSRC]]]"
	markerCURLOpen  = "[[[CURL]]]"
	markerCURLClose = "[[[/CURL]]]"
)

// maxScriptLines and maxScriptRequests bound an import. A router script is a
// login and a reboot; a file with thousands of requests in it is not one, and
// replaying it would hammer the router rather than reconnect it.
const (
	maxScriptLines    = 2000
	maxScriptRequests = 64
)

// ImportScript reads a JDownloader LiveHeader or curl reconnect script into this
// package's HTTP request method.
//
// Thousands of these exist, one per router model, and a user who has one in hand
// should not have to retype it. What they must not get is a script that was
// imported approximately: every line that cannot be mapped is reported with its
// number and the reason, and the error is non-nil whenever there is one of them.
// Guessing at an unrecognised line is the failure this refuses to produce,
// because the guess only shows up as a reconnect that silently does nothing.
func ImportScript(text string) (Import, error) {
	var imp Import
	vars := make(map[string]bool)

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > maxScriptLines {
		return imp, fmt.Errorf("%w: it has more than %d lines", ErrImport, maxScriptLines)
	}

	problem := func(n int, raw, why string) {
		imp.Problems = append(imp.Problems, Problem{Line: n, Text: strings.TrimSpace(raw), Why: why})
	}

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isComment(trimmed) {
			continue
		}
		upper := strings.ToUpper(trimmed)
		switch {
		case upper == markerHSRCOpen, upper == markerCURLOpen:
			curl := upper == markerCURLOpen
			closer := markerHSRCClose
			if curl {
				closer = markerCURLClose
			}
			body, end, ok := blockLines(lines, i+1, closer)
			if !ok {
				problem(i+1, raw, "the block is never closed with "+closer)
				// Parsing on from here would read the rest of the file as one
				// enormous request. Stopping is the honest answer.
				imp.Variables = sortedVars(vars)
				return imp, importError(imp)
			}
			req, probs := parseBlock(body, i+2, curl, vars)
			imp.Problems = append(imp.Problems, probs...)
			if len(probs) == 0 {
				imp.Requests = append(imp.Requests, req)
			}
			i = end
		case upper == markerHSRCClose, upper == markerCURLClose:
			problem(i+1, raw, "a closing marker with no block open before it")
		default:
			// Everything else outside a block is refused rather than skipped.
			// JD's own scripts carry metadata lines up here, and a header this
			// parser silently ignored could just as easily have been the request
			// that does the reboot.
			problem(i+1, raw, "outside a "+markerHSRCOpen+" block")
		}
		if len(imp.Requests) > maxScriptRequests {
			return imp, fmt.Errorf("%w: it has more than %d requests", ErrImport, maxScriptRequests)
		}
	}

	if len(imp.Requests) == 0 && len(imp.Problems) == 0 {
		// A file with nothing in it is not a successful import of nothing: the
		// user pasted the wrong thing, and saying so beats a form that clears
		// itself and looks like it worked.
		return imp, fmt.Errorf("%w: it contains no requests", ErrImport)
	}
	imp.Variables = sortedVars(vars)
	return imp, importError(imp)
}

// importError turns the collected problems into the one error the caller checks.
func importError(imp Import) error {
	if len(imp.Problems) == 0 {
		return nil
	}
	parts := make([]string, 0, len(imp.Problems))
	for _, p := range imp.Problems {
		parts = append(parts, p.Error())
	}
	return fmt.Errorf("%w: %s", ErrImport, strings.Join(parts, "; "))
}

// isComment reports whether a line outside a block is a comment. All three
// spellings appear in scripts found in the wild.
func isComment(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, ";")
}

// blockLines returns the lines of one block and the index of its closing marker.
func blockLines(lines []string, from int, closer string) ([]string, int, bool) {
	for i := from; i < len(lines); i++ {
		if strings.EqualFold(strings.TrimSpace(lines[i]), closer) {
			return lines[from:i], i, true
		}
	}
	return nil, 0, false
}

// parseBlock turns one block into a request. firstLine is the script line number
// of body[0], so every problem can name the line in the file the user is looking
// at rather than an offset into a block.
func parseBlock(body []string, firstLine int, curl bool, vars map[string]bool) (Request, []Problem) {
	if curl {
		return parseCurlBlock(body, firstLine, vars)
	}
	return parseHSRCBlock(body, firstLine, vars)
}

// parseHSRCBlock reads a LiveHeader recording: a request line, header lines, a
// blank line, and then the body.
//
//	GET /login.cgi HTTP/1.1
//	Host: %%%routerip%%%
//	Authorization: Basic ...
//
//	user=%%%username%%%&pass=%%%password%%%
func parseHSRCBlock(body []string, firstLine int, vars map[string]bool) (Request, []Problem) {
	var (
		req      Request
		problems []Problem
		headers  = map[string]string{}
		target   string
		inBody   bool
		bodyText []string
		// Set once the line that should have carried the request has been
		// refused. Everything after it in this block follows from that one
		// fault: with no method recorded, the next line is read as the request
		// line too, and the block ends without one. Reporting all three turns a
		// single bad line into a list the reader has to work out for themselves,
		// so the first message stands and the consequences stay quiet.
		reqLineRefused bool
	)
	problem := func(n int, raw, why string) {
		problems = append(problems, Problem{Line: n, Text: strings.TrimSpace(raw), Why: why})
	}

	for i, raw := range body {
		line := firstLine + i
		text, err := translateVars(raw, vars)
		if err != nil {
			problem(line, raw, err.Error())
			// An unmappable variable in the request line is exactly that fault:
			// the line is dropped, so no method is ever recorded.
			if !inBody && req.Method == "" {
				reqLineRefused = true
			}
			continue
		}
		trimmed := strings.TrimSpace(text)

		if inBody {
			bodyText = append(bodyText, text)
			continue
		}
		if trimmed == "" {
			// The blank line ends the headers only once a request line has been
			// seen. Recordings routinely have a blank line above the request,
			// and treating that one as the separator would put the whole request
			// into the body of an empty one.
			if req.Method != "" {
				inBody = true
			}
			continue
		}
		if req.Method == "" {
			method, tgt, ok := parseRequestLine(trimmed)
			if !ok {
				if !reqLineRefused {
					problem(line, raw, "expected a request line like \"GET /reboot.cgi HTTP/1.1\"")
					reqLineRefused = true
				}
				continue
			}
			req.Method, target = method, tgt
			continue
		}
		name, value, ok := strings.Cut(trimmed, ":")
		if !ok || strings.TrimSpace(name) == "" {
			problem(line, raw, "expected a header line like \"Host: 192.168.1.1\"")
			continue
		}
		headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}

	if req.Method == "" {
		// Only when nothing was said about the request line yet — otherwise this
		// repeats a fault that has already been named, one line further up.
		if !reqLineRefused {
			problem(firstLine, "", "the block has no request line in it")
		}
		return req, problems
	}

	full, usedHost, err := absoluteURL(target, headers)
	if err != nil {
		problem(firstLine, target, err.Error())
		return req, problems
	}
	req.URL = full
	req.Body = strings.TrimRight(strings.Join(bodyText, "\n"), "\n")
	if usedHost {
		// The Host header is dropped only where it became the URL's host.
		// Leaving it would say the same thing twice, and this package honours
		// the header over the URL, so a later edit to one of them would change
		// where the request goes without changing what the form shows.
		delete(headers, hostHeaderName(headers))
	}
	if len(headers) > 0 {
		req.Headers = headers
	}
	return req, problems
}

// parseRequestLine splits "GET /reboot.cgi HTTP/1.1" into its parts. The
// protocol version is dropped: this package speaks whatever net/http negotiates,
// and honouring a recorded "HTTP/1.0" would be a promise it cannot keep.
func parseRequestLine(s string) (method, target string, ok bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return "", "", false
	}
	method = strings.ToUpper(fields[0])
	if !knownMethod(method) {
		return "", "", false
	}
	return method, fields[1], true
}

// knownMethod is a fixed list rather than a "looks like a word" test, because
// the first line of a block is also where a stray header or a curl command would
// land, and reading "Content-Type:" as a method named CONTENT-TYPE: produces a
// request the router answers with nothing at all.
func knownMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodPatch, http.MethodOptions:
		return true
	}
	return false
}

// absoluteURL turns a recorded target into something that can be requested.
//
// LiveHeader records the request line as the router saw it, so the target is
// usually a path and the address lives in the Host header. Building the URL
// without it would produce "/login.cgi", which is not a URL and fails at the
// request rather than at the import - days later, in the middle of the night.
// usedHost is true when the Host header was folded into the URL, which is the
// only case where dropping it from the header map is right.
func absoluteURL(target string, headers map[string]string) (url string, usedHost bool, err error) {
	if strings.HasPrefix(strings.ToLower(target), "http://") || strings.HasPrefix(strings.ToLower(target), "https://") {
		// An absolute target keeps its Host header. Router firmware that
		// virtual-hosts its administration page is recorded as an address in the
		// URL and a name in the header, and folding the two together sends the
		// name the firmware answers to nowhere.
		return target, false, nil
	}
	if !strings.HasPrefix(target, "/") {
		return "", false, fmt.Errorf("the request target %q is neither a URL nor a path", target)
	}
	host := ""
	if name := hostHeaderName(headers); name != "" {
		host = strings.TrimSpace(headers[name])
	}
	if host == "" {
		return "", false, fmt.Errorf("the request target %q is a path and the block has no Host header to put in front of it", target)
	}
	return "http://" + host + target, true, nil
}

// hostHeaderName finds the Host header whatever case it was written in.
func hostHeaderName(headers map[string]string) string {
	for k := range headers {
		if strings.EqualFold(k, "Host") {
			return k
		}
	}
	return ""
}

// parseCurlBlock reads a curl command line. Only the flags that change what is
// sent are understood; a flag that only changes how curl behaves locally
// (--insecure, --silent, --location) is accepted and dropped, and anything else
// is refused by name rather than ignored, because a dropped --data is a login
// that posts nothing and reports success.
func parseCurlBlock(body []string, firstLine int, vars map[string]bool) (Request, []Problem) {
	var (
		req      Request
		problems []Problem
		headers  = map[string]string{}
	)
	problem := func(n int, raw, why string) {
		problems = append(problems, Problem{Line: n, Text: strings.TrimSpace(raw), Why: why})
	}

	// A curl command may be split over several lines with a trailing backslash,
	// which is how anybody who copied one out of a terminal will have it.
	joined, line := joinContinuations(body, firstLine)
	if strings.TrimSpace(joined) == "" {
		problem(firstLine, "", "the block has no curl command in it")
		return req, problems
	}
	text, err := translateVars(joined, vars)
	if err != nil {
		problem(line, joined, err.Error())
		return req, problems
	}

	tokens, err := splitCommand(text)
	if err != nil {
		problem(line, joined, err.Error())
		return req, problems
	}
	if len(tokens) == 0 || !strings.EqualFold(strings.TrimSuffix(tokens[0], ".exe"), "curl") {
		problem(line, joined, "expected a command starting with \"curl\"")
		return req, problems
	}

	target := ""
	// The index lives outside the loop because the flags consume the token after
	// them, and a loop variable advanced from inside a closure is a thing every
	// later reader has to stop and verify.
	i := 1
	for i < len(tokens) {
		tok := tokens[i]
		i++
		next := func() (string, bool) {
			if i >= len(tokens) {
				return "", false
			}
			v := tokens[i]
			i++
			return v, true
		}
		switch {
		case tok == "-X", tok == "--request":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no method after it")
				continue
			}
			req.Method = strings.ToUpper(v)
		case tok == "-H", tok == "--header":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no header after it")
				continue
			}
			name, value, found := strings.Cut(v, ":")
			if !found || strings.TrimSpace(name) == "" {
				problem(line, v, "the header is not written as \"Name: value\"")
				continue
			}
			headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
		case tok == "-d", tok == "--data", tok == "--data-raw", tok == "--data-urlencode", tok == "--data-binary":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no data after it")
				continue
			}
			if strings.HasPrefix(v, "@") {
				// curl reads the body out of a file here. That file is on the
				// machine the script was recorded on, so importing the flag
				// would produce a request whose body is the literal "@post.txt".
				problem(line, v, "reading the request body from a file is not supported")
				continue
			}
			if req.Body != "" {
				req.Body += "&"
			}
			req.Body += v
		case tok == "-u", tok == "--user":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no credentials after it")
				continue
			}
			if strings.Contains(v, "%%") {
				// The header is base64, and the encoding happens here while the
				// variable is expanded at run time - so the router would be sent
				// the encoded string "%%username%%:%%password%%" and answer with
				// a login failure that names the credentials. Refusing is the
				// only honest option: there is no encoded-variable to emit.
				problem(line, v, "curl's "+tok+" flag cannot carry a variable, because the header it becomes is encoded before the variable is filled in; write the credentials into the request body or an Authorization header instead")
				continue
			}
			headers["Authorization"] = basicAuth(v)
		case tok == "--url":
			v, ok := next()
			if !ok {
				problem(line, tok, "the --url flag has no URL after it")
				continue
			}
			target = v
		case tok == "-A", tok == "--user-agent":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no value after it")
				continue
			}
			headers["User-Agent"] = v
		case tok == "-e", tok == "--referer":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no value after it")
				continue
			}
			headers["Referer"] = v
		case tok == "-b", tok == "--cookie":
			v, ok := next()
			if !ok {
				problem(line, tok, "the "+tok+" flag has no value after it")
				continue
			}
			headers["Cookie"] = v
		case curlIgnorable[tok]:
			// Accepted and dropped: these change curl's own behaviour, not the
			// request on the wire, and refusing them would reject scripts that
			// are perfectly importable.
		case strings.HasPrefix(tok, "-"):
			problem(line, tok, "the curl flag "+tok+" is not understood, so the request would not be the one that was recorded")
		default:
			if target != "" {
				problem(line, tok, "there is more than one URL in the command")
				continue
			}
			target = tok
		}
	}

	if target == "" {
		problem(line, joined, "the curl command has no URL in it")
		return req, problems
	}
	full, usedHost, err := absoluteURL(target, headers)
	if err != nil {
		problem(line, target, err.Error())
		return req, problems
	}
	req.URL = full
	if req.Method == "" {
		// curl's own rule: a body makes it a POST, and everything else is a GET.
		req.Method = http.MethodGet
		if req.Body != "" {
			req.Method = http.MethodPost
		}
	}
	if usedHost {
		delete(headers, hostHeaderName(headers))
	}
	if len(headers) > 0 {
		req.Headers = headers
	}
	return req, problems
}

// curlIgnorable are the flags that change nothing about the request as the
// router receives it.
var curlIgnorable = map[string]bool{
	"-k": true, "--insecure": true,
	"-s": true, "--silent": true,
	"-S": true, "--show-error": true,
	"-L": true, "--location": true,
	"-i": true, "--include": true,
	"-v": true, "--verbose": true,
	"-g": true, "--globoff": true,
	"--compressed": true,
}

// basicAuth builds the header curl's -u flag produces. The credentials are
// encoded, not hashed, and that is worth saying out loud: an imported script
// with a password baked into it stores that password in the request list, where
// this package's password field and its redaction cannot reach it.
func basicAuth(userpass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(userpass))
}

// joinContinuations folds a backslash-continued command into one line and
// reports the line number the command started on.
func joinContinuations(body []string, firstLine int) (string, int) {
	var (
		parts []string
		start = firstLine
		found bool
	)
	for i, raw := range body {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if !found {
			start = firstLine + i
			found = true
		}
		parts = append(parts, strings.TrimSuffix(trimmed, "\\"))
	}
	return strings.Join(parts, " "), start
}

// splitCommand splits a command line into arguments the way a shell would,
// honouring single and double quotes and a backslash escape.
//
// This is the reverse of what the script method refuses to do. Reading quotes
// here is safe because the result is a list that is never handed to a shell: the
// tokens become a URL, a body and header values in this package's own request
// list, so a backtick or a semicolon inside one is data and stays data.
func splitCommand(s string) ([]string, error) {
	var (
		out   []string
		cur   strings.Builder
		open  bool // a token is being built
		quote byte // 0, '\'' or '"'
	)
	flush := func() {
		if open {
			out = append(out, cur.String())
			cur.Reset()
			open = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == '\'':
			// Single quotes are literal in every shell, backslash included.
			cur.WriteByte(c)
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			open = true
		case quote != 0:
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			open = true
		case c == ' ' || c == '\t' || c == '\n':
			flush()
		default:
			cur.WriteByte(c)
			open = true
		}
	}
	if quote != 0 {
		// An unterminated quote means the rest of the command was read as one
		// argument, which is never what was meant and would silently swallow the
		// URL into a header value.
		return nil, errors.New("the command has an unclosed quote in it")
	}
	flush()
	return out, nil
}

// translateVars rewrites JDownloader's %%%name%%% into this package's %%name%%.
//
// An unknown name is a refusal, not a pass-through. Every one of them would
// expand to nothing at run time and leave a request that is subtly wrong -
// a login posting an empty password reads to the router as a bad password, and
// the reconnect fails with a message about credentials that are perfectly fine.
func translateVars(s string, seen map[string]bool) (string, error) {
	const marker = "%%%"
	if !strings.Contains(s, marker) {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	rest := s
	for {
		before, after, found := strings.Cut(rest, marker)
		if !found {
			b.WriteString(rest)
			return b.String(), nil
		}
		name, tail, closed := strings.Cut(after, marker)
		if !closed {
			return "", errors.New("a %%%variable%%% is opened and never closed")
		}
		b.WriteString(before)
		key := strings.ToLower(strings.TrimSpace(name))
		mapped, ok := jdVariables[key]
		if !ok {
			return "", fmt.Errorf("the variable %%%%%%%s%%%%%% has no equivalent here", name)
		}
		seen[mapped] = true
		b.WriteString("%%" + mapped + "%%")
		rest = tail
	}
}

// sortedVars returns the variables a script used, in a fixed order so the form
// always asks for them the same way round.
func sortedVars(seen map[string]bool) []string {
	order := []string{VarRouter, VarUsername, VarPassword, VarIP}
	var out []string
	for _, v := range order {
		if seen[v] {
			out = append(out, v)
		}
	}
	return out
}
