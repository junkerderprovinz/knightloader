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

// requestTimeout bounds one SOAP call. Shorter than reconnect's thirty
// seconds, because this runs from a settings-page button somebody is watching
// rather than from a background retry.
const requestTimeout = 15 * time.Second

// maxSOAPBody caps a SOAP answer, as reconnect's own ceiling does: the answer
// is a handful of parameters, and a device that sends more is not worth
// reading further from.
const maxSOAPBody = 64 << 10

// errNoResponse mirrors reconnect's own: a Doer is an interface, and
// something other than *http.Client behind it could return neither a response
// nor an error. Turning that into a plain error keeps such a bug from
// panicking this package rather than failing the one attempt.
var errNoResponse = errors.New("portmap: the HTTP client returned no response")

// soapArg is one child element of a SOAP action's argument list, kept in the
// order the action's definition specifies. IGD implementations are not
// required to accept any other order, and enough router firmware does not,
// so a map's range order would not do.
type soapArg struct {
	name  string
	value string
}

// faultError is a UPnP SOAP fault, carrying the router's own error code and
// description. A distinct type rather than a formatted error string, because
// attemptOn acts on one specific code (upnpNoSuchEntry) rather than only
// displaying it.
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

// soapEnvelope builds one action's request body. reconnect's own soap has a
// literal template with no room for arguments, and AddPortMapping cannot be
// sent without them. It lives here rather than in reconnect, whose two
// actions take no arguments.
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

// soapCall performs one action with arguments against svc and returns the raw
// response body on success. A non-2xx answer becomes a *faultError when the
// body parses as a UPnP fault, through reconnect.SOAPFault, which already
// copes with the namespace prefixes firmware disagrees about, and a plain
// error otherwise.
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
	// specification, and router firmware that does not get them answers a
	// bare 500 naming nothing.
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

// parseSpecificEntry reads the two facts a GetSpecificPortMappingEntry answer
// carries: which internal address and port the router has on file for the
// external port that was asked about. Namespace prefixes are not matched on,
// as in reconnect's soapFault: encoding/xml compares the local name when a
// struct tag names no namespace, and firmware disagrees about whether the
// prefix is u, SOAP-ENV or nothing at all.
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
