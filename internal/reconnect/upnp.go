package reconnect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// The two ways the UPnP method fails, kept apart because they send the user to
// different places.
//
// ErrNoGateway means nothing answered the search: the router has UPnP switched
// off, or the search never left the machine - which is the normal case in a
// container on a bridge network, where multicast does not cross to the LAN.
// ErrUPnPRefused means a gateway did answer and then would not do it, which is a
// setting on the router ("allow UPnP control" / "allow user to reconfigure").
// Reporting both as one error sends half the people who hit it to the wrong
// screen, and there is no way for them to tell from the outside which half.
var (
	ErrNoGateway   = errors.New("reconnect: no UPnP gateway answered")
	ErrUPnPRefused = errors.New("reconnect: the UPnP gateway refused to reconnect")
)

// Gateway is one device that answered an SSDP search.
type Gateway struct {
	// Location is the device description URL from the LOCATION header.
	Location string `json:"location"`
	// Server is the SERVER header verbatim, so an error can name the firmware
	// that refused rather than only the address it refused from.
	Server string `json:"server,omitempty"`
}

// Discoverer finds UPnP gateways on the local network. It is an injected
// function so that no test in this package ever opens a socket: the default
// sends multicast, which a test machine may not even be allowed to do.
//
// It must return within timeout. A search that waits for a reply that is never
// coming is the difference between a reconnect that fails in three seconds and a
// download queue that is stopped until somebody notices.
type Discoverer func(ctx context.Context, timeout time.Duration) ([]Gateway, error)

// The SSDP wire constants, from the UPnP Device Architecture.
const (
	ssdpAddr = "239.255.255.250:1900"

	// ssdpTimeout is how long the search listens. The specification has devices
	// answer within their advertised MX window, so waiting much longer only adds
	// dead time to a failing reconnect.
	ssdpTimeout = 3 * time.Second

	// ssdpMX is the delay window handed to the devices, in seconds. It is
	// smaller than ssdpTimeout so a device that waits the whole window still
	// gets its answer back before the search stops listening.
	ssdpMX = 2

	// maxSSDPDatagram is the read buffer. An SSDP answer is a handful of header
	// lines; anything longer is not one.
	maxSSDPDatagram = 4 << 10

	// maxGateways stops the search early on a network where hundreds of devices
	// answer. Only a gateway is useful and they are always among the first.
	maxGateways = 8
)

// ssdpSearchTargets are the search targets tried, in order of how specific they
// are. The service targets are included because some firmware answers a search
// for the service it implements and ignores a search for the device type.
var ssdpSearchTargets = []string{
	"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// The service types that can drop a WAN connection, matched by prefix so that
// version 2 of either is picked up without another constant here. Routers expose
// the PPP flavour for a dial-up style WAN (PPPoE) and the IP flavour for
// everything else, and a fair number expose both with only one of them live.
const (
	wanIPPrefix  = "urn:schemas-upnp-org:service:WANIPConnection:"
	wanPPPPrefix = "urn:schemas-upnp-org:service:WANPPPConnection:"
)

// The two actions, in the order they are used.
const (
	actionForceTermination  = "ForceTermination"
	actionRequestConnection = "RequestConnection"
)

// upnpSettleDelay is the pause between dropping the connection and asking for a
// new one. Firmware that is handed both back to back frequently answers the
// second with "connection in use" and then never dials, because the first one is
// still being torn down.
const upnpSettleDelay = 2 * time.Second

// maxDescriptionBody caps the device description read. A description is a few
// kilobytes of XML; a device that answers the search and then streams is not
// going to be reconnected by reading all of it.
const maxDescriptionBody = 256 << 10

// maxSOAPBody caps a SOAP answer, which is read only to name the fault in it.
const maxSOAPBody = 64 << 10

// upnp asks the gateway to drop and re-establish the WAN connection.
//
// This is the method that works for a user who knows nothing about their router,
// which is exactly why it has to be honest about how it failed: everything else
// in this package fails because of something the user typed, and this one fails
// because of something on a device they have never opened the settings of.
func (r *Reconnector) upnp(ctx context.Context, cfg Config) error {
	gateways, err := r.gateways(ctx, cfg)
	if err != nil {
		return err
	}
	if len(gateways) == 0 {
		return fmt.Errorf("%w: the SSDP search found nothing in %s", ErrNoGateway, ssdpTimeout)
	}

	// Every reason is collected and reported together. A box with two gateways
	// on it - a modem and a router, or one real device answering twice under two
	// service types - would otherwise report whichever happened to be tried last
	// and hide the one that was nearly right.
	var refusals []string
	for _, g := range gateways {
		services, err := r.wanServices(ctx, g)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("%s: %v", g.Location, err))
			continue
		}
		if len(services) == 0 {
			refusals = append(refusals, fmt.Sprintf("%s: answered the search but exposes no WAN connection service", g.Location))
			continue
		}
		for _, svc := range services {
			if err := r.upnpDisconnect(ctx, svc); err != nil {
				refusals = append(refusals, fmt.Sprintf("%s: %v", svc.ServiceType, err))
				continue
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrUPnPRefused, strings.Join(refusals, "; "))
}

// gateways is the list of devices to try, either the one the user pinned or
// whatever answers the search.
func (r *Reconnector) gateways(ctx context.Context, cfg Config) ([]Gateway, error) {
	if cfg.UPnPLocation != "" {
		// A pinned location skips discovery entirely, so a network that filters
		// multicast is not a dead end. It is not merged with the discovered set:
		// pinning means "use this one", and quietly trying others as well would
		// make the field look like it had no effect.
		return []Gateway{{Location: cfg.UPnPLocation}}, nil
	}
	found, err := r.discover(ctx, ssdpTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoGateway, err)
	}
	return found, nil
}

// Service is one control endpoint, already resolved to an absolute URL and
// pinned to the host that answered the original SSDP search - see the
// security note in WANServices for why that pin has to travel with the
// endpoint rather than be redone by whoever calls it.
//
// Exported, with exported fields: internal/portmap sends its own SOAP action
// (AddPortMapping) against the same kind of endpoint this package already
// finds for ForceTermination and RequestConnection, and it must not
// re-implement the discovery that finds it - a second, unreviewed SSDP and
// device-description parser is exactly the kind of duplicate this type
// exists to prevent.
type Service struct {
	ServiceType string
	ControlURL  string
}

// upnpDisconnect runs the two actions against one service.
//
// ForceTermination is the one that does the work and RequestConnection is what
// brings the line back up. The order matters and so does what is done with each
// failure: a router that took the termination has already dropped the line, so a
// refused RequestConnection is not a failed reconnect - most firmware redials on
// its own and answers the second action with "already connecting". Failing the
// run there would report a reconnect that did happen as one that did not, and
// the caller would go on holding downloads back.
func (r *Reconnector) upnpDisconnect(ctx context.Context, svc Service) error {
	termErr := r.soap(ctx, svc, actionForceTermination)
	if termErr == nil {
		// The wait is not politeness; see upnpSettleDelay.
		if err := r.sleep(ctx, upnpSettleDelay); err != nil {
			return err
		}
		_ = r.soap(ctx, svc, actionRequestConnection)
		return nil
	}
	// ForceTermination is an optional action and some firmware simply does not
	// implement it, so RequestConnection is tried on its own: on a connection
	// that is already up it is a no-op, but on one the router dropped for its
	// own reasons it is the thing that dials.
	reqErr := r.soap(ctx, svc, actionRequestConnection)
	if reqErr == nil {
		return nil
	}
	return fmt.Errorf("%s failed (%v) and so did %s (%v)", actionForceTermination, termErr, actionRequestConnection, reqErr)
}

// deviceDescription is as much of the UPnP device description as this package
// reads. Everything else in it is ignored on purpose: the icons, the presentation
// URL and the model numbers are all attacker-supplied strings from a device on
// the LAN, and nothing here should be able to reach a log or a page.
type deviceDescription struct {
	XMLName xml.Name        `xml:"root"`
	URLBase string          `xml:"URLBase"`
	Device  describedDevice `xml:"device"`
}

type describedDevice struct {
	DeviceType string            `xml:"deviceType"`
	Services   []describedSvc    `xml:"serviceList>service"`
	Devices    []describedDevice `xml:"deviceList>device"`
}

type describedSvc struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

// wanServices reads a gateway's description and returns every WAN connection
// service in it, with control URLs resolved against the description's own
// location. It is this method's own thin wrapper around WANServices, kept so
// every call site in this file that already has a Reconnector in hand stays a
// method call; see WANServices for the implementation, and for why it exists
// as a package-level function taking a Doer instead of only as this method.
func (r *Reconnector) wanServices(ctx context.Context, g Gateway) ([]Service, error) {
	return WANServices(ctx, r.http, g)
}

// WANServices reads a gateway's device description over doer and returns
// every WAN connection service in it, with control URLs resolved against the
// description's own location.
//
// Exported and taking a Doer directly - rather than only living as the
// Reconnector method above - so a caller that needs the same WAN control
// endpoint for an action this package does not implement can reuse this
// discovery instead of writing a second SSDP-and-device-description parser.
// internal/portmap's AddPortMapping is that caller: port mapping needs
// exactly the endpoint ForceTermination and RequestConnection already use,
// and the SSRF pinning below is the reason it must come from here rather
// than from a fresh implementation that has not been through the same
// adversarial review this one has (see upnp_test.go's
// TestWANServicesKeepsTheControlURLOnTheHostThatAnswered).
func WANServices(ctx context.Context, doer Doer, g Gateway) ([]Service, error) {
	base, err := url.Parse(g.Location)
	if err != nil {
		return nil, fmt.Errorf("its description URL is unusable: %v", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		// A LOCATION with any other scheme is not something to go and fetch.
		return nil, fmt.Errorf("its description URL is not HTTP: %s", base.Scheme)
	}

	body, err := fetch(ctx, doer, base.String(), maxDescriptionBody)
	if err != nil {
		return nil, fmt.Errorf("its description could not be read: %v", err)
	}
	var desc deviceDescription
	if err := xml.Unmarshal(body, &desc); err != nil {
		return nil, fmt.Errorf("its description is not readable XML: %v", err)
	}
	// The host that answered the search is the only one this gateway may send us
	// to. Everything below this line comes out of a document written by a device
	// on the LAN, and two fields in it name a host: URLBase here, and an absolute
	// controlURL in collectWANServices. Without the pin, any box that answers a
	// multicast search can hand back a description that points the SOAP call at
	// an address it chose - so the reconnect becomes a request made on that
	// device's behalf, from inside the network, against a host it could not
	// reach itself. The LOCATION is already checked against the sender in
	// parseSSDPResponse; this is the same guard carried through to the second
	// and third places a host can appear, which is where it was missing.
	//
	// A different port or path is allowed on purpose, because that is the real
	// case: firmware does serve its description on one port and its control
	// endpoint on another. Only the host is fixed.
	host := base.Hostname()

	// URLBase, when present, wins over the location for resolving relative
	// control URLs. Firmware that serves its description from one port and its
	// control endpoint on another says so this way, and ignoring it produces a
	// control URL that answers 404 on every action.
	if desc.URLBase != "" {
		if u, err := url.Parse(strings.TrimSpace(desc.URLBase)); err == nil && u.Host != "" && strings.EqualFold(u.Hostname(), host) {
			base = u
		}
		// A URLBase naming another host is dropped rather than refused: the
		// location still resolves every relative control URL, so a device whose
		// firmware writes something odd here keeps working, and one that is
		// trying to redirect us simply does not get to.
	}

	var out []Service
	// Two passes so a WANIPConnection is always tried before a WANPPPConnection
	// on a device that lists both: the PPP service on such a device is usually
	// the vestigial one, and calling ForceTermination on a service with no
	// connection behind it wastes the settle delay before the real one is tried.
	for _, prefix := range []string{wanIPPrefix, wanPPPPrefix} {
		collectWANServices(desc.Device, prefix, base, host, &out)
	}
	return out, nil
}

// collectWANServices walks the nested device tree. The WAN services live three
// levels down (InternetGatewayDevice > WANDevice > WANConnectionDevice), and a
// flat scan of the top-level service list finds nothing at all.
// The host is passed alongside the base because a controlURL is allowed to be
// absolute, and an absolute one replaces the base's host entirely rather than
// resolving against it. See the pin in WANServices for why that matters.
func collectWANServices(d describedDevice, prefix string, base *url.URL, host string, out *[]Service) {
	for _, s := range d.Services {
		t := strings.TrimSpace(s.ServiceType)
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		ctl := strings.TrimSpace(s.ControlURL)
		if ctl == "" {
			continue
		}
		u, err := base.Parse(ctl)
		if err != nil || u.Host == "" {
			continue
		}
		if !strings.EqualFold(u.Hostname(), host) {
			continue
		}
		*out = append(*out, Service{ServiceType: t, ControlURL: u.String()})
	}
	for _, child := range d.Devices {
		collectWANServices(child, prefix, base, host, out)
	}
}

// soapEnvelope is the request body. The action names are this package's own
// constants and the service type is escaped, because it came off the network:
// a device that reports its service type as `"/><script>` would otherwise be
// writing the XML we send back to it.
const soapEnvelope = `<?xml version="1.0" encoding="utf-8"?>` +
	`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"` +
	` s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
	`<s:Body><u:%s xmlns:u="%s"></u:%s></s:Body></s:Envelope>`

// soap performs one action and turns a refusal into a sentence.
func (r *Reconnector) soap(ctx context.Context, svc Service, action string) error {
	escaped := &bytes.Buffer{}
	if err := xml.EscapeText(escaped, []byte(svc.ServiceType)); err != nil {
		return err
	}
	body := fmt.Sprintf(soapEnvelope, action, escaped.String(), action)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, svc.ControlURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	// The quotes around the SOAPAction value are required by the specification,
	// and a good deal of router firmware rejects the header without them with a
	// bare 500 that names nothing.
	req.Header.Set("SOAPAction", `"`+svc.ServiceType+"#"+action+`"`)

	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	if resp == nil {
		return errNoResponse
	}
	answer, readErr := io.ReadAll(io.LimitReader(bodyOf(resp), maxSOAPBody))
	closeBody(resp)
	if ok2xx(resp.StatusCode) {
		return nil
	}
	if readErr == nil {
		if code, desc := soapFault(answer); code != 0 {
			// The device's own error code and text, because "500" from a router
			// covers "you are not allowed to do that", "there is no connection"
			// and "I do not know that action", and the fix differs for each.
			return fmt.Errorf("%s: UPnP error %d %s", action, code, desc)
		}
	}
	return fmt.Errorf("%s: unexpected status %s", action, statusText(resp))
}

// soapFault pulls the UPnP error out of a fault body. Namespace prefixes are not
// matched on: encoding/xml compares the local name when the tag names no
// namespace, and firmware disagrees about whether the prefix is s, SOAP-ENV or
// nothing at all.
func soapFault(b []byte) (int, string) {
	var env struct {
		XMLName     xml.Name `xml:"Envelope"`
		Code        int      `xml:"Body>Fault>detail>UPnPError>errorCode"`
		Description string   `xml:"Body>Fault>detail>UPnPError>errorDescription"`
		FaultString string   `xml:"Body>Fault>faultstring"`
	}
	if err := xml.Unmarshal(b, &env); err != nil {
		return 0, ""
	}
	desc := strings.TrimSpace(env.Description)
	if desc == "" {
		desc = strings.TrimSpace(env.FaultString)
	}
	return env.Code, desc
}

// SOAPFault is soapFault, exported so a caller sending its own SOAP action
// against a Service this package discovered - internal/portmap's
// AddPortMapping and its GetSpecificPortMappingEntry read-back - can report
// the router's own error code and text instead of a bare HTTP status, the
// same quality of error this package gives its own two actions.
func SOAPFault(b []byte) (code int, desc string) {
	return soapFault(b)
}

// fetch reads a bounded body over doer. It is a package-level function
// rather than a method on Reconnector so WANServices can be called with any
// Doer, not only from a Reconnector that already has one - see that
// function's own doc comment for why that matters.
func fetch(ctx context.Context, doer Doer, target string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errNoResponse
	}
	b, err := io.ReadAll(io.LimitReader(bodyOf(resp), limit))
	closeBody(resp)
	if err != nil {
		return nil, err
	}
	if !ok2xx(resp.StatusCode) {
		return nil, fmt.Errorf("unexpected status %s", statusText(resp))
	}
	return b, nil
}

// SSDPSearch is ssdpSearch, exported so a caller that needs the same gateway
// search for an action this package does not implement - internal/portmap's
// AddPortMapping, specifically - can reuse it as its own default Discoverer
// instead of a second SSDP implementation. It is the identical function
// New uses as the package's own default; there is only one SSDP search in
// this codebase and this is it.
func SSDPSearch(ctx context.Context, timeout time.Duration) ([]Gateway, error) {
	return ssdpSearch(ctx, timeout)
}

// ssdpSearch is the default Discoverer: one M-SEARCH per search target on the
// SSDP multicast group, then every answer that arrives inside the window.
func ssdpSearch(ctx context.Context, timeout time.Duration) ([]Gateway, error) {
	if timeout <= 0 {
		timeout = ssdpTimeout
	}
	dst, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, err
	}
	// An unbound local port rather than 1900: binding the SSDP port would fail
	// outright on a host already running a UPnP daemon, and a search does not
	// need it - the answers come back to whatever port they were sent from.
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// A cancelled context has to end the read, and there is no way to hand a
	// context to ReadFrom. Setting a deadline in the past wakes it immediately,
	// and deadlines are safe to set from another goroutine.
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetReadDeadline(time.Now())
		case <-stopped:
		}
	}()

	var sent bool
	for _, st := range ssdpSearchTargets {
		msg := "M-SEARCH * HTTP/1.1\r\n" +
			"HOST: " + ssdpAddr + "\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			fmt.Sprintf("MX: %d\r\n", ssdpMX) +
			"ST: " + st + "\r\n\r\n"
		if _, err := conn.WriteTo([]byte(msg), dst); err == nil {
			sent = true
		}
	}
	if !sent {
		// Every send failing is a machine with no route for multicast at all,
		// which is a different thing from a network where nobody answered, and
		// waiting three seconds to say so helps nobody.
		return nil, errors.New("the SSDP search could not be sent")
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var found []Gateway
	buf := make([]byte, maxSSDPDatagram)
	for len(found) < maxGateways {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			// The deadline expiring is how a search ends normally.
			break
		}
		g, ok := parseSSDPResponse(buf[:n], from)
		if !ok || seen[g.Location] {
			continue
		}
		seen[g.Location] = true
		found = append(found, g)
	}
	if len(found) == 0 && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return found, nil
}

// parseSSDPResponse reads one datagram.
//
// The LOCATION is checked against the address the datagram came from whenever it
// is written as a literal. Anything on the LAN can answer a search, and a
// LOCATION pointing somewhere else turns this reconnect into a request made on
// that device's behalf to an address it chose - with the client's credentials
// and from inside the network. Devices name themselves by address in practice,
// so the check costs nothing real.
func parseSSDPResponse(b []byte, from net.Addr) (Gateway, bool) {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	status, err := tp.ReadLine()
	if err != nil || !strings.HasPrefix(strings.ToUpper(status), "HTTP/") {
		// NOTIFY advertisements land on this socket too and start with the
		// method, not a status line. They are not answers to our search.
		return Gateway{}, false
	}
	// The error is ignored: a datagram is not required to end with the blank
	// line that terminates a header block, and the headers that were read are
	// still exactly what the device sent.
	h, _ := tp.ReadMIMEHeader()
	loc := strings.TrimSpace(h.Get("Location"))
	if loc == "" {
		return Gateway{}, false
	}
	u, err := url.Parse(loc)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Gateway{}, false
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && !sameHost(addr, from) {
		return Gateway{}, false
	}
	return Gateway{Location: u.String(), Server: strings.TrimSpace(h.Get("Server"))}, true
}

// sameHost reports whether an address literal matches the sender of a datagram.
func sameHost(addr netip.Addr, from net.Addr) bool {
	ua, ok := from.(*net.UDPAddr)
	if !ok {
		// An address of some other kind cannot be compared, and refusing every
		// answer on a platform whose net package surprises us would break the
		// method outright. The comparison is a hardening step, not the contract.
		return true
	}
	src, ok := netip.AddrFromSlice(ua.IP)
	if !ok {
		return true
	}
	return src.Unmap() == addr.Unmap()
}
