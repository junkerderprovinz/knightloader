package reconnect

import (
	"strings"
	"testing"
)

// TestFindIP is the table that matters most in this package: everything downstream
// trusts this answer, and a wrong address here is reported to the user as a
// successful reconnect. The negative cases are therefore the point of the test,
// not an afterthought.
func TestFindIP(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // "" means nothing should be found
	}{
		{"bare address", "203.0.113.9", "203.0.113.9"},
		{"trailing newline", "203.0.113.9\n", "203.0.113.9"},
		{"surrounded by prose", "Your IP address is 203.0.113.9 right now", "203.0.113.9"},
		{"end of a sentence", "You are at 203.0.113.9.", "203.0.113.9"},
		{"json", `{"ip":"203.0.113.9","country":"DE"}`, "203.0.113.9"},
		{"html", "<html><body><p>203.0.113.9</p></body></html>", "203.0.113.9"},
		{"with a port", "WAN: 203.0.113.9:8080", "203.0.113.9"},
		{"first of several wins", "203.0.113.9 198.51.100.7", "203.0.113.9"},
		{"ipv6", "2001:db8::1234", "2001:db8::1234"},
		{"ipv6 in json", `{"ip":"2001:db8::1"}`, "2001:db8::1"},
		{"ipv6 loopback", "::1", "::1"},
		{"ipv6 trailing colons", "1::", "1::"},

		// An IPv4-mapped literal has to come back as the plain address. A check
		// page that alternates between the two spellings would otherwise look
		// like an address that keeps changing, and every reconnect would be
		// reported as a success.
		{"ipv4 mapped is unmapped", "::ffff:203.0.113.9", "203.0.113.9"},

		{"empty body", "", ""},
		{"no address at all", "Access denied. Please log in.", ""},
		{"version number", "KnightLoader 1.2.3", ""},
		{"four-part version is not an address", "build 1.2.3.4444", ""},
		{"css colour", "body { color: #abcdef; }", ""},
		{"date", "2026-08-06", ""},
		{"octet out of range", "999.999.999.999", ""},
		{"three octets", "203.0.113", ""},
		{"hex word", "deadbeef", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FindIP(tc.body)
			if tc.want == "" {
				if ok {
					t.Fatalf("FindIP(%q) found %s, want nothing", tc.body, got)
				}
				return
			}
			if !ok {
				t.Fatalf("FindIP(%q) found nothing, want %s", tc.body, tc.want)
			}
			if got.String() != tc.want {
				t.Errorf("FindIP(%q) = %s, want %s", tc.body, got, tc.want)
			}
		})
	}
}

// TestFindIPSkipsOverlongRuns pins the cheap guard against a body that is one
// enormous run of address-shaped bytes.
func TestFindIPSkipsOverlongRuns(t *testing.T) {
	blob := strings.Repeat("abcdef0123456789:.", 64)
	if _, ok := FindIP(blob); ok {
		t.Error("an overlong run was parsed as an address")
	}
	if _, ok := FindIP(blob + " 203.0.113.9"); !ok {
		t.Error("a real address after an overlong run was missed")
	}
}

// TestDropPartialTail is the truncation guard. Cutting "203.0.113.99" mid-way
// leaves a perfectly valid, entirely wrong address, so the last run of a
// truncated body is thrown away rather than parsed.
func TestDropPartialTail(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"cut address is dropped", "ip is 203.0.113.9", "ip is "},
		{"complete-looking tail is dropped too", "ip is 203.0.113.99", "ip is "},
		{"nothing to drop", "ip is 203.0.113.9 <", "ip is 203.0.113.9 <"},
		{"all one run", "203.0.113.9", ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(dropPartialTail([]byte(tc.body))); got != tc.want {
				t.Errorf("dropPartialTail(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
