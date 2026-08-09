package reconnect

import (
	"errors"
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

// TestPublicIP is the range table. Every row here is an address a check
// response really can carry - a router status page printing the LAN side, a box
// behind carrier-grade NAT, a captive portal answering with its own gateway -
// and every one of them would otherwise be compared against the next reading as
// if it were the public address.
func TestPublicIP(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // "" means the address must be refused
		// names is a word the refusal has to contain, so a reason that says
		// "not public" and nothing else fails here. The user has to be able to
		// tell a LAN address apart from a carrier one from the message alone.
		names string
	}{
		{name: "public v4", body: "203.0.113.9", want: "203.0.113.9"},
		{name: "public v4 in json", body: `{"ip":"8.8.4.4"}`, want: "8.8.4.4"},
		{name: "public v6", body: "2606:4700:4700::1111", want: "2606:4700:4700::1111"},
		{name: "public v6 in prose", body: "Your address is 2001:db8::1234 today", want: "2001:db8::1234"},

		{name: "loopback v4", body: "127.0.0.1", names: "loopback"},
		{name: "loopback v6", body: "::1", names: "loopback"},
		{name: "private 10/8", body: "10.0.0.5", names: "private"},
		{name: "private 172.16/12", body: "172.16.0.1", names: "private"},
		{name: "private 172.31 is still 172.16/12", body: "172.31.255.254", names: "private"},
		{name: "private 192.168/16", body: "192.168.1.1", names: "private"},
		{name: "link-local v4", body: "169.254.10.20", names: "link-local"},
		{name: "link-local v6", body: "fe80::1", names: "link-local"},
		{name: "unique-local fc00", body: "fc00::1", names: "unique-local"},
		{name: "unique-local fd00", body: "fd12:3456:789a::1", names: "unique-local"},
		{name: "cgnat bottom", body: "100.64.0.1", names: "carrier-grade"},
		{name: "cgnat top", body: "100.127.255.255", names: "carrier-grade"},
		{name: "unspecified v4", body: "0.0.0.0", names: "unspecified"},
		{name: "unspecified v6", body: "::", names: "unspecified"},
		// The SSDP group, which is the multicast address most likely to be sitting
		// in a page this package fetches. 224.0.0.0/24 is deliberately not used
		// here: it is link-local as well as multicast, and it is named for the
		// half a user can do something about.
		{name: "multicast", body: "239.255.255.250", names: "multicast"},

		// The edges of the two ranges that are carved out of otherwise public
		// space. Getting either boundary wrong refuses a perfectly good address
		// and leaves the reconnect unusable on that line.
		{name: "just below cgnat", body: "100.63.255.255", want: "100.63.255.255"},
		{name: "just above cgnat", body: "100.128.0.0", want: "100.128.0.0"},
		{name: "just below 172.16/12", body: "172.15.0.1", want: "172.15.0.1"},
		{name: "just above 172.16/12", body: "172.32.0.1", want: "172.32.0.1"},

		// A router web page that prints the LAN address first is the case this
		// whole check exists for.
		{name: "router page echoing the lan side", body: "<td>WAN IP</td><td>192.168.0.10</td>", names: "private"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PublicIP(tc.body)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("PublicIP(%q) accepted %s", tc.body, got)
				}
				if !errors.Is(err, ErrNotPublic) {
					t.Fatalf("PublicIP(%q) failed with %v, want ErrNotPublic", tc.body, err)
				}
				if !strings.Contains(err.Error(), tc.names) {
					t.Errorf("PublicIP(%q) said %q, which never says %q", tc.body, err, tc.names)
				}
				// The address itself belongs in the message: "a private address"
				// with no address in it is a sentence nobody can act on.
				if addr, ok := FindIP(tc.body); ok && !strings.Contains(err.Error(), addr.String()) {
					t.Errorf("PublicIP(%q) said %q, which never names %s", tc.body, err, addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PublicIP(%q) refused a public address: %v", tc.body, err)
			}
			if got.String() != tc.want {
				t.Errorf("PublicIP(%q) = %s, want %s", tc.body, got, tc.want)
			}
		})
	}
}

// TestPublicIPKeepsFindIPsVerdictOnNothing pins the other half of the contract:
// a body with no address in it is still ErrNoAddress, not a range refusal. They
// are two different repairs - a wrong URL against a URL that answers with the
// wrong side of the router - and one error for both sends half the people who
// hit it to the wrong field.
func TestPublicIPKeepsFindIPsVerdictOnNothing(t *testing.T) {
	for _, body := range []string{"", "Access denied. Please log in.", "build 1.2.3.4444"} {
		_, err := PublicIP(body)
		if !errors.Is(err, ErrNoAddress) {
			t.Errorf("PublicIP(%q) failed with %v, want ErrNoAddress", body, err)
		}
	}
}

// TestPublicIPDoesNotScanPastARefusal fixes the tempting shortcut in place: when
// the first address on the page is a LAN one, the answer is a refusal naming it,
// never the next address further down. Reading on is guessing which of two
// addresses the page meant, and a guess here is indistinguishable from a
// successful reconnect for as long as it happens to be right.
func TestPublicIPDoesNotScanPastARefusal(t *testing.T) {
	const body = "LAN 192.168.1.1 · WAN 203.0.113.9"
	got, err := PublicIP(body)
	if err == nil {
		t.Fatalf("PublicIP(%q) picked %s out of a page with a private address first", body, got)
	}
	if strings.Contains(err.Error(), "203.0.113.9") {
		t.Errorf("PublicIP(%q) reported the second address: %v", body, err)
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
