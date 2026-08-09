package reconnect

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// routeHeader is the header line the kernel writes. It is included in every
// fixture because a parser that only works on a file with the header stripped is
// a parser that works on no real machine.
const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

// route builds one row. The addresses are written the way /proc/net/route writes
// them: little-endian hexadecimal, so 192.168.1.1 is "0101A8C0".
func route(iface, dest, gateway, flags string, metric int) string {
	return iface + "\t" + dest + "\t" + gateway + "\t" + flags + "\t0\t0\t" +
		strconv.Itoa(metric) + "\t00000000\t0\t0\t0\n"
}

// TestParseProcNetRoute pins every reason a row is passed over. The rejections
// are the point: an address offered to the user as "your router" that is not one
// gets typed into the form, and the first reconnect posts the router password to
// whatever answers at that address.
func TestParseProcNetRoute(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		want      string // "" means the read must fail
		wantIface string
	}{
		{
			name:      "one default route",
			table:     routeHeader + route("eth0", "00000000", "0101A8C0", "0003", 0),
			want:      "192.168.1.1",
			wantIface: "eth0",
		},
		{
			// A box on wifi and ethernet at once has two. Offering the gateway of
			// the link that is not carrying traffic sends the login out of the
			// wrong interface, where it either times out or reaches a stranger.
			name: "lowest metric wins",
			table: routeHeader +
				route("wlan0", "00000000", "0102A8C0", "0003", 600) +
				route("eth0", "00000000", "0101A8C0", "0003", 100),
			want:      "192.168.1.1",
			wantIface: "eth0",
		},
		{
			name: "the first default route is not automatically the answer",
			table: routeHeader +
				route("eth0", "00000000", "0101A8C0", "0003", 100) +
				route("wlan0", "00000000", "0102A8C0", "0003", 20),
			want:      "192.168.2.1",
			wantIface: "wlan0",
		},
		{
			// The on-link route to the local subnet has no next hop. Matching on
			// the gateway column alone would offer the first neighbour instead.
			name: "an on-link route is not a default route",
			table: routeHeader +
				route("eth0", "0001A8C0", "00000000", "0001", 0),
			want: "",
		},
		{
			// A host route through a gateway is a route to one machine, not the
			// way out. Reading it as the default is how a VPN peer's address ends
			// up in the router field.
			name: "a non-default destination is skipped even with a gateway",
			table: routeHeader +
				route("tun0", "0A0A0A0A", "0101A8C0", "0003", 0),
			want: "",
		},
		{
			name:  "a route that is not up is skipped",
			table: routeHeader + route("eth0", "00000000", "0101A8C0", "0002", 0),
			want:  "",
		},
		{
			name:  "a route with no gateway flag is skipped",
			table: routeHeader + route("eth0", "00000000", "0101A8C0", "0001", 0),
			want:  "",
		},
		{
			name:  "a loopback gateway is refused",
			table: routeHeader + route("lo", "00000000", "0100007F", "0003", 0),
			want:  "",
		},
		{
			name:  "an unspecified gateway is refused",
			table: routeHeader + route("eth0", "00000000", "00000000", "0003", 0),
			want:  "",
		},
		{
			name:  "unreadable flags are skipped rather than guessed at",
			table: routeHeader + route("eth0", "00000000", "0101A8C0", "zzzz", 0),
			want:  "",
		},
		{
			name:  "a truncated row is skipped",
			table: routeHeader + "eth0\t00000000\t0101A8C0\n",
			want:  "",
		},
		{
			name:  "nothing but the header",
			table: routeHeader,
			want:  "",
		},
		{
			name:  "an empty file",
			table: "",
			want:  "",
		},
		{
			// The container case, which is the one that will be reported as a
			// bug: the answer is the bridge, not the router. It is still given
			// back, with the interface name that says so.
			name:      "a docker bridge is reported with its interface",
			table:     routeHeader + route("eth0", "00000000", "010011AC", "0003", 0),
			want:      "172.17.0.1",
			wantIface: "eth0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProcNetRoute(strings.NewReader(tc.table))
			if tc.want == "" {
				if err == nil {
					t.Fatalf("parseProcNetRoute found %s, want a failure", got.Address)
				}
				if !errors.Is(err, ErrGatewayUnavailable) {
					t.Errorf("error = %v, want ErrGatewayUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProcNetRoute: %v", err)
			}
			if got.Address.String() != tc.want {
				t.Errorf("address = %s, want %s", got.Address, tc.want)
			}
			if got.Interface != tc.wantIface {
				t.Errorf("interface = %q, want %q", got.Interface, tc.wantIface)
			}
		})
	}
}

// TestParseHexAddrByteOrder is separate because getting it backwards produces an
// address that parses, prints and looks entirely reasonable - 1.1.168.192 - and
// is not the router.
func TestParseHexAddrByteOrder(t *testing.T) {
	tests := []struct {
		hex  string
		want string // "" means it must be refused
	}{
		{"0101A8C0", "192.168.1.1"},
		{"FE01A8C0", "192.168.1.254"},
		{"010011AC", "172.17.0.1"},

		// The same eight characters read the other way round are a different
		// address that parses and prints perfectly, which is why the byte order
		// gets a case of its own rather than being left to the rows above.
		{"00000001", "1.0.0.0"},

		{"0100007F", ""},   // loopback
		{"00000000", ""},   // unspecified
		{"010000E0", ""},   // 224.0.0.1, multicast
		{"01000000", ""},   // 0.0.0.1, in the unassigned 0.0.0.0/8 range
		{"0101A8", ""},     // too short
		{"0101A8C000", ""}, // too long
		{"0101A8CZ", ""},   // not hexadecimal
		{"", ""},           // empty
		{"0101A8C0 ", ""},  // trailing space, which Fields would already have cut
		{"\t0101A8C0", ""}, // and leading whitespace
		{"0x0101A8C0", ""}, // a prefix the kernel never writes
	}
	for _, tc := range tests {
		t.Run(tc.hex, func(t *testing.T) {
			got, ok := parseHexAddr(tc.hex)
			if tc.want == "" {
				if ok {
					t.Fatalf("parseHexAddr(%q) = %s, want a refusal", tc.hex, got)
				}
				return
			}
			if !ok {
				t.Fatalf("parseHexAddr(%q) was refused, want %s", tc.hex, tc.want)
			}
			if got.String() != tc.want {
				t.Errorf("parseHexAddr(%q) = %s, want %s", tc.hex, got, tc.want)
			}
		})
	}
}

// TestDefaultGatewayNeverGuesses is the whole contract of this file on a
// platform whose routing table cannot be read. A form pre-filled with a
// plausible 192.168.1.1 gets accepted, and the first reconnect sends the router
// password to whatever happens to live there.
func TestDefaultGatewayNeverGuesses(t *testing.T) {
	got, err := DefaultGateway()
	if runtime.GOOS == "linux" {
		// On Linux either answer is legitimate - a build machine may genuinely
		// have no default route - so only the shape is pinned.
		if err != nil && !errors.Is(err, ErrGatewayUnavailable) {
			t.Fatalf("DefaultGateway: %v, want ErrGatewayUnavailable or an address", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("DefaultGateway answered %s on %s, where it cannot know", got.Address, runtime.GOOS)
	}
	if !errors.Is(err, ErrGatewayUnavailable) {
		t.Errorf("error = %v, want ErrGatewayUnavailable", err)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Errorf("error %q does not name the platform it cannot read", err)
	}
	if got.Address.IsValid() {
		t.Errorf("an address came back anyway: %s", got.Address)
	}
	for _, guess := range []string{"192.168.", "10.0.0.", "172.16."} {
		if strings.Contains(err.Error(), guess) {
			t.Errorf("the error suggests %s, which is a guess dressed up as an answer", guess)
		}
	}
}
