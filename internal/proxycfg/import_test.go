package proxycfg

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseLineReadsTheFormsListsAreWrittenIn covers the shapes real suppliers
// send, not only the documented one.
func TestParseLineReadsTheFormsListsAreWrittenIn(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Entry
	}{
		{
			name: "the documented form",
			line: "socks5://alice:secret@proxy.example.org:1080",
			want: Entry{Kind: KindSOCKS5, Host: "proxy.example.org", Port: 1080, Username: "alice", Password: "secret", Enabled: true},
		},
		{
			name: "no credentials at all",
			line: "http://proxy.lan:8080",
			want: Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true},
		},
		{
			name: "a user id with no password, which is all socks4 has",
			line: "socks4a://alice@10.0.0.5:1080",
			want: Entry{Kind: KindSOCKS4A, Host: "10.0.0.5", Port: 1080, Username: "alice", Enabled: true},
		},
		{
			// The last @ separates, so this is a password and not a second host.
			name: "a password containing an at sign",
			line: "http://alice:p@ss@proxy.lan:8080",
			want: Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Username: "alice", Password: "p@ss", Enabled: true},
		},
		{
			// The first colon separates, so the rest is all password.
			name: "a password containing a colon",
			line: "https://alice:a:b:c@proxy.lan:8443",
			want: Entry{Kind: KindHTTPS, Host: "proxy.lan", Port: 8443, Username: "alice", Password: "a:b:c", Enabled: true},
		},
		{
			name: "an IPv6 endpoint",
			line: "socks5://[2001:db8::5]:1080",
			want: Entry{Kind: KindSOCKS5, Host: "2001:db8::5", Port: 1080, Enabled: true},
		},
		{
			name: "case and stray spacing",
			line: "  HTTP://Proxy.LAN:8080  ",
			want: Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true},
		},
		{
			// Every socks5 entry lets the proxy resolve the name anyway.
			name: "socks5h is the same proxy",
			line: "socks5h://proxy.lan:1080",
			want: Entry{Kind: KindSOCKS5, Host: "proxy.lan", Port: 1080, Enabled: true},
		},
		{
			name: "a trailing slash somebody's browser added",
			line: "http://proxy.lan:8080/",
			want: Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseLine(c.line)
			if err != nil {
				t.Fatalf("ParseLine(%q) refused it: %v", c.line, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseLine(%q)\n got %+v\nwant %+v", c.line, got, c.want)
			}
		})
	}
}

// TestParseLineRefusesWithSomethingToActOn: every refusal names what is wrong,
// and those with a mechanical fix write out the corrected line.
func TestParseLineRefusesWithSomethingToActOn(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		contains []string
	}{
		{
			// Guessing http could send SOCKS traffic to an HTTP proxy.
			name:     "no scheme is not assumed to be http",
			line:     "proxy.lan:8080",
			contains: []string{"no connection type", "socks5://"},
		},
		{
			name:     "the other list format is named, with the line rewritten",
			line:     "http://10.0.0.5:8080:alice:secret",
			contains: []string{"host:port:user:pass", "http://alice:secret@10.0.0.5:8080"},
		},
		{
			name:     "socks without a version",
			line:     "socks://proxy.lan:1080",
			contains: []string{"not a version", "socks4://", "socks5://"},
		},
		{
			name:     "an unknown scheme lists the ones that work",
			line:     "ftp://proxy.lan:21",
			contains: []string{`"ftp"`, "socks4a"},
		},
		{
			// clean would otherwise drop the password silently.
			name:     "socks4 with a password says the protocol has no field for it",
			line:     "socks4://alice:secret@proxy.lan:1080",
			contains: []string{"no password field", "socks4://alice@proxy.lan:1080"},
		},
		{
			name:     "no port",
			line:     "http://proxy.lan",
			contains: []string{"no port", "proxy.lan:PORT"},
		},
		{
			name:     "a port that is not a number",
			line:     "http://proxy.lan:eighty",
			contains: []string{`"eighty"`, "not a number"},
		},
		{
			name:     "a port outside the range, in the validator's own words",
			line:     "http://proxy.lan:70000",
			contains: []string{"70000", "1-65535"},
		},
		{
			name:     "an unbracketed IPv6 address",
			line:     "socks5://2001:db8::5",
			contains: []string{"bracket", "[::1]:1080"},
		},
		{
			name:     "a whole URL",
			line:     "http://proxy.lan:8080/proxy.pac",
			contains: []string{"not an address", "/proxy.pac"},
		},
		{
			name:     "a password with no user name",
			line:     "http://:secret@proxy.lan:8080",
			contains: []string{"no user name", "user:pass@host:port"},
		},
		{
			name:     "an empty credentials section",
			line:     "http://@proxy.lan:8080",
			contains: []string{"no user name before the @"},
		},
		{
			// A direct row is added by hand, not imported.
			name:     "direct is not something a list can contain",
			line:     "direct://nas.local:0",
			contains: []string{"direct", "by hand"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseLine(c.line)
			if err == nil {
				t.Fatalf("ParseLine(%q) accepted it as %+v", c.line, got)
			}
			for _, want := range c.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q:\n%s", want, err)
				}
			}
		})
	}
}

// TestParseListPointsAtTheRightLine: an off-by-one would point at a line that
// is fine.
func TestParseListPointsAtTheRightLine(t *testing.T) {
	text := strings.Join([]string{
		"# the ones from the first order",
		"",
		"http://proxy.lan:8080",
		"proxy.lan:9090",
		"socks5://alice:secret@proxy.example.org:1080",
		"   ",
		"// leftover note",
		"socks4://alice:secret@proxy.lan:1080",
	}, "\r\n") // many lists are written on Windows

	got := ParseList(text, nil)
	if len(got.Entries) != 2 {
		t.Fatalf("accepted %d entries, want 2: %+v", len(got.Entries), got.Entries)
	}
	if len(got.Rejected) != 2 {
		t.Fatalf("refused %d lines, want 2: %+v", len(got.Rejected), got.Rejected)
	}
	if got.Rejected[0].Line != 4 {
		t.Errorf("the schemeless line is reported at %d, want 4", got.Rejected[0].Line)
	}
	if got.Rejected[1].Line != 8 {
		t.Errorf("the socks4 line is reported at %d, want 8", got.Rejected[1].Line)
	}
}

// TestParseListNeverEchoesTheLine: a rejected line may hold a password in the
// clear, and rejections end up in logs and screenshots.
func TestParseListNeverEchoesTheLine(t *testing.T) {
	const secret = "hunter2-schwarzpulver"
	// Refused for the port, before the password is parsed out.
	got := ParseList("http://alice:"+secret+"@proxy.lan:notaport", nil)
	if len(got.Rejected) != 1 {
		t.Fatalf("refused %d lines, want 1", len(got.Rejected))
	}
	if strings.Contains(got.Rejected[0].Reason, secret) {
		t.Errorf("the refusal carries the password: %q", got.Rejected[0].Reason)
	}
}

// TestParseListRefusesDuplicatesRatherThanDoublingAShare: a connection listed
// twice would take two turns in the rotation for every one the others get.
func TestParseListRefusesDuplicatesRatherThanDoublingAShare(t *testing.T) {
	existing := Sanitize([]Entry{{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true}})

	got := ParseList(strings.Join([]string{
		"http://proxy.lan:8080",                 // already stored
		"socks5://proxy.example.org:1080",       // new
		"socks5://PROXY.example.org:1080",       // the same one again, spelled differently
		"socks5://alice@proxy.example.org:1080", // a different user is a different connection
	}, "\n"), existing)

	if len(got.Entries) != 2 {
		t.Fatalf("accepted %d entries, want 2: %+v", len(got.Entries), got.Entries)
	}
	if len(got.Rejected) != 2 {
		t.Fatalf("refused %d lines, want 2: %+v", len(got.Rejected), got.Rejected)
	}
	// A duplicate of a stored row and one of an earlier line read differently.
	if !strings.Contains(got.Rejected[0].Reason, "already in the list") {
		t.Errorf("line 1 should say it is already configured: %q", got.Rejected[0].Reason)
	}
	if !strings.Contains(got.Rejected[1].Reason, "line 2") {
		t.Errorf("line 3 should point at the line that claimed it: %q", got.Rejected[1].Reason)
	}
}

// TestParseListSurvivesSanitize: an accepted line must not be silently dropped
// by Sanitize on the way to disk.
func TestParseListSurvivesSanitize(t *testing.T) {
	got := ParseList(strings.Join([]string{
		"http://proxy.lan:8080",
		"socks5://alice:secret@proxy.example.org:1080",
		"https://[2001:db8::5]:8443",
		"socks4a://alice@10.0.0.5:1080",
	}, "\n"), nil)
	if len(got.Rejected) != 0 {
		t.Fatalf("refused %+v", got.Rejected)
	}
	if n := len(Sanitize(got.Entries)); n != len(got.Entries) {
		t.Errorf("Sanitize kept %d of %d imported entries", n, len(got.Entries))
	}
}

// TestParseListIsAlwaysCountable: both slices are non-nil even for an empty
// paste.
func TestParseListIsAlwaysCountable(t *testing.T) {
	got := ParseList("", nil)
	if got.Entries == nil || got.Rejected == nil {
		t.Errorf("an empty paste answered with a nil half: %+v", got)
	}
}
