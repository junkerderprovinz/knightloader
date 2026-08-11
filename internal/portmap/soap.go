package portmap

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// requestTimeout bounds one SOAP call. Shorter than reconnect's own 30
// seconds on purpose: this runs from a settings-page button the user is
// sitting in front of watching, not from a background retry nobody is
// looking at, so a router that has stopped answering should say so quickly.
const requestTimeout = 15 * time.Second

// maxSOAPBody caps a SOAP answer, mirroring reconnect's own ceiling and for
// the identical reason: the answer is a handful of parameters, and a device
// that sends more than that is not a device worth reading further from.
const maxSOAPBody = 64 << 10

// errNoResponse mirrors reconnect's own errNoResponse: a Doer is an
// interface, and something other than *http.Client behind it could return
// neither a response nor an error. Turning that into a plain error keeps a
// third-party Doer's bug from panicking this package instead of merely
// failing the one attempt it broke.
var errNoResponse = errors.New("portmap: the HTTP client returned no response")

// soapArg is one child element of a SOAP action's own argument list, kept in
// the order the action's own definition specifies. IGD implementations are
// not required to accept arguments in any order, and enough router firmware
// in the wild does not, that this package always sends them the way the
// specification lists them rather than the way a map would range over them.
type soapArg struct {
	name  string
	value string
}

// faultError is a UPnP SOAP fault, carrying the router's own error code and
// description. It is a distinct type - rather than only a formatted error
// string, which is all reconnect's own soap() produces for its two actions -
// because attemptOn has to act on one specific code (upnpNoSuchEntry) rather
// than merely display it; see attemptOn's own doc comment.
type faultError struct {
	action string
	code   int
	desc   string
}

func (e *faultError) Error() string {
	if e.code != 0 {
		return fmt.Sprintf("%s: UPnP error %d %s", e.action, e.code, e.desc)
	}
	return fmt.Sprintf("%s: %s", e.action, e.desc)
}

// soapEnvelope builds one action's request body. It is the sibling function
// reconnect's own soap() doc comment anticipated needing: that function's
// literal template - "<u:%s xmlns:u=\"%s\"></u:%s>" - has no way to hold
// arguments, and AddPortMapping cannot be sent without them. It lives here,
// next to the WAN actions that need it, rather than as a change to
// reconnect's own template: reconnect's two actions never take arguments and
// never will, so widening its envelope for them would be complexity with no
// caller.
func soapEnvelope(serviceType, action string, args []soapArg) (string, error) {
	escapedType := &bytes.Buffer{}
	if err := xml.EscapeText(escapedType, []byte(serviceType)); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	b.WriteString(`<s:Body><u:`)
	b.WriteString(action)
	b.WriteString(` xmlns:u="`)
	b.WriteString(escapedType.String())
	b.WriteString(`">`)
	for _, a := range args {
		// Argument names are this package's own constants, never network
		// input, so they are written verbatim; the value came off a request
		// (a description string, a formatted port number) or off the
		// network (an internal client address is still local, but nothing
		// stops a value from carrying markup) and is always escaped.
		escapedVal := &bytes.Buffer{}
		if err := xml.EscapeText(escapedVal, []byte(a.value)); err != nil {
			return "", err
		}
		b.WriteString("<")
		b.WriteString(a.name)
		b.WriteString(">")
		b.WriteString(escapedVal.String())
		b.WriteString("</")
		b.WriteString(a.name)
		b.WriteString(">")
	}
	b.WriteString(`</u:`)
	b.WriteString(action)
	b.WriteString(`></s:Body></s:Envelope>`)
	return b.String(), nil
}

// soapCall performs one action with arguments against svc and returns the
// raw response body on success. A non-2xx answer becomes a *faultError when
// the body parses as a UPnP fault - reconnect.SOAPFault is reconnect's own
// fault parser, reused here for the same reason WANServices is: firmware
// disagrees about SOAP fault namespace prefixes and that parser already
// copes with it - and a plain error otherwise.
func soapCall(ctx context.Context, doer reconnect.Doer, svc reconnect.Service, action string, args []soapArg) ([]byte, error) {
	body, err := soapEnvelope(svc.ServiceType, action, args)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, svc.ControlURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	// The quotes around the SOAPAction value are required by the
	// specification, and a good deal of router firmware rejects the header
	// without them with a bare 500 that names nothing - reconnect's own
	// soap() carries the identical note for the identical reason.
	req.Header.Set("SOAPAction", `"`+svc.ServiceType+"#"+action+`"`)

	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errNoResponse
	}
	var answer []byte
	if resp.Body != nil {
		answer, err = io.ReadAll(io.LimitReader(resp.Body, maxSOAPBody))
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return answer, nil
	}
	if code, desc := reconnect.SOAPFault(answer); code != 0 {
		return nil, &faultError{action: action, code: code, desc: desc}
	}
	return nil, fmt.Errorf("%s: unexpected status %d", action, resp.StatusCode)
}

// parseSpecificEntry reads a GetSpecificPortMappingEntry answer's own two
// load-bearing facts: which internal address and port the router has on
// file for the external port that was asked about. Namespace prefixes are
// not matched on, the same reason reconnect's own soapFault does not:
// encoding/xml compares the local name when a struct tag names no
// namespace, and firmware disagrees about whether the prefix is u, SOAP-ENV
// or nothing at all.
func parseSpecificEntry(b []byte) (internalClient, internalPort string, ok bool) {
	var env struct {
		XMLName xml.Name `xml:"Envelope"`
		Client  string   `xml:"Body>GetSpecificPortMappingEntryResponse>NewInternalClient"`
		Port    string   `xml:"Body>GetSpecificPortMappingEntryResponse>NewInternalPort"`
	}
	if err := xml.Unmarshal(b, &env); err != nil {
		return "", "", false
	}
	client := strings.TrimSpace(env.Client)
	if client == "" {
		return "", "", false
	}
	return client, strings.TrimSpace(env.Port), true
}
