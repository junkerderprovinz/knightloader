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

// scriptedDoer answers both halves of a real attempt - the device
// description GET and the two SOAP POSTs - without ever opening a socket,
// the same discipline reconnect's own tests hold to (see upnp_test.go's
// descDoer) and for the same reason: a router discovered on the LAN is not
// something any test here may actually talk to.
//
// Requests are dispatched by host: a GET goes to whichever URL was asked
// for, and a POST is routed to the handler registered for its target host,
// so a test with two gateways can give each one its own script.
type scriptedDoer struct {
	byHost map[string]hostScript
	// requests records every request actually sent, host included, so a
	// test can assert a hostile or unreachable candidate was never spoken
	// to at all rather than only checking the final Result.
	requests []string
}

type hostScript struct {
	description string
	// soap answers a POST by action name. A host with no entry for an
	// action that is actually called fails the test loudly rather than
	// silently, because a nil handler here has silently hidden a real bug
	// in this suite before.
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

// wanIPDescription is a device description with one WANIPConnection
// service, nested the way a real gateway nests it - InternetGatewayDevice >
// WANDevice > WANConnectionDevice - which upnp_test.go's own description()
// helper notes is load-bearing: a flat service list passes tests a real
// parser fails.
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
// description fetch, but is not an internet gateway at all - a printer, a
// smart-home hub, anything else that speaks SSDP.
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

// TestAttemptConfirmsAMappingTheRouterReadsBack is the happy path: the
// router accepts AddPortMapping and its own read-back agrees.
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

// TestAttemptSendsTheInjectedLocalIPAsNewInternalClient is the check that
// closes the loop between localIPFor's own job and what actually reaches
// the router: a wrong NewInternalClient produces a mapping the router
// confirms and no real peer can use, silently. This asserts on the literal
// request body, not just on the Result, because a bug that put the wrong
// value into the envelope while still reporting Confirmed would pass every
// other test in this file.
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

// TestAttemptReportsNoSuchEntryAsFailedNotUnconfirmed is the one case this
// whole package exists to get right: a router that says yes to
// AddPortMapping and then, when asked, says there is no such mapping. That
// is proof, not merely an absence of proof, and Outcome must say Failed.
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

// TestAttemptReportsAnUnimplementedReadBackAsUnconfirmed covers the common,
// non-failure case docs/torrent-support.md's own risk note names: a router
// that applies the mapping and simply does not implement the query action.
// This must read as Unconfirmed, never as Failed and never as a plain
// success.
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

// TestAttemptReportsAMismatchedReadBackAsFailed covers a mapping table that
// already has this external port pointed somewhere else - a stale entry
// from a machine that used to have this address, or a second box on the
// network racing for the same port. The router's own answer disagrees with
// what was just requested, which is a Failed, not a Confirmed with the
// wrong facts in it.
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

// TestAttemptRollsOverWhenAddPortMappingItselfIsRefused is the case
// reconnect's own upnp() already handles for ForceTermination/
// RequestConnection: a service that flatly refuses the action is not the
// end of the attempt while another candidate remains.
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

// TestAttemptReportsNoGatewayHonestly is the ordinary case for anyone not on
// a UPnP-capable router at all - most self-hosted boxes behind a consumer
// modem in bridge mode, notably. It must be Failed/noGateway, not an error
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

// TestAttemptReportsADiscoveryTransportErrorAsNoGateway covers ssdpSearch's
// own "could not even send the search" case (no route for multicast at
// all), which is a different shape of failure from "sent it and nobody
// answered" but must land in the same honest bucket for a caller that only
// wants to know whether to suggest UPnP at all.
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

// TestAttemptReportsNoWANServiceAsRefused covers a device that answers SSDP
// and serves a description, but is not an internet gateway - a smart plug,
// a media server, anything else on the LAN that happens to speak SSDP too.
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

// TestAttemptNeverCallsAHostileControlURL is the adversarial case this
// package inherits from reconnect.WANServices rather than having to prove
// again from scratch (see upnp_test.go's own
// TestWANServicesKeepsTheControlURLOnTheHostThatAnswered): a device
// description naming a controlURL on a different host must never be
// followed. This test exists to prove the protection actually reaches all
// the way through Attempt, not only through reconnect's own package - the
// same class of gap the Wave 10 file-containment lesson this wave was
// briefed on turned out to be.
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

// TestAttemptRefusesAnOutOfRangePort and its protocol counterpart are the
// "caller made a mistake" errors, the only case Attempt's error return is
// for - see Attempt's own doc comment.
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

// TestAttemptDefaultsExternalPortAndProtocol pins the two conveniences a
// caller (the settings page's "attempt UPnP mapping" button, specifically)
// relies on rather than having to fill in: the external port mirrors the
// internal one, and the protocol defaults to TCP.
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
// GetSpecificPortMappingEntry request body, so a script can answer the TCP
// half and the UDP half of an AttemptPort call differently - both go
// through the same action name against the same service, and only the
// argument tells them apart.
func protocolOf(reqBody string) string {
	if strings.Contains(reqBody, "<NewProtocol>UDP</NewProtocol>") {
		return "UDP"
	}
	return "TCP"
}

// TestAttemptPortConfirmsOnlyWhenBothProtocolsConfirm is combine's own
// contract: a torrent listen port is not usably confirmed off one protocol
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

// TestAttemptPortDowngradesToTheWorseProtocol is the case that matters most:
// one protocol's read-back cannot be confirmed while the other's can, and
// the combined answer must not quietly report the better half only.
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

// TestAttemptPortReportsFailedWhenEitherProtocolIsRefused covers the
// stronger case: AddPortMapping itself refuses one protocol outright (a
// router that maps TCP but has never implemented UDP forwarding, which is a
// real firmware gap, not a hypothetical one). Failed must win over
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

// TestAttemptHonoursAPinnedLocationWithoutDiscovering mirrors
// reconnect.Config.UPnPLocation: a network whose multicast is filtered but
// whose gateway is otherwise reachable must still work, and pinning it must
// skip discovery outright rather than merely preferring the pinned address.
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
