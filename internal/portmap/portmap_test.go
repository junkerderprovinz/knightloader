package portmap

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// scriptedDoer answers both halves of a real attempt, the device description
// GET and the two SOAP POSTs, without opening a socket, as reconnect's own
// tests do: a router discovered on the LAN is not something a test may talk
// to.
//
// Requests are dispatched by host: a GET goes to whichever URL was asked
// for, and a POST is routed to the handler registered for its target host,
// so a test with two gateways can give each one its own script.
type scriptedDoer struct {
	byHost map[string]hostScript
	// requests records every request sent, host included, so a test can
	// assert that a hostile or unreachable candidate was never spoken to
	// rather than only checking the final Result.
	requests []string
}

type hostScript struct {
	description string
	// soap answers a POST by action name. A host with no entry for an
	// action that is called fails the test rather than returning nothing,
	// which would hide a real bug.
	soap map[string]func(reqBody string) (status int, body string)
}

func (d *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	d.requests = append(d.requests, req.Method+" "+req.URL.String())
	sc, ok := d.byHost[host]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}
	if req.Method == http.MethodGet {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sc.description)), Request: req}, nil
	}
	action := actionFromSOAPAction(req.Header.Get("SOAPAction"))
	fn, ok := sc.soap[action]
	if !ok {
		return nil, errUnexpectedAction{host: host, action: action}
	}
	status, body := fn(readBody(req))
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

type errUnexpectedAction struct {
	host, action string
}

func (e errUnexpectedAction) Error() string {
	return "scriptedDoer: " + e.host + " was asked to run " + e.action + ", which this test never scripted"
}

func readBody(req *http.Request) string {
	if req.Body == nil {
		return ""
	}
	b, _ := io.ReadAll(req.Body)
	return string(b)
}

func actionFromSOAPAction(h string) string {
	i := strings.LastIndex(h, "#")
	if i < 0 {
		return ""
	}
	return strings.TrimSuffix(h[i+1:], `"`)
}

// wanIPDescription is a device description with one WANIPConnection service,
// nested the way a real gateway nests it (InternetGatewayDevice, WANDevice,
// WANConnectionDevice). A flat service list passes tests a real parser fails.
func wanIPDescription(controlURL string) string {
	return `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0">` +
		`<device><deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>` +
		`<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>` +
		`<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>` +
		`<serviceList><service>` +
		`<serviceType>` + testServiceType + `</serviceType>` +
		`<controlURL>` + controlURL + `</controlURL>` +
		`</service></serviceList>` +
		`</device></deviceList></device></deviceList></device></root>`
}

// noWANServiceDescription is a gateway that answers the search and the
// description fetch but is not an internet gateway: a printer, a smart-home
// hub, anything else that speaks SSDP.
func noWANServiceDescription() string {
	return `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0">` +
		`<device><deviceType>urn:schemas-upnp-org:device:Basic:1</deviceType></device></root>`
}

func addPortMappingOK(string) (int, string) {
	return http.StatusOK, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<s:Body><u:AddPortMappingResponse xmlns:u="` + testServiceType + `"></u:AddPortMappingResponse></s:Body></s:Envelope>`
}

func faultResponse(code int, desc string) (int, string) {
	return http.StatusInternalServerError, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring>` +
		`<detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">` +
		`<errorCode>` + strconv.Itoa(code) + `</errorCode><errorDescription>` + desc + `</errorDescription>` +
		`</UPnPError></detail></s:Fault></s:Body></s:Envelope>`
}

func fixedDiscoverer(gateways ...reconnect.Gateway) reconnect.Discoverer {
	return func(ctx context.Context, timeout time.Duration) ([]reconnect.Gateway, error) {
		return gateways, nil
	}
}

func failingDiscoverer(err error) reconnect.Discoverer {
	return func(ctx context.Context, timeout time.Duration) ([]reconnect.Gateway, error) {
		return nil, err
	}
}

func fixedLocalIP(addr string) func(context.Context, string) (netip.Addr, error) {
	ip := netip.MustParseAddr(addr)
	return func(context.Context, string) (netip.Addr, error) {
		return ip, nil
	}
}

const testGatewayLocation = "http://192.168.1.1:5000/desc.xml"
const testControlURL = "http://192.168.1.1:5000/ctl/IPConn"

func baseRequest() Request {
	return Request{
		InternalPort: 6881,
		Discover:     fixedDiscoverer(reconnect.Gateway{Location: testGatewayLocation}),
		LocalIP:      fixedLocalIP("192.168.1.50"),
	}
}

// The happy path: the router accepts AddPortMapping and its read-back agrees.
func TestAttemptConfirmsAMappingTheRouterReadsBack(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Confirmed || res.Reason != ReasonConfirmed {
		t.Fatalf("got outcome=%q reason=%q, want confirmed: %+v", res.Outcome, res.Reason, res)
	}
	if res.InternalIP.String() != "192.168.1.50" || res.InternalPort != 6881 || res.ExternalPort != 6881 {
		t.Errorf("Result does not carry the mapping it confirmed: %+v", res)
	}
	if res.Gateway != testGatewayLocation {
		t.Errorf("Gateway = %q, want %q", res.Gateway, testGatewayLocation)
	}
	if res.Protocol != "TCP" {
		t.Errorf("Protocol = %q, want the TCP default", res.Protocol)
	}
}

// A wrong NewInternalClient produces a mapping the router confirms and no peer
// can use. The assertion is on the request body rather than on the Result,
// because a bug that put the wrong value into the envelope while still
// reporting Confirmed would pass every other test in this file.
func TestAttemptSendsTheInjectedLocalIPAsNewInternalClient(t *testing.T) {
	var addBody string
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": func(body string) (int, string) {
					addBody = body
					return addPortMappingOK(body)
				},
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer
	req.LocalIP = fixedLocalIP("192.168.1.77")

	if _, err := Attempt(context.Background(), req); err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if !strings.Contains(addBody, "<NewInternalClient>192.168.1.77</NewInternalClient>") {
		t.Errorf("AddPortMapping body does not carry the resolved local address: %s", addBody)
	}
	if !strings.Contains(addBody, "<NewInternalPort>6881</NewInternalPort>") {
		t.Errorf("AddPortMapping body does not carry the internal port: %s", addBody)
	}
}

// A router that says yes to AddPortMapping and then, when asked, says there is
// no such mapping. That is proof rather than an absence of proof, so Outcome
// says Failed.
func TestAttemptReportsNoSuchEntryAsFailedNotUnconfirmed(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return faultResponse(upnpNoSuchEntry, "NoSuchEntryInArray")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Failed || res.Reason != ReasonNoSuchEntry {
		t.Fatalf("got outcome=%q reason=%q, want failed/noSuchEntry: %+v", res.Outcome, res.Reason, res)
	}
}

// A router that applies the mapping and does not implement the query action.
// That reads as Unconfirmed, never as Failed and never as a plain success.
func TestAttemptReportsAnUnimplementedReadBackAsUnconfirmed(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return faultResponse(401, "Invalid Action")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Unconfirmed || res.Reason != ReasonReadBackUnsupported {
		t.Fatalf("got outcome=%q reason=%q, want unconfirmed/readBackUnsupported: %+v", res.Outcome, res.Reason, res)
	}
}

// A mapping table that already has this external port pointed somewhere else:
// a stale entry from a machine that used to have this address, or a second box
// racing for the same port. The router's answer disagrees with what was
// requested, which is Failed rather than Confirmed with the wrong facts in it.
func TestAttemptReportsAMismatchedReadBackAsFailed(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.200", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Failed || res.Reason != ReasonMismatch {
		t.Fatalf("got outcome=%q reason=%q, want failed/mismatch: %+v", res.Outcome, res.Reason, res)
	}
	if !strings.Contains(res.Detail, "192.168.1.200") {
		t.Errorf("Detail does not name the conflicting address: %q", res.Detail)
	}
}

// A service that refuses the action is not the end of the attempt while
// another candidate remains, as in reconnect's own upnp.
func TestAttemptRollsOverWhenAddPortMappingItselfIsRefused(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": func(string) (int, string) { return faultResponse(606, "Action not authorized") },
			},
		},
		"192.168.1.2": {
			description: wanIPDescription("http://192.168.1.2:5000/ctl/IPConn"),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer
	req.Discover = fixedDiscoverer(
		reconnect.Gateway{Location: testGatewayLocation},
		reconnect.Gateway{Location: "http://192.168.1.2:5000/desc.xml"},
	)

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Confirmed {
		t.Fatalf("got outcome=%q, want confirmed from the second gateway after the first refused: %+v", res.Outcome, res)
	}
	if res.Gateway != "http://192.168.1.2:5000/desc.xml" {
		t.Errorf("Gateway = %q, want the second gateway", res.Gateway)
	}
}

// The ordinary case for anyone not on a UPnP-capable router, such as a box
// behind a consumer modem in bridge mode: Failed and noGateway, not an error
// that reads like a bug in this app.
func TestAttemptReportsNoGatewayHonestly(t *testing.T) {
	req := baseRequest()
	req.HTTP = &scriptedDoer{byHost: map[string]hostScript{}}
	req.Discover = fixedDiscoverer() // none

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Failed || res.Reason != ReasonNoGateway {
		t.Fatalf("got outcome=%q reason=%q, want failed/noGateway: %+v", res.Outcome, res.Reason, res)
	}
}

// ssdpSearch's "could not send the search at all" case, with no route for
// multicast. A different failure from "sent it and nobody answered", but the
// same bucket for a caller that only wants to know whether to suggest UPnP.
func TestAttemptReportsADiscoveryTransportErrorAsNoGateway(t *testing.T) {
	req := baseRequest()
	req.HTTP = &scriptedDoer{byHost: map[string]hostScript{}}
	req.Discover = failingDiscoverer(errBoom)

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Failed || res.Reason != ReasonNoGateway {
		t.Fatalf("got outcome=%q reason=%q, want failed/noGateway: %+v", res.Outcome, res.Reason, res)
	}
	if !strings.Contains(res.Detail, "boom") {
		t.Errorf("Detail lost the underlying discovery error: %q", res.Detail)
	}
}

var errBoom = errBoomType{}

type errBoomType struct{}

func (errBoomType) Error() string { return "boom" }

// A device that answers SSDP and serves a description but is not an internet
// gateway: a smart plug, a media server, anything else that speaks SSDP.
func TestAttemptReportsNoWANServiceAsRefused(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {description: noWANServiceDescription()},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Failed || res.Reason != ReasonRefused {
		t.Fatalf("got outcome=%q reason=%q, want failed/refused: %+v", res.Outcome, res.Reason, res)
	}
	if !strings.Contains(res.Detail, "no WAN connection service") {
		t.Errorf("Detail does not explain why: %q", res.Detail)
	}
}

// A device description naming a controlURL on a different host must not be
// followed. reconnect.WANServices is what enforces that; this proves the
// protection reaches all the way through Attempt.
func TestAttemptNeverCallsAHostileControlURL(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {description: wanIPDescription("http://10.0.0.9:9999/ctl")},
		"10.0.0.9": {
			// If Attempt ever reaches this host, answering with a fault
			// would still make the test fail below on the request log, but
			// the point stands even if this were AddPortMappingOK: nothing
			// this host answers should matter, because nothing should ever
			// ask it.
			description: wanIPDescription("http://10.0.0.9:9999/ctl"),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Failed || res.Reason != ReasonRefused {
		t.Fatalf("got outcome=%q reason=%q, want failed/refused (the hostile service must be dropped, not used): %+v", res.Outcome, res.Reason, res)
	}
	for _, r := range doer.requests {
		if strings.Contains(r, "10.0.0.9") {
			t.Fatalf("a request was sent to the hostile control URL: %s", r)
		}
	}
}

// A caller mistake, which is the only case Attempt's error return is for.
func TestAttemptRefusesAnOutOfRangePort(t *testing.T) {
	for _, p := range []int{0, -1, 65536, 100000} {
		req := baseRequest()
		req.InternalPort = p
		if _, err := Attempt(context.Background(), req); err == nil {
			t.Errorf("internal port %d was accepted", p)
		}
	}
}

func TestAttemptRefusesAnUnknownProtocol(t *testing.T) {
	req := baseRequest()
	req.Protocol = "SCTP"
	if _, err := Attempt(context.Background(), req); err == nil {
		t.Error("protocol \"SCTP\" was accepted")
	}
}

// The two defaults the settings page's "attempt UPnP mapping" button relies
// on: the external port mirrors the internal one, and the protocol is TCP.
func TestAttemptDefaultsExternalPortAndProtocol(t *testing.T) {
	var addBody string
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": func(body string) (int, string) {
					addBody = body
					return addPortMappingOK(body)
				},
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.ExternalPort != 6881 {
		t.Errorf("ExternalPort = %d, want it to default to InternalPort (6881)", res.ExternalPort)
	}
	if !strings.Contains(addBody, "<NewProtocol>TCP</NewProtocol>") {
		t.Errorf("AddPortMapping body does not default the protocol to TCP: %s", addBody)
	}
	if !strings.Contains(addBody, "<NewPortMappingDescription>KnightLoader</NewPortMappingDescription>") {
		t.Errorf("AddPortMapping body does not default the description: %s", addBody)
	}
}

// protocolOf reads NewProtocol back out of a captured AddPortMapping or
// GetSpecificPortMappingEntry request body, so a script can answer the TCP and
// UDP halves of an AttemptPort call differently: both go through the same
// action against the same service, and only the argument tells them apart.
func protocolOf(reqBody string) string {
	if strings.Contains(reqBody, "<NewProtocol>UDP</NewProtocol>") {
		return "UDP"
	}
	return "TCP"
}

// combine's contract: a torrent listen port is not confirmed off one protocol
// alone.
func TestAttemptPortConfirmsOnlyWhenBothProtocolsConfirm(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(body string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := AttemptPort(context.Background(), req)
	if err != nil {
		t.Fatalf("AttemptPort: %v", err)
	}
	if res.Outcome != Confirmed {
		t.Fatalf("got outcome=%q, want confirmed when both TCP and UDP confirm: %+v", res.Outcome, res)
	}
	if !strings.Contains(res.Detail, "TCP:") || !strings.Contains(res.Detail, "UDP:") {
		t.Errorf("Detail does not name both protocols: %q", res.Detail)
	}
}

// One protocol's read-back cannot be confirmed while the other's can, and the
// combined answer must not report the better half only.
func TestAttemptPortDowngradesToTheWorseProtocol(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(body string) (int, string) {
					if protocolOf(body) == "UDP" {
						return faultResponse(401, "Invalid Action")
					}
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := AttemptPort(context.Background(), req)
	if err != nil {
		t.Fatalf("AttemptPort: %v", err)
	}
	if res.Outcome != Unconfirmed {
		t.Fatalf("got outcome=%q, want unconfirmed (TCP confirmed, UDP did not): %+v", res.Outcome, res)
	}
	if !strings.Contains(res.Detail, "TCP: confirmed") {
		t.Errorf("Detail does not credit the confirmed TCP half: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "UDP: unconfirmed") {
		t.Errorf("Detail does not name the unconfirmed UDP half: %q", res.Detail)
	}
}

// AddPortMapping refuses one protocol outright, which is what a router that
// maps TCP and never implemented UDP forwarding does. Failed wins over
// Confirmed, the same way it wins over Unconfirmed.
func TestAttemptPortReportsFailedWhenEitherProtocolIsRefused(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": func(body string) (int, string) {
					if protocolOf(body) == "UDP" {
						return faultResponse(606, "Action not authorized")
					}
					return addPortMappingOK(body)
				},
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer

	res, err := AttemptPort(context.Background(), req)
	if err != nil {
		t.Fatalf("AttemptPort: %v", err)
	}
	if res.Outcome != Failed {
		t.Fatalf("got outcome=%q, want failed (UDP was refused outright): %+v", res.Outcome, res)
	}
}

// As with reconnect.Config.UPnPLocation: a network whose multicast is filtered
// but whose gateway is reachable still works, and pinning skips discovery
// rather than preferring the pinned address.
func TestAttemptHonoursAPinnedLocationWithoutDiscovering(t *testing.T) {
	doer := &scriptedDoer{byHost: map[string]hostScript{
		"192.168.1.1": {
			description: wanIPDescription(testControlURL),
			soap: map[string]func(string) (int, string){
				"AddPortMapping": addPortMappingOK,
				"GetSpecificPortMappingEntry": func(string) (int, string) {
					return http.StatusOK, getEntryResponse("u", "192.168.1.50", "6881")
				},
			},
		},
	}}
	req := baseRequest()
	req.HTTP = doer
	req.Location = testGatewayLocation
	req.Discover = func(ctx context.Context, timeout time.Duration) ([]reconnect.Gateway, error) {
		t.Fatal("Discover was called despite a pinned Location")
		return nil, nil
	}

	res, err := Attempt(context.Background(), req)
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if res.Outcome != Confirmed {
		t.Fatalf("got outcome=%q, want confirmed: %+v", res.Outcome, res)
	}
}
