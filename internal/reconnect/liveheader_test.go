package reconnect

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestImportScript is the table for what a JDownloader reconnect script turns
// into. Every refusal in it is a request that would otherwise have been imported
// approximately, and an approximately imported script does not fail at import
// time - it fails at three in the morning as "the address did not change".
func TestImportScript(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []Request
		// wantProblem is a fragment of the reason, and wantLine the script line
		// it has to be reported against. Both empty means the import must work.
		wantProblem string
		wantLine    int
	}{
		{
			name: "a two-step LiveHeader script",
			script: "# Fritz!Box 7590\n" +
				"[[[HSRC]]]\n" +
				"POST /login.cgi HTTP/1.1\n" +
				"Host: %%%routerip%%%\n" +
				"Content-Type: application/x-www-form-urlencoded\n" +
				"\n" +
				"username=%%%username%%%&password=%%%password%%%\n" +
				"[[[/HSRC]]]\n" +
				"[[[HSRC]]]\n" +
				"GET /reboot.cgi HTTP/1.1\n" +
				"Host: %%%routerip%%%\n" +
				"[[[/HSRC]]]\n",
			want: []Request{
				{
					Method:  "POST",
					URL:     "http://%%router%%/login.cgi",
					Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
					Body:    "username=%%username%%&password=%%password%%",
				},
				{Method: "GET", URL: "http://%%router%%/reboot.cgi"},
			},
		},
		{
			// An absolute target keeps its Host header: router firmware that
			// virtual-hosts the administration page is recorded exactly this way,
			// and folding the two together is a login that never lands.
			name: "an absolute URL keeps a differing Host header",
			script: "[[[HSRC]]]\n" +
				"GET http://192.168.1.1/reboot HTTP/1.1\n" +
				"Host: fritz.box\n" +
				"[[[/HSRC]]]\n",
			want: []Request{{
				Method:  "GET",
				URL:     "http://192.168.1.1/reboot",
				Headers: map[string]string{"Host": "fritz.box"},
			}},
		},
		{
			name: "a blank line above the request line is not the body separator",
			script: "[[[HSRC]]]\n" +
				"\n" +
				"GET /reboot HTTP/1.1\n" +
				"Host: 192.168.1.1\n" +
				"[[[/HSRC]]]\n",
			want: []Request{{Method: "GET", URL: "http://192.168.1.1/reboot"}},
		},
		{
			name: "a curl command",
			script: "[[[CURL]]]\n" +
				`curl -X POST -H "Content-Type: application/x-www-form-urlencoded" ` +
				`-d "user=%%%username%%%&pw=%%%password%%%" http://%%%routerip%%%/login` + "\n" +
				"[[[/CURL]]]\n",
			want: []Request{{
				Method:  "POST",
				URL:     "http://%%router%%/login",
				Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
				Body:    "user=%%username%%&pw=%%password%%",
			}},
		},
		{
			name: "a curl command split over lines with a backslash",
			script: "[[[CURL]]]\n" +
				"curl -s -k \\\n" +
				"  --data 'x=1' \\\n" +
				"  'http://192.168.1.1/reboot'\n" +
				"[[[/CURL]]]\n",
			want: []Request{{
				Method: "POST", // curl's own rule: a body makes it a POST
				URL:    "http://192.168.1.1/reboot",
				Body:   "x=1",
			}},
		},
		{
			name: "two curl data flags are joined the way curl joins them",
			script: "[[[CURL]]]\n" +
				"curl -d a=1 -d b=2 http://192.168.1.1/x\n" +
				"[[[/CURL]]]\n",
			want: []Request{{Method: "POST", URL: "http://192.168.1.1/x", Body: "a=1&b=2"}},
		},
		{
			name: "curl basic auth without a variable",
			script: "[[[CURL]]]\n" +
				"curl -u admin:letmein http://192.168.1.1/reboot\n" +
				"[[[/CURL]]]\n",
			want: []Request{{
				Method:  "GET",
				URL:     "http://192.168.1.1/reboot",
				Headers: map[string]string{"Authorization": "Basic YWRtaW46bGV0bWVpbg=="},
			}},
		},

		// Everything below has to be refused, with the line named.

		{
			name: "an unknown variable",
			script: "[[[HSRC]]]\n" +
				"GET /reboot?token=%%%sessionid%%% HTTP/1.1\n" +
				"Host: 192.168.1.1\n" +
				"[[[/HSRC]]]\n",
			wantProblem: "sessionid",
			wantLine:    2,
		},
		{
			name: "a variable that is never closed",
			script: "[[[HSRC]]]\n" +
				"GET /reboot HTTP/1.1\n" +
				"Host: %%%routerip\n" +
				"[[[/HSRC]]]\n",
			wantProblem: "never closed",
			wantLine:    3,
		},
		{
			name: "a path target with no Host header",
			script: "[[[HSRC]]]\n" +
				"GET /reboot.cgi HTTP/1.1\n" +
				"[[[/HSRC]]]\n",
			wantProblem: "no Host header",
			wantLine:    2,
		},
		{
			name: "a block with no request line",
			script: "[[[HSRC]]]\n" +
				"Host: 192.168.1.1\n" +
				"[[[/HSRC]]]\n",
			wantProblem: "request line",
			wantLine:    2,
		},
		{
			name: "a block that is never closed",
			script: "[[[HSRC]]]\n" +
				"GET /reboot HTTP/1.1\n" +
				"Host: 192.168.1.1\n",
			wantProblem: "never closed",
			wantLine:    1,
		},
		{
			name: "a closing marker on its own",
			script: "[[[/HSRC]]]\n" +
				"[[[HSRC]]]\nGET /x HTTP/1.1\nHost: h\n[[[/HSRC]]]\n",
			wantProblem: "no block open",
			wantLine:    1,
		},
		{
			// JD's own files carry metadata above the blocks. Skipping an
			// unrecognised line silently would just as happily skip the request
			// that does the reboot.
			name: "a line outside any block",
			script: "Router: Fritz!Box\n" +
				"[[[HSRC]]]\nGET /x HTTP/1.1\nHost: h\n[[[/HSRC]]]\n",
			wantProblem: "outside a",
			wantLine:    1,
		},
		{
			name: "a curl flag that changes what is sent",
			script: "[[[CURL]]]\n" +
				"curl --form file=@x http://192.168.1.1/x\n" +
				"[[[/CURL]]]\n",
			wantProblem: "--form",
			wantLine:    2,
		},
		{
			name: "a curl body read from a file",
			script: "[[[CURL]]]\n" +
				"curl -d @post.txt http://192.168.1.1/x\n" +
				"[[[/CURL]]]\n",
			wantProblem: "from a file",
			wantLine:    2,
		},
		{
			// The header is base64 and the variable is filled in later, so the
			// router would be handed the encoded placeholder and answer with a
			// login failure that blames perfectly good credentials.
			name: "curl basic auth with a variable in it",
			script: "[[[CURL]]]\n" +
				"curl -u %%%username%%%:%%%password%%% http://192.168.1.1/reboot\n" +
				"[[[/CURL]]]\n",
			wantProblem: "cannot carry a variable",
			wantLine:    2,
		},
		{
			name: "a curl command with an unclosed quote",
			script: "[[[CURL]]]\n" +
				"curl -d \"x=1 http://192.168.1.1/x\n" +
				"[[[/CURL]]]\n",
			wantProblem: "unclosed quote",
			wantLine:    2,
		},
		{
			name: "a curl command with two URLs",
			script: "[[[CURL]]]\n" +
				"curl http://192.168.1.1/a http://192.168.1.1/b\n" +
				"[[[/CURL]]]\n",
			wantProblem: "more than one URL",
			wantLine:    2,
		},
		{
			name: "a block that is not curl",
			script: "[[[CURL]]]\n" +
				"wget http://192.168.1.1/x\n" +
				"[[[/CURL]]]\n",
			wantProblem: "starting with \"curl\"",
			wantLine:    2,
		},
		{
			name:        "an empty script",
			script:      "",
			wantProblem: "no requests",
		},
		{
			name:        "a script that is only comments",
			script:      "# nothing here\n// nor here\n",
			wantProblem: "no requests",
		},
		{
			name: "a header line that is not a header",
			script: "[[[HSRC]]]\n" +
				"GET /x HTTP/1.1\n" +
				"Host 192.168.1.1\n" +
				"[[[/HSRC]]]\n",
			wantProblem: "header line",
			wantLine:    3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			imp, err := ImportScript(tc.script)
			if tc.wantProblem != "" {
				if err == nil {
					t.Fatalf("ImportScript accepted a script it cannot run: %+v", imp)
				}
				if !errors.Is(err, ErrImport) {
					t.Errorf("error = %v, want ErrImport", err)
				}
				if !strings.Contains(err.Error(), tc.wantProblem) {
					t.Errorf("error %q does not mention %q", err, tc.wantProblem)
				}
				if tc.wantLine != 0 {
					if !hasProblemOnLine(imp.Problems, tc.wantLine) {
						t.Errorf("problems %+v do not name line %d", imp.Problems, tc.wantLine)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("ImportScript: %v", err)
			}
			if len(imp.Problems) != 0 {
				t.Fatalf("a clean import reported problems: %+v", imp.Problems)
			}
			assertRequests(t, imp.Requests, tc.want)
		})
	}
}

func hasProblemOnLine(problems []Problem, line int) bool {
	for _, p := range problems {
		if p.Line == line {
			return true
		}
	}
	return false
}

func assertRequests(t *testing.T, got, want []Request) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d requests, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Method != w.Method || g.URL != w.URL || g.Body != w.Body {
			t.Errorf("request %d = %s %s body %q, want %s %s body %q",
				i+1, g.Method, g.URL, g.Body, w.Method, w.URL, w.Body)
		}
		if len(g.Headers) != len(w.Headers) {
			t.Errorf("request %d headers = %v, want %v", i+1, g.Headers, w.Headers)
			continue
		}
		for k, v := range w.Headers {
			if g.Headers[k] != v {
				t.Errorf("request %d header %s = %q, want %q", i+1, k, g.Headers[k], v)
			}
		}
	}
}

// TestImportNeverMapsRouterIPOntoThePublicAddress is the one mapping that must
// not be got wrong, and it is easy to get wrong: JD calls the gateway
// %%%routerip%%% and this package calls the pre-reconnect public address %%ip%%,
// so a translation table that folded the two together would send a request
// carrying the router password out to the public internet instead of to the
// router on the LAN.
func TestImportNeverMapsRouterIPOntoThePublicAddress(t *testing.T) {
	imp, err := ImportScript("[[[HSRC]]]\nGET /reboot HTTP/1.1\nHost: %%%routerip%%%\n[[[/HSRC]]]\n")
	if err != nil {
		t.Fatalf("ImportScript: %v", err)
	}
	got := imp.Requests[0].URL
	if got != "http://%%"+VarRouter+"%%/reboot" {
		t.Fatalf("URL = %q, want the router variable", got)
	}
	if strings.Contains(got, "%%"+VarIP+"%%") {
		t.Fatal("%%%routerip%%% was mapped onto the public address variable")
	}

	// And the run has to follow the mapping: a configuration whose router is
	// 192.168.1.1 and whose public address is 203.0.113.9 must reach the former.
	client := &stubClient{checks: []step{{body: "203.0.113.9"}, {body: "198.51.100.7"}}}
	cfg := Config{
		Method:          MethodHTTP,
		Router:          "192.168.1.1",
		Requests:        imp.Requests,
		CheckURL:        checkURL,
		IntervalSeconds: 1,
		TimeoutSeconds:  10,
	}
	rc := newTestReconnector(t, cfg, client, (&runRecorder{}).run, newClock())
	if _, err := rc.Do(context.Background()); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if sent := client.request(0); sent.url != "http://192.168.1.1/reboot" {
		t.Errorf("the request went to %s, want the router at 192.168.1.1", sent.url)
	}
}

// TestImportReportsTheVariablesItNeeds: a form that asks for a username and a
// password a script never references is a form people abandon, and one that does
// not ask for the router address produces a request to "http:///login".
func TestImportReportsTheVariablesItNeeds(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "none at all",
			script: "[[[HSRC]]]\nGET http://192.168.1.1/reboot HTTP/1.1\n[[[/HSRC]]]\n",
			want:   nil,
		},
		{
			name:   "the router only",
			script: "[[[HSRC]]]\nGET /reboot HTTP/1.1\nHost: %%%routerip%%%\n[[[/HSRC]]]\n",
			want:   []string{VarRouter},
		},
		{
			name: "all three credentials, in a fixed order",
			script: "[[[HSRC]]]\nPOST /login HTTP/1.1\nHost: %%%routerip%%%\n\n" +
				"p=%%%password%%%&u=%%%username%%%\n[[[/HSRC]]]\n",
			want: []string{VarRouter, VarUsername, VarPassword},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			imp, err := ImportScript(tc.script)
			if err != nil {
				t.Fatalf("ImportScript: %v", err)
			}
			if strings.Join(imp.Variables, ",") != strings.Join(tc.want, ",") {
				t.Errorf("variables = %v, want %v", imp.Variables, tc.want)
			}
		})
	}
}

// TestImportedScriptNeedsARouterAddress pins the other half of that decision:
// the variable exists, so the configuration has to be refused while the field
// behind it is empty. An unset variable expands to nothing, and
// "http:///login.cgi" fails with a URL parse error that names no field at all.
func TestImportedScriptNeedsARouterAddress(t *testing.T) {
	imp, err := ImportScript("[[[HSRC]]]\nGET /reboot HTTP/1.1\nHost: %%%routerip%%%\n[[[/HSRC]]]\n")
	if err != nil {
		t.Fatalf("ImportScript: %v", err)
	}
	cfg := Sanitize(Config{Method: MethodHTTP, Requests: imp.Requests, CheckURL: checkURL})
	err = cfg.Validate()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Validate() = %v, want ErrNotConfigured", err)
	}
	if !strings.Contains(err.Error(), "router") {
		t.Errorf("error %q does not name the field that is empty", err)
	}
	cfg.Router = "192.168.1.1"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with a router set = %v", err)
	}
}

// TestImportShowsWhatMappedAlongsideWhatDidNot: the editor needs both halves, so
// a refused script can still show which of its four blocks was the problem.
func TestImportShowsWhatMappedAlongsideWhatDidNot(t *testing.T) {
	imp, err := ImportScript(
		"[[[HSRC]]]\nGET /login HTTP/1.1\nHost: 192.168.1.1\n[[[/HSRC]]]\n" +
			"[[[HSRC]]]\nGET /reboot?t=%%%nonce%%% HTTP/1.1\nHost: 192.168.1.1\n[[[/HSRC]]]\n")
	if err == nil {
		t.Fatal("a script with an unmappable block was accepted")
	}
	if len(imp.Requests) != 1 {
		t.Fatalf("got %d requests, want the one block that did map", len(imp.Requests))
	}
	if len(imp.Problems) != 1 || imp.Problems[0].Line != 6 {
		t.Fatalf("problems = %+v, want one on line 6", imp.Problems)
	}
	if !strings.Contains(imp.Problems[0].Text, "nonce") {
		t.Errorf("the problem does not quote the line it is about: %+v", imp.Problems[0])
	}
}

// TestSplitCommand covers the quoting rules on their own. The tokens become a
// URL, a body and header values and are never handed to a shell, so a semicolon
// inside a quoted argument has to survive as data.
func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
		bad  bool
	}{
		{name: "plain", in: "curl -X POST http://h/x", want: []string{"curl", "-X", "POST", "http://h/x"}},
		{name: "double quotes", in: `curl -d "a=1&b=2" http://h`, want: []string{"curl", "-d", "a=1&b=2", "http://h"}},
		{name: "single quotes", in: `curl -d 'a=1&b=2' http://h`, want: []string{"curl", "-d", "a=1&b=2", "http://h"}},
		{name: "a space inside quotes", in: `curl -H "X: a b" h`, want: []string{"curl", "-H", "X: a b", "h"}},
		{name: "shell metacharacters stay data", in: `curl -d 'a=;rm -rf /' h`, want: []string{"curl", "-d", "a=;rm -rf /", "h"}},
		{name: "a backslash escape", in: `curl -d a\ b h`, want: []string{"curl", "-d", "a b", "h"}},
		{name: "a backslash inside single quotes is literal", in: `curl -d 'a\b' h`, want: []string{"curl", "-d", `a\b`, "h"}},
		{name: "an empty quoted argument survives", in: `curl -d "" h`, want: []string{"curl", "-d", "", "h"}},
		{name: "runs of whitespace", in: "curl   -d\ta   h", want: []string{"curl", "-d", "a", "h"}},
		{name: "an unclosed double quote", in: `curl -d "a=1 h`, bad: true},
		{name: "an unclosed single quote", in: `curl -d 'a=1 h`, bad: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitCommand(tc.in)
			if tc.bad {
				if err == nil {
					t.Fatalf("splitCommand(%q) = %q, want a refusal", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitCommand(%q): %v", tc.in, err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("splitCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
