// Package portmap asks a UPnP gateway to forward a port to this machine, and
// tells the caller whether the router actually kept the mapping rather than
// just accepting the request.
//
// Home, decided rather than assumed: this is its own package, not
// internal/reconnect widened to cover it. reconnect's own package doc names
// its purpose in one sentence - "gets the box a new public IP address, which
// is the only thing that lifts a hoster's free-user limit when the limit is
// keyed to the address" - and says nothing about forwarding a port, because
// the two jobs do not overlap beyond talking to the same class of device.
// reconnect drops and re-establishes the WAN connection once and reports
// whether the public address moved; this package asks the gateway to
// remember a rule indefinitely and reports whether it actually did. Widening
// reconnect to cover this would stretch a package whose own doc comment
// already commits to a narrower job, for a caller - BitTorrent reachability -
// none of reconnect's existing callers have any reason to know exists.
//
// What is reused rather than rebuilt: gateway discovery.
// internal/reconnect/upnp.go already finds a device's WAN connection service
// over SSDP and its device description, with the SSRF pin - a description
// may only point a SOAP call at the host that answered the search - that
// makes doing so safe against an untrusted LAN. reconnect.SSDPSearch,
// reconnect.WANServices and the reconnect.Gateway/reconnect.Service/
// reconnect.Doer types they use are exported from that package for exactly
// this reuse; see their own doc comments for the discovery this package
// deliberately does not reimplement. What this package adds is the one
// thing reconnect's own soap() cannot express - that function's own doc
// comment anticipates it: "soap()'s signature likely needs widening, or a
// sibling function" - an action with real parameters (AddPortMapping's
// internal IP, internal port, external port, protocol, description), plus a
// second call, GetSpecificPortMappingEntry, to read the mapping back rather
// than trust the first call's bare success.
//
// Confirmed, unconfirmed, failed: AddPortMapping's own response carries no
// output arguments, so a 2xx from it proves only that the router accepted
// the request, never that the mapping exists. A router is free to accept
// the call and then do nothing with it - a full mapping table, a firmware
// bug, a modem one hop further out doing the actual NAT while this box's
// own gateway answers politely and changes nothing. The read-back in
// Attempt is what tells the three apart, and every caller sees all three as
// what they are rather than a single success/failure boolean - see Outcome.
//
// Two entry points: Attempt maps one (port, protocol) pair, the shape the
// real AddPortMapping action itself takes. AttemptPort is what an HTTP route
// for a single "map my torrent port" button actually wants - both TCP and
// UDP, since a listen port needs both - folded into the one Result such a
// button has one place to show.
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
	// positively disproves the router's own success - there is proof, not
	// merely an absence of proof, that the mapping did not take.
	Failed Outcome = "failed"
	// Confirmed means AddPortMapping succeeded and GetSpecificPortMappingEntry
	// reads back a mapping pointing at this machine, at the port asked for.
	Confirmed Outcome = "confirmed"
	// Unconfirmed means AddPortMapping succeeded but the read-back could not
	// prove it either way: the query action is not implemented, the call
	// failed for a transport reason, or its answer could not be parsed.
	// This is not Failed - a great many routers apply the mapping and
	// simply do not support confirming it - and must never be rendered as a
	// plain success either. This is the state a caller exists to be honest
	// about; see docs/torrent-support.md's own risk note on this exact
	// failure mode.
	Unconfirmed Outcome = "unconfirmed"
)

// The Reason codes: a typed word for why Outcome came out the way it did,
// alongside the free-text Detail - see reconnect.ConfigProblem's own doc
// comment for why this app hands a code across an API rather than only a
// sentence: the sentence is for a log or a curl user, the code is what a
// translated interface actually keys off.
const (
	ReasonConfirmed = "confirmed"
	// ReasonNoGateway means SSDP found nothing, and no gateway was pinned -
	// the same fact reconnect.ErrNoGateway names for the reconnect method.
	ReasonNoGateway = "noGateway"
	// ReasonRefused means at least one gateway answered discovery, but no
	// WAN connection service on any of them would complete the
	// AddPortMapping call - covering a gateway with no WAN service at all,
	// AddPortMapping itself faulting, and this machine's own address not
	// being resolvable toward a candidate service, all three of which
	// belong in Detail rather than as separate reasons: a person reading
	// "why did this fail" wants the list of what was tried, and a page
	// rendering the reason only needs to know that nothing worked.
	ReasonRefused = "refused"
	// ReasonNoSuchEntry means AddPortMapping succeeded and the read-back
	// proves the router has no such mapping now.
	ReasonNoSuchEntry = "noSuchEntry"
	// ReasonMismatch means the read-back shows a mapping at the requested
	// external port, but pointing at a different internal address or port
	// than what was requested.
	ReasonMismatch = "mismatch"
	// ReasonReadBackUnsupported means AddPortMapping succeeded but
	// GetSpecificPortMappingEntry itself could not be completed - the
	// action is not implemented, the call failed in transport, or its
	// answer could not be parsed.
	ReasonReadBackUnsupported = "readBackUnsupported"
)

// defaultDescription is what a blank Request.Description becomes: shown in
// the router's own port-forwarding list, so blank is worse than a name that
// at least says which app asked.
const defaultDescription = "KnightLoader"

// discoverTimeout bounds the SSDP search this package runs when
// Request.Location is not pinned. It matches the spirit of reconnect's own
// (unexported) ssdpTimeout - the UPnP specification has devices answer
// inside their advertised MX window, so waiting longer only adds dead time
// to a failing attempt - kept as this package's own constant because the
// two packages do not share unexported values.
const discoverTimeout = 3 * time.Second

// Request is one port mapping to attempt - a torrent listen port, almost
// always, though nothing here assumes that.
type Request struct {
	InternalPort int    // required, 1-65535
	ExternalPort int    // 0 means "same as InternalPort", the common case
	Protocol     string // "TCP" or "UDP", case-insensitive; empty means "TCP"
	Description  string // shown in the router's own port-forwarding list; empty means defaultDescription

	// Location pins the gateway's device description URL and skips
	// discovery, mirroring reconnect.Config.UPnPLocation - the same router,
	// the same reason: a network whose multicast is filtered but whose
	// gateway is otherwise perfectly reachable.
	Location string

	// HTTP is the client every SOAP call and device-description fetch goes
	// out on. Nil means a plain client with requestTimeout, matching
	// reconnect's own fallback client for the same reason: the caller that
	// has a policy to inject - the app, through internal/httpx - injects
	// one, and only a caller with no policy of its own falls back to this.
	HTTP reconnect.Doer

	// Discover finds UPnP gateways. Nil means reconnect.SSDPSearch. Tests
	// replace it so nothing in this package's own suite ever sends a
	// multicast datagram, the same discipline reconnect.Discoverer's own
	// doc comment holds itself to and for the identical reason: a sandboxed
	// test runner may not be allowed to send one at all.
	Discover reconnect.Discoverer

	// LocalIP resolves this machine's own address on the interface that
	// reaches a gateway, given that gateway's control URL. Nil means
	// localIPFor - see localip.go. Tests replace it for the same reason
	// Discover is replaced: the real answer depends on the test machine's
	// actual network configuration, which a unit test has no business
	// asserting against.
	LocalIP func(ctx context.Context, controlURL string) (netip.Addr, error)
}

// Result is the honest answer to one Request: never a bare success. Outcome
// and Reason are always set whenever Attempt returns a nil error; Detail and
// the rest are filled in as far as the attempt got - see Attempt's own doc
// comment for exactly when each is populated.
type Result struct {
	Outcome Outcome `json:"outcome"`
	Reason  string  `json:"reason"`
	// Detail is the one thing Reason cannot carry: the router's own words -
	// a UPnP fault's errorDescription, or a Go transport error - or another
	// concrete fact, such as which address a conflicting mapping points at.
	// It is English and untranslated on purpose: the words came from the
	// router or from Go's own errors, not from this app, matching how
	// reconnect hands its own errors back verbatim rather than templating
	// them.
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
// The error return means req itself could not be attempted at all - an
// internal or external port outside 1-65535, or a protocol that is neither
// TCP nor UDP - and Result is not meaningful when it is set. Every other
// outcome, including no gateway answering, every gateway refusing, and the
// three shades of a mapping that was attempted (Confirmed, Unconfirmed,
// Failed), is reported through a nil error and a fully populated Result:
// this is a network's answer, not a failure of this function, and belongs
// in Result.Outcome/Result.Reason for a caller - the HTTP route this
// package is built for, specifically - to render honestly rather than as a
// bare success or a generic server error. A caller must check the error
// first, and once it is nil, treat Result.Outcome as the real verdict
// rather than assuming success because the call returned at all.
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

	// Every reason is collected and reported together, the same as
	// reconnect's own upnp(): a box with two gateways on it, or one real
	// device answering twice under two service types, would otherwise
	// report whichever happened to be tried last and hide the one that was
	// nearly right.
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

// AttemptPort asks a UPnP gateway to forward one port number for both TCP
// and UDP - what a BitTorrent listen port actually needs, since peers arrive
// over TCP and DHT/uTP traffic arrives over UDP on the very same port
// number - and folds the two independent attempts into the one Result a
// caller with a single button and a single place to show an outcome needs.
// req.Protocol is ignored; both protocols are always attempted.
//
// The combined Outcome is deliberately the worse of the two - Confirmed
// only when both protocols confirm - because a listen port only one of TCP
// or UDP can reach is not a port every peer can use, and reporting
// Confirmed off the better half alone would be exactly the unverified
// success this whole package exists to refuse to give. See combine's own
// doc comment for how Reason and Detail carry both halves forward.
//
// The error return has the same meaning as Attempt's own: a caller mistake
// in req (a port out of range), never a network outcome.
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
	default: // Failed, or an empty Outcome from a bug - treated as the worst, not a crash
		return 0
	}
}

// combine folds a TCP Result and a UDP Result for the same port into one.
// Every field but Detail is copied from whichever of the two ranks worse
// (see outcomeRank) - Gateway, InternalIP and the ports are expected to
// agree between the two anyway, since both were attempted against the same
// machine and the same requested port. Detail always names both halves
// explicitly, because "unconfirmed" alone does not say whether it was the
// TCP half, the UDP half, or both that could not be confirmed, and that is
// exactly the kind of fact a person trying to fix their router setup needs.
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

// mapping is the one port mapping being attempted, factored out of the
// gateway/service loop above because it does not change across candidates -
// only which WAN service and which local address it is attempted against
// does.
type mapping struct {
	internalPort int
	externalPort int
	protocol     string
	description  string
}

// upnpNoSuchEntry is GetSpecificPortMappingEntry's own UPnP IGD
// (WANIPConnection:1/WANPPPConnection:1) error code for "no mapping exists
// at this (RemoteHost, ExternalPort, Protocol)". It is the one fault code
// this package acts on by number rather than only quoting, because seeing
// it after AddPortMapping itself reported success is positive proof the
// mapping did not take - not merely an inability to check - which is the
// distinction Outcome exists to make (Failed, not Unconfirmed).
const upnpNoSuchEntry = 714

// attemptOn calls AddPortMapping against one already-discovered WAN service
// and then GetSpecificPortMappingEntry to read the result back.
//
// A non-nil error here means AddPortMapping itself was refused - this
// service could not do this at all - and Attempt's caller moves on to the
// next candidate. Everything AddPortMapping accepted, however the read-back
// then turns out, comes back as a Result with a nil error: see Attempt's own
// doc comment for why "accepted" and "confirmed" are different facts that
// belong on different sides of the return.
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

// gatewaysFor mirrors reconnect's own unexported gateways(): a pinned
// location skips discovery outright, otherwise the discoverer is asked -
// see Request.Location's own doc comment for why pinning exists at all.
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
