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
// The line number is a field so the editor can mark the row.
type Problem struct {
	Line int    `json:"line"` // one-based, counting the script's own lines
	Text string `json:"text"` // the line verbatim, trimmed
	Why  string `json:"why"`
}

func (p Problem) Error() string { return fmt.Sprintf("line %d: %s", p.Line, p.Why) }

// Import is what one script turned into. Requests holds as much as mapped, so
// the editor can show it next to the problems, but a non-nil error from
// ImportScript means the script must not be stored: half a router script is a
// login without a reboot or the other way round.
type Import struct {
	Requests []Request `json:"requests"`
	Problems []Problem `json:"problems,omitempty"`

	// Variables are the names the script used, after translation, so the form
	// asks only for the username, password and router fields it needs.
	Variables []string `json:"variables,omitempty"`
}

// jdVariables translates JDownloader's variable names into this package's.
// JDownloader's %%%routerip%%% is the gateway on the LAN, while %%ip%% here is
// the public address; mapping one onto the other would send the router
// password to the internet, so the router gets its own variable.
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

// The block markers. JDownloader wraps a LiveHeader recording in HSRC and a
// curl recording in CURL; they are compared case-insensitively because scripts
// are hand-edited.
const (
	markerHSRCOpen  = "[[[HSRC]]]"
	markerHSRCClose = "[[[/HSRC]]]"
	markerCURLOpen  = "[[[CURL]]]"
	markerCURLClose = "[[[/CURL]]]"
)

// maxScriptLines and maxScriptRequests bound an import. A router script is a
// login and a reboot, and replaying thousands of requests would hammer the
// router.
const (
	maxScriptLines    = 2000
	maxScriptRequests = 64
)

// ImportScript reads a JDownloader LiveHeader or curl reconnect script into
// this package's HTTP request method. Every line that cannot be mapped is
// reported with its number and reason and makes the error non-nil; guessing
// would only show up as a reconnect that silently does nothing.
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
				// Parsing on would read the rest of the file as one request.
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
			// Anything else outside a block is refused rather than skipped; a
			// line silently ignored could have been the reboot request.
			problem(i+1, raw, "outside a "+markerHSRCOpen+" block")
		}
		if len(imp.Requests) > maxScriptRequests {
			return imp, fmt.Errorf("%w: it has more than %d requests", ErrImport, maxScriptRequests)
		}
	}

	if len(imp.Requests) == 0 && len(imp.Problems) == 0 {
		// An empty file means the wrong thing was pasted.
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

// parseBlock turns one block into a request. firstLine is the script line
// number of body[0], so problems name lines in the file.
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
		// reqLineRefused is set once the request line has been refused, so the
		// follow-on faults (the next line read as a request line, a block with
		// no request) are not reported as well.
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
			// An unmappable variable in the request line drops the line.
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
			// A blank line ends the headers only after a request line;
			// recordings often have one above the request.
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
		// Unless the request line was already reported.
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
		// The Host header became the URL's host. Keeping it too would let a
		// later edit to the header, which wins over the URL, redirect the
		// request without the form showing it.
		delete(headers, hostHeaderName(headers))
	}
	if len(headers) > 0 {
		req.Headers = headers
	}
	return req, problems
}

// parseRequestLine splits "GET /reboot.cgi HTTP/1.1" into its parts. The
// protocol version is dropped, since net/http negotiates its own.
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

// knownMethod is a fixed list so a stray header on a block's first line is not
// read as a method named "CONTENT-TYPE:".
func knownMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodPatch, http.MethodOptions:
		return true
	}
	return false
}

// absoluteURL turns a recorded target into something that can be requested.
// LiveHeader records the request line as the router saw it, so the target is
// usually a path and the address is in the Host header. usedHost reports that
// the Host header was folded into the URL.
func absoluteURL(target string, headers map[string]string) (url string, usedHost bool, err error) {
	if strings.HasPrefix(strings.ToLower(target), "http://") || strings.HasPrefix(strings.ToLower(target), "https://") {
		// An absolute target keeps its Host header, which firmware that
		// virtual-hosts its admin page needs.
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

// parseCurlBlock reads a curl command line. Flags that change what is sent are
// understood, flags that only change curl's local behaviour are dropped, and
// anything else is refused by name, since a dropped --data would be a login
// that posts nothing.
func parseCurlBlock(body []string, firstLine int, vars map[string]bool) (Request, []Problem) {
	var (
		req      Request
		problems []Problem
		headers  = map[string]string{}
	)
	problem := func(n int, raw, why string) {
		problems = append(problems, Problem{Line: n, Text: strings.TrimSpace(raw), Why: why})
	}

	// A command copied from a terminal is often split with trailing
	// backslashes.
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
	// next advances i past a flag's argument.
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
				// The file is on the machine the script was recorded on.
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
				// The header is base64-encoded here, before variables are
				// expanded at run time, so the router would receive the
				// encoded placeholder.
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
// only encoded, and they end up in the request list, out of reach of the
// password field's redaction.
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
// honouring single and double quotes and a backslash escape. The tokens become
// request fields and never reach a shell, so a backtick or semicolon stays
// data.
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
		// The rest of the command would be one argument, swallowing the URL.
		return nil, errors.New("the command has an unclosed quote in it")
	}
	flush()
	return out, nil
}

// translateVars rewrites JDownloader's %%%name%%% into this package's %%name%%.
// An unknown name is refused: it would expand to nothing at run time, and an
// empty password reads to the router as a wrong one.
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

// sortedVars returns the variables a script used, in a fixed order.
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
