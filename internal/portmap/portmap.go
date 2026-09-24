// Package portmap asks a UPnP gateway to forward a port to this machine, and
// tells the caller whether the router actually kept the mapping rather than
// just accepting the request.
//
// Gateway discovery is internal/reconnect's: reconnect.SSDPSearch and
// reconnect.WANServices find a device's WAN connection service over SSDP and
// its description, with the pin that a description may only point a SOAP call
// at the host that answered the search. What this package adds is an action
// with real parameters (AddPortMapping's internal IP, internal port, external
// port, protocol, description) and a second call,
// GetSpecificPortMappingEntry, to read the mapping back.
//
// That read-back is what tells confirmed, unconfirmed and failed apart.
// AddPortMapping's response carries no output arguments, so a 2xx proves only
// that the router accepted the request: a full mapping table, a firmware bug
// or a modem one hop further out doing the actual NAT all look like success.
// Every caller sees the three states rather than one boolean, see Outcome.
//
// Attempt maps one (port, protocol) pair, the shape AddPortMapping itself
// takes. AttemptPort does both TCP and UDP, which is what a listen port needs,
// folded into the one Result a "map my torrent port" button can show.
package portmap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// Outcome is how one mapping attempt ended, once a WAN service has accepted
// the AddPortMapping call at all - see Attempt's own doc comment for why
// getting this far always reports through Result rather than through the
// error return, regardless of which of the three it turns out to be.
type Outcome string

const (
	// Failed means either AddPortMapping itself was refused (a SOAP fault,
	// or no gateway/WAN service could be reached at all), or the read-back
	// positively disproves the router's own success: there is proof, not
	// merely an absence of proof, that the mapping did not take.
	Failed Outcome = "failed"
	// Confirmed means AddPortMapping succeeded and GetSpecificPortMappingEntry
	// reads back a mapping pointing at this machine, at the port asked for.
	Confirmed Outcome = "confirmed"
	// Unconfirmed means AddPortMapping succeeded but the read-back could not
	// prove it either way: the query action is not implemented, the call
	// failed for a transport reason, or its answer could not be parsed.
	// This is not Failed, because a great many routers apply the mapping and
	// simply do not support confirming it, and it must never be rendered as
	// a plain success either: a router can accept the call and silently drop
	// the mapping, so the honest answer is "could not confirm".
	Unconfirmed Outcome = "unconfirmed"
)

// The Reason codes: a typed word for why Outcome came out the way it did,
// beside the free-text Detail. The sentence is for a log or a curl user, the
// code is what a translated interface keys off, as with
// reconnect.ConfigProblem.
const (
	ReasonConfirmed = "confirmed"
	// ReasonNoGateway means SSDP found nothing and no gateway was pinned,
	// the fact reconnect.ErrNoGateway names for the reconnect method.
	ReasonNoGateway = "noGateway"
	// ReasonRefused means at least one gateway answered discovery but no WAN
	// connection service on any of them completed the AddPortMapping call,
	// whether because a gateway has no WAN service, AddPortMapping faulted,
	// or this machine's address could not be resolved toward a candidate
	// service. Which of the three it was belongs in Detail: a page needs to
	// know that nothing worked, a person wants the list of what was tried.
	ReasonRefused = "refused"
	// ReasonNoSuchEntry means AddPortMapping succeeded and the read-back
	// proves the router has no such mapping now.
	ReasonNoSuchEntry = "noSuchEntry"
	// ReasonMismatch means the read-back shows a mapping at the requested
	// external port, but pointing at a different internal address or port
	// than what was requested.
	ReasonMismatch = "mismatch"
	// ReasonReadBackUnsupported means AddPortMapping succeeded but
	// GetSpecificPortMappingEntry could not be completed: the action is not
	// implemented, the call failed in transport, or its answer could not be
	// parsed.
	ReasonReadBackUnsupported = "readBackUnsupported"
)

// defaultDescription is what a blank Request.Description becomes: shown in
// the router's own port-forwarding list, so blank is worse than a name that
// at least says which app asked.
const defaultDescription = "KnightLoader"

// discoverTimeout bounds the SSDP search this package runs when
// Request.Location is not pinned. It matches reconnect's own unexported
// ssdpTimeout, which the two packages cannot share: the UPnP specification
// has devices answer inside their advertised MX window, so waiting longer
// only adds dead time to a failing attempt.
const discoverTimeout = 3 * time.Second

// Request is one port mapping to attempt, almost always a torrent listen
// port, though nothing here assumes that.
type Request struct {
	InternalPort int    // required, 1-65535
	ExternalPort int    // 0 means "same as InternalPort", the common case
	Protocol     string // "TCP" or "UDP", case-insensitive; empty means "TCP"
	Description  string // shown in the router's own port-forwarding list; empty means defaultDescription

	// Location pins the gateway's device description URL and skips
	// discovery, as reconnect.Config.UPnPLocation does for the same router:
	// a network whose multicast is filtered but whose gateway is reachable.
	Location string

	// HTTP is the client every SOAP call and device-description fetch goes
	// out on. Nil means a plain client with requestTimeout: the app injects
	// its own policy through internal/httpx, and only a caller without one
	// falls back to this.
	HTTP reconnect.Doer

	// Discover finds UPnP gateways. Nil means reconnect.SSDPSearch. Tests
	// replace it so nothing in this package's suite sends a multicast
	// datagram, which a sandboxed test runner may not be allowed to do.
	Discover reconnect.Discoverer

	// LocalIP resolves this machine's own address on the interface that
	// reaches a gateway, given that gateway's control URL. Nil means
	// localIPFor, see localip.go. Tests replace it because the real answer
	// depends on the test machine's network configuration.
	LocalIP func(ctx context.Context, controlURL string) (netip.Addr, error)
}

// Result is the answer to one Request, never a bare success. Outcome and
// Reason are set whenever Attempt returns a nil error, and the rest is filled
// in as far as the attempt got, see Attempt.
type Result struct {
	Outcome Outcome `json:"outcome"`
	Reason  string  `json:"reason"`
	// Detail is what Reason cannot carry: the router's own words, a UPnP
	// fault's errorDescription or a Go transport error, or a concrete fact
	// such as which address a conflicting mapping points at. English and
	// untranslated, because the words came from the router or from Go rather
	// than from this app.
	Detail string `json:"detail,omitempty"`

	Gateway      string     `json:"gateway,omitempty"` // the Location that answered
	InternalIP   netip.Addr `json:"internalIp,omitzero"`
	InternalPort int        `json:"internalPort,omitempty"`
	ExternalPort int        `json:"externalPort,omitempty"`
	Protocol     string     `json:"protocol,omitempty"`
}

// Attempt asks a UPnP gateway to forward req.ExternalPort/req.Protocol to
// this machine's req.InternalPort, and reports what happened.
//
// The error return means req itself could not be attempted at all, because a
// port is outside 1-65535 or the protocol is neither TCP nor UDP, and Result
// is not meaningful then. Every other outcome, including no gateway answering,
// every gateway refusing and the three shades of a mapping that was attempted,
// comes back as a nil error and a populated Result: that is the network's
// answer rather than a failure of this function, and the HTTP route renders it
// from Result.Outcome and Result.Reason. A caller checks the error first and
// then reads Result.Outcome as the verdict.
func Attempt(ctx context.Context, req Request) (Result, error) {
	doer := req.HTTP
	if doer == nil {
		doer = &http.Client{Timeout: requestTimeout}
	}
	discover := req.Discover
	if discover == nil {
		discover = reconnect.SSDPSearch
	}
	localIP := req.LocalIP
	if localIP == nil {
		localIP = localIPFor
	}

	if req.InternalPort < 1 || req.InternalPort > 65535 {
		return Result{}, fmt.Errorf("portmap: internal port %d is not 1-65535", req.InternalPort)
	}
	externalPort := req.ExternalPort
	if externalPort == 0 {
		externalPort = req.InternalPort
	}
	if externalPort < 1 || externalPort > 65535 {
		return Result{}, fmt.Errorf("portmap: external port %d is not 1-65535", externalPort)
	}
	protocol := strings.ToUpper(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = "TCP"
	}
	if protocol != "TCP" && protocol != "UDP" {
		return Result{}, fmt.Errorf("portmap: protocol %q is not TCP or UDP", req.Protocol)
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = defaultDescription
	}
	plan := mapping{internalPort: req.InternalPort, externalPort: externalPort, protocol: protocol, description: description}

	// Carried on every returned Result from here on, including the failure
	// paths below: a caller showing "could not map port 6881" needs the
	// number even when discovery never got far enough to attempt anything,
	// and Attempt already knows it before a single packet goes out.
	asked := Result{InternalPort: plan.internalPort, ExternalPort: plan.externalPort, Protocol: plan.protocol}

	gateways, err := gatewaysFor(ctx, discover, strings.TrimSpace(req.Location))
	if err != nil {
		asked.Outcome, asked.Reason, asked.Detail = Failed, ReasonNoGateway, err.Error()
		return asked, nil
	}
	if len(gateways) == 0 {
		asked.Outcome, asked.Reason = Failed, ReasonNoGateway
		asked.Detail = fmt.Sprintf("the SSDP search found nothing in %s", discoverTimeout)
		return asked, nil
	}

	// Every reason is collected and reported together, as reconnect's upnp
	// does: a box with two gateways, or one device answering twice under two
	// service types, would otherwise report whichever was tried last and hide
	// the one that was nearly right.
	var refusals []string
	for _, g := range gateways {
		services, err := reconnect.WANServices(ctx, doer, g)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("%s: %v", g.Location, err))
			continue
		}
		if len(services) == 0 {
			refusals = append(refusals, fmt.Sprintf("%s: answered the search but exposes no WAN connection service", g.Location))
			continue
		}
		for _, svc := range services {
			ip, err := localIP(ctx, svc.ControlURL)
			if err != nil {
				refusals = append(refusals, fmt.Sprintf("%s: %v", svc.ServiceType, err))
				continue
			}
			res, err := attemptOn(ctx, doer, svc, ip, plan)
			if err != nil {
				refusals = append(refusals, fmt.Sprintf("%s: %v", svc.ServiceType, err))
				continue
			}
			res.Gateway = g.Location
			return res, nil
		}
	}
	asked.Outcome, asked.Reason = Failed, ReasonRefused
	asked.Detail = strings.Join(refusals, "; ")
	return asked, nil
}

// AttemptPort asks a UPnP gateway to forward one port number for both TCP and
// UDP, which is what a BitTorrent listen port needs, since peers arrive over
// TCP and DHT and uTP traffic over UDP on the same number. The two attempts
// fold into one Result. req.Protocol is ignored.
//
// The combined Outcome is the worse of the two, so Confirmed means both
// protocols confirmed: a port only one of TCP or UDP can reach is not a port
// every peer can use. See combine for how Reason and Detail carry both halves
// forward.
//
// The error return means the same as Attempt's: a mistake in req, never a
// network outcome.
func AttemptPort(ctx context.Context, req Request) (Result, error) {
	tcpReq, udpReq := req, req
	tcpReq.Protocol, udpReq.Protocol = "TCP", "UDP"
	tcp, err := Attempt(ctx, tcpReq)
	if err != nil {
		return Result{}, err
	}
	udp, err := Attempt(ctx, udpReq)
	if err != nil {
		return Result{}, err
	}
	return combine(tcp, udp), nil
}

// outcomeRank orders Outcome from worst to best, so combine can pick the
// worse of two without a switch at every call site.
func outcomeRank(o Outcome) int {
	switch o {
	case Confirmed:
		return 2
	case Unconfirmed:
		return 1
	default: // Failed, or an empty Outcome, which ranks worst
		return 0
	}
}

// combine folds a TCP Result and a UDP Result for the same port into one.
// Every field but Detail comes from whichever of the two ranks worse (see
// outcomeRank); Gateway, InternalIP and the ports agree anyway, since both
// were attempted against the same machine and port. Detail names both halves,
// because "unconfirmed" alone does not say which of them could not be
// confirmed.
func combine(tcp, udp Result) Result {
	out := tcp
	if outcomeRank(udp.Outcome) < outcomeRank(tcp.Outcome) {
		out = udp
	}
	out.Detail = fmt.Sprintf("TCP: %s. UDP: %s.", oneLine(tcp), oneLine(udp))
	return out
}

// oneLine renders one protocol's half of combine's Detail.
func oneLine(r Result) string {
	if r.Detail == "" {
		return string(r.Outcome)
	}
	return string(r.Outcome) + " - " + r.Detail
}

// mapping is the one port mapping being attempted. It does not change across
// candidates; only the WAN service and the local address it is attempted
// against do.
type mapping struct {
	internalPort int
	externalPort int
	protocol     string
	description  string
}

// upnpNoSuchEntry is GetSpecificPortMappingEntry's UPnP IGD error code for
// "no mapping exists at this (RemoteHost, ExternalPort, Protocol)". It is the
// one fault code this package acts on by number rather than quoting, because
// seeing it after AddPortMapping reported success proves the mapping did not
// take rather than that it could not be checked: Failed, not Unconfirmed.
const upnpNoSuchEntry = 714

// attemptOn calls AddPortMapping against one already-discovered WAN service
// and then GetSpecificPortMappingEntry to read the result back.
//
// A non-nil error means AddPortMapping itself was refused, so this service
// cannot do it at all and Attempt moves on to the next candidate. Everything
// AddPortMapping accepted comes back as a Result with a nil error, however the
// read-back turned out: accepted and confirmed are different facts.
func attemptOn(ctx context.Context, doer reconnect.Doer, svc reconnect.Service, internalIP netip.Addr, m mapping) (Result, error) {
	_, err := soapCall(ctx, doer, svc, "AddPortMapping", []soapArg{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(m.externalPort)},
		{"NewProtocol", m.protocol},
		{"NewInternalPort", strconv.Itoa(m.internalPort)},
		{"NewInternalClient", internalIP.String()},
		{"NewEnabled", "1"},
		{"NewPortMappingDescription", m.description},
		{"NewLeaseDuration", "0"},
	})
	if err != nil {
		return Result{}, err
	}

	res := Result{
		InternalIP:   internalIP,
		InternalPort: m.internalPort,
		ExternalPort: m.externalPort,
		Protocol:     m.protocol,
	}

	body, err := soapCall(ctx, doer, svc, "GetSpecificPortMappingEntry", []soapArg{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(m.externalPort)},
		{"NewProtocol", m.protocol},
	})
	if err != nil {
		var fe *faultError
		if errors.As(err, &fe) && fe.code == upnpNoSuchEntry {
			res.Outcome, res.Reason = Failed, ReasonNoSuchEntry
			res.Detail = fmt.Sprintf("the router accepted the mapping request but now reports no such mapping: %s", fe.desc)
			return res, nil
		}
		res.Outcome, res.Reason = Unconfirmed, ReasonReadBackUnsupported
		res.Detail = err.Error()
		return res, nil
	}

	client, port, ok := parseSpecificEntry(body)
	if !ok {
		res.Outcome, res.Reason = Unconfirmed, ReasonReadBackUnsupported
		res.Detail = "the router's confirmation reply could not be read"
		return res, nil
	}
	if client != internalIP.String() || (port != "" && port != strconv.Itoa(m.internalPort)) {
		res.Outcome, res.Reason = Failed, ReasonMismatch
		res.Detail = fmt.Sprintf("the router reports this external port mapped to %s:%s, not this machine", client, port)
		return res, nil
	}

	res.Outcome, res.Reason = Confirmed, ReasonConfirmed
	res.Detail = "the router confirms the mapping points at this machine"
	return res, nil
}

// gatewaysFor mirrors reconnect's own unexported gateways: a pinned location
// skips discovery, otherwise the discoverer is asked. See Request.Location.
func gatewaysFor(ctx context.Context, discover reconnect.Discoverer, pinned string) ([]reconnect.Gateway, error) {
	if pinned != "" {
		return []reconnect.Gateway{{Location: pinned}}, nil
	}
	found, err := discover(ctx, discoverTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", reconnect.ErrNoGateway, err)
	}
	return found, nil
}
