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

// The two ways the UPnP method fails, which need different fixes.
// ErrNoGateway means nothing answered the search: UPnP is off on the router,
// or multicast never left the machine, as in a container on a bridge network.
// ErrUPnPRefused means a gateway answered and then refused, which is a router
// setting ("allow UPnP control").
var (
	ErrNoGateway   = errors.New("reconnect: no UPnP gateway answered")
	ErrUPnPRefused = errors.New("reconnect: the UPnP gateway refused to reconnect")
)

// Gateway is one device that answered an SSDP search.
type Gateway struct {
	// Location is the device description URL from the LOCATION header.
	Location string `json:"location"`
	// Server is the SERVER header verbatim, so an error can name the firmware
	// that refused.
	Server string `json:"server,omitempty"`
}

// Discoverer finds UPnP gateways on the local network. Tests replace it so no
// test sends multicast. It must return within timeout, or a failing reconnect
// stalls the download queue.
type Discoverer func(ctx context.Context, timeout time.Duration) ([]Gateway, error)

// The SSDP wire constants, from the UPnP Device Architecture.
const (
	ssdpAddr = "239.255.255.250:1900"

	// ssdpTimeout is how long the search listens. Devices answer within their
	// MX window, so waiting longer only delays a failing reconnect.
	ssdpTimeout = 3 * time.Second

	// ssdpMX is the delay window handed to the devices, in seconds, shorter
	// than ssdpTimeout so a device that waits the whole window is still heard.
	ssdpMX = 2

	// maxSSDPDatagram is the read buffer. An SSDP answer is a handful of
	// header lines.
	maxSSDPDatagram = 4 << 10

	// maxGateways stops the search early on a network where hundreds of
	// devices answer; gateways are always among the first.
	maxGateways = 8
)

// ssdpSearchTargets are tried from most to least specific. The service
// targets are there because some firmware ignores a search for the device
// type.
var ssdpSearchTargets = []string{
	"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// The service types that can drop a WAN connection, matched by prefix so later
// versions are picked up. PPP is for a PPPoE WAN and IP for everything else;
// many routers expose both with only one live.
const (
	wanIPPrefix  = "urn:schemas-upnp-org:service:WANIPConnection:"
	wanPPPPrefix = "urn:schemas-upnp-org:service:WANPPPConnection:"
)

// The two actions, in the order they are used.
const (
	actionForceTermination  = "ForceTermination"
	actionRequestConnection = "RequestConnection"
)

// upnpSettleDelay is the pause between dropping the connection and asking for
// a new one. Firmware given both back to back often answers the second with
// "connection in use" and never dials.
const upnpSettleDelay = 2 * time.Second

// maxDescriptionBody caps the device description read; a description is a few
// kilobytes of XML.
const maxDescriptionBody = 256 << 10

// maxSOAPBody caps a SOAP answer, which is read only to name the fault in it.
const maxSOAPBody = 64 << 10

// upnp asks the gateway to drop and re-establish the WAN connection.
func (r *Reconnector) upnp(ctx context.Context, cfg Config) error {
	gateways, err := r.gateways(ctx, cfg)
	if err != nil {
		return err
	}
	if len(gateways) == 0 {
		return fmt.Errorf("%w: the SSDP search found nothing in %s", ErrNoGateway, ssdpTimeout)
	}

	// Every reason is reported, so with two gateways (a modem and a router)
	// the one that nearly worked is not hidden behind the last one tried.
	var refusals []string
	for _, g := range gateways {
		services, err := WANServices(ctx, r.http, g)
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

// gateways is the list of devices to try: the one the user pinned, or whatever
// answers the search.
func (r *Reconnector) gateways(ctx context.Context, cfg Config) ([]Gateway, error) {
	if cfg.UPnPLocation != "" {
		// A pinned location skips discovery and is used alone.
		return []Gateway{{Location: cfg.UPnPLocation}}, nil
	}
	found, err := r.discover(ctx, ssdpTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoGateway, err)
	}
	return found, nil
}

// Service is one control endpoint, resolved to an absolute URL on the host
// that answered the SSDP search (see WANServices). internal/portmap sends its
// own SOAP actions to it instead of repeating the discovery.
type Service struct {
	ServiceType string
	ControlURL  string
}

// upnpDisconnect runs the two actions against one service. ForceTermination
// drops the line and RequestConnection brings it back. Once the termination
// succeeded, a refused RequestConnection is ignored: most firmware redials on
// its own and answers "already connecting".
func (r *Reconnector) upnpDisconnect(ctx context.Context, svc Service) error {
	termErr := r.soap(ctx, svc, actionForceTermination)
	if termErr == nil {
		if err := r.sleep(ctx, upnpSettleDelay); err != nil {
			return err
		}
		_ = r.soap(ctx, svc, actionRequestConnection)
		return nil
	}
	// ForceTermination is optional and some firmware lacks it. On its own,
	// RequestConnection is a no-op on a live connection and dials a dropped
	// one.
	reqErr := r.soap(ctx, svc, actionRequestConnection)
	if reqErr == nil {
		return nil
	}
	return fmt.Errorf("%s failed (%v) and so did %s (%v)", actionForceTermination, termErr, actionRequestConnection, reqErr)
}

// deviceDescription is as much of the UPnP device description as this package
// reads. Everything else is untrusted text from a device on the LAN and is
// ignored.
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

// WANServices reads a gateway's device description over doer and returns
// every WAN connection service in it, with control URLs resolved against the
// description's location. internal/portmap uses it for AddPortMapping, so the
// host pinning below is not repeated elsewhere.
func WANServices(ctx context.Context, doer Doer, g Gateway) ([]Service, error) {
	base, err := url.Parse(g.Location)
	if err != nil {
		return nil, fmt.Errorf("its description URL is unusable: %v", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
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
	// Only the host that answered the search may be named. The description is
	// written by a device on the LAN, and URLBase or an absolute controlURL
	// could otherwise aim the SOAP call at any host it chose, from inside the
	// network. parseSSDPResponse applies the same check to LOCATION. A
	// different port or path is allowed, since firmware often serves control
	// on another port.
	host := base.Hostname()

	// URLBase, when present, wins for resolving relative control URLs; that is
	// how firmware says control lives on another port.
	if desc.URLBase != "" {
		if u, err := url.Parse(strings.TrimSpace(desc.URLBase)); err == nil && u.Host != "" && strings.EqualFold(u.Hostname(), host) {
			base = u
		}
		// A URLBase naming another host is ignored rather than refused, so odd
		// firmware keeps working and a redirect attempt has no effect.
	}

	var out []Service
	// WANIPConnection first: on a device listing both, the PPP service is
	// usually vestigial and would waste the settle delay.
	for _, prefix := range []string{wanIPPrefix, wanPPPPrefix} {
		collectWANServices(desc.Device, prefix, base, host, &out)
	}
	return out, nil
}

// collectWANServices walks the nested device tree; the WAN services sit three
// levels down (InternetGatewayDevice > WANDevice > WANConnectionDevice). host
// is passed separately because an absolute controlURL replaces the base's host
// instead of resolving against it.
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

// soapEnvelope is the request body. The service type is escaped before it goes
// in, because it came off the network.
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
	// The specification requires the quotes, and much firmware answers a bare
	// 500 without them.
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
			// A router's 500 covers "not allowed", "no connection" and "unknown
			// action", so the device's own code and text are reported.
			return fmt.Errorf("%s: UPnP error %d %s", action, code, desc)
		}
	}
	return fmt.Errorf("%s: unexpected status %s", action, statusText(resp))
}

// soapFault pulls the UPnP error out of a fault body. Tags name no namespace,
// so encoding/xml matches the local name whatever prefix the firmware uses.
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

// SOAPFault returns the UPnP error code and text in a SOAP fault body, for
// callers such as internal/portmap that send their own actions to a Service.
func SOAPFault(b []byte) (code int, desc string) {
	return soapFault(b)
}

// fetch reads a bounded body over doer.
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

// SSDPSearch is the SSDP gateway search this package uses by default, for
// callers such as internal/portmap.
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
	// An ephemeral port, since port 1900 is taken on a host running a UPnP
	// daemon and answers come back to the sending port anyway.
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// ReadFrom takes no context, so cancellation sets a deadline in the past,
	// which is safe from another goroutine.
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
		// No route for multicast at all, which differs from nobody answering.
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

// parseSSDPResponse reads one datagram. A LOCATION written as an address
// literal must match the datagram's sender: anything on the LAN can answer,
// and a LOCATION pointing elsewhere would make us send requests to an address
// the device chose. Gateways name themselves by address in practice.
func parseSSDPResponse(b []byte, from net.Addr) (Gateway, bool) {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	status, err := tp.ReadLine()
	if err != nil || !strings.HasPrefix(strings.ToUpper(status), "HTTP/") {
		// NOTIFY advertisements arrive on this socket too and are not answers.
		return Gateway{}, false
	}
	// A datagram need not end with the blank line that terminates a header
	// block, so the error is ignored and the headers read are used.
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
		// Another address type cannot be compared; the check is hardening, so
		// it must not break the method on an unexpected platform.
		return true
	}
	src, ok := netip.AddrFromSlice(ua.IP)
	if !ok {
		return true
	}
	return src.Unmap() == addr.Unmap()
}
