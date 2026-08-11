package reconnect

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// The UPnP method takes its instructions from a device that answered an
// unauthenticated multicast search, which means every host it names is a claim
// made by something on the LAN rather than a fact. These tests are about the
// three places such a claim can arrive - the SSDP LOCATION, the description's
// URLBase, and an absolute controlURL - because a guard on one of the three
// reads as a guard on all of them right up until it is not.

// descDoer answers a description fetch without a socket, and remembers what it
// was asked for.
type descDoer struct {
	body string
	got  []string
}

func (d *descDoer) Do(req *http.Request) (*http.Response, error) {
	d.got = append(d.got, req.URL.String())
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(d.body)),
		Header:     http.Header{"Content-Type": []string{"text/xml"}},
		Request:    req,
	}, nil
}

// description writes a device description with the WAN service nested where a
// real gateway puts it: InternetGatewayDevice > WANDevice > WANConnectionDevice.
// A flat one would pass a test that the real parser fails.
func description(urlBase, controlURL string) string {
	var base string
	if urlBase != "" {
		base = "<URLBase>" + urlBase + "</URLBase>"
	}
	return `<?xml version="1.0"?>` +
		`<root xmlns="urn:schemas-upnp-org:device-1-0">` + base +
		`<device><deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>` +
		`<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>` +
		`<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>` +
		`<serviceList><service>` +
		`<serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>` +
		`<controlURL>` + controlURL + `</controlURL>` +
		`</service></serviceList>` +
		`</device></deviceList></device></deviceList></device></root>`
}

// TestWANServicesKeepsTheControlURLOnTheHostThatAnswered is the important one.
// The LOCATION is checked against the sender of the datagram, but the document
// it points at can name a host twice more, and both were once taken at face
// value. A device that answers the search from its own address could then hand
// back a description that sends the SOAP call somewhere else entirely - an
// address it picked, reached from inside the network, by us.
func TestWANServicesKeepsTheControlURLOnTheHostThatAnswered(t *testing.T) {
	const gateway = "http://192.168.1.1:5000/desc.xml"

	tests := []struct {
		name       string
		urlBase    string
		controlURL string
		want       string // "" means the service must be dropped
	}{{
		name:       "a relative control URL resolves against the location",
		controlURL: "/ctl/IPConn",
		want:       "http://192.168.1.1:5000/ctl/IPConn",
	}, {
		// The real reason URLBase exists, and it has to keep working: plenty of
		// firmware serves the description on one port and control on another.
		name:       "URLBase may move the port",
		urlBase:    "http://192.168.1.1:49000/",
		controlURL: "ctl/IPConn",
		want:       "http://192.168.1.1:49000/ctl/IPConn",
	}, {
		name:       "URLBase may not move the host",
		urlBase:    "http://192.168.10.10/",
		controlURL: "/ctl/IPConn",
		// Dropped, not refused: the location still resolves the relative URL,
		// so odd firmware keeps working and a redirect attempt simply does not
		// take effect.
		want: "http://192.168.1.1:5000/ctl/IPConn",
	}, {
		name:       "an absolute control URL may not move the host either",
		controlURL: "http://192.168.10.10:80/ctl/IPConn",
		want:       "",
	}, {
		name:       "an absolute control URL on the same host is fine",
		controlURL: "http://192.168.1.1:49000/ctl/IPConn",
		want:       "http://192.168.1.1:49000/ctl/IPConn",
	}, {
		// Both fields hostile at once, which is what an attacker would actually
		// write: the base moves the host and the control URL looks relative, so
		// a guard that only inspected the controlURL string would see nothing
		// wrong with it.
		name:       "a hostile URLBase with an innocent-looking control URL",
		urlBase:    "http://169.254.169.254/",
		controlURL: "latest/meta-data/",
		want:       "http://192.168.1.1:5000/latest/meta-data/",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doer := &descDoer{body: description(tc.urlBase, tc.controlURL)}
			r, err := New(Options{Config: func() Config { return Config{} }, HTTP: doer})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			svcs, err := r.wanServices(context.Background(), Gateway{Location: gateway})
			if err != nil {
				t.Fatalf("wanServices: %v", err)
			}

			if tc.want == "" {
				if len(svcs) != 0 {
					t.Fatalf("got %+v, want the service dropped", svcs)
				}
				return
			}
			if len(svcs) != 1 {
				t.Fatalf("got %d services, want 1: %+v", len(svcs), svcs)
			}
			if svcs[0].ControlURL != tc.want {
				t.Errorf("control URL = %q, want %q", svcs[0].ControlURL, tc.want)
			}
		})
	}
}

// TestWANServicesReadsTheDescriptionFromTheLocation guards the plumbing the
// test above depends on: if the fetch went somewhere else, every case would
// pass for the wrong reason.
func TestWANServicesReadsTheDescriptionFromTheLocation(t *testing.T) {
	doer := &descDoer{body: description("", "/ctl")}
	r, err := New(Options{Config: func() Config { return Config{} }, HTTP: doer})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.wanServices(context.Background(), Gateway{Location: "http://192.168.1.1:5000/desc.xml"}); err != nil {
		t.Fatalf("wanServices: %v", err)
	}
	if len(doer.got) != 1 || doer.got[0] != "http://192.168.1.1:5000/desc.xml" {
		t.Errorf("fetched %v, want just the location", doer.got)
	}
}

// TestWANServicesRefusesANonHTTPLocation: a LOCATION is a URL a device chose,
// and file:// or gopher:// is not something to go and open.
func TestWANServicesRefusesANonHTTPLocation(t *testing.T) {
	for _, loc := range []string{"file:///etc/passwd", "ftp://192.168.1.1/desc.xml", "gopher://192.168.1.1/"} {
		doer := &descDoer{body: description("", "/ctl")}
		r, err := New(Options{Config: func() Config { return Config{} }, HTTP: doer})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := r.wanServices(context.Background(), Gateway{Location: loc}); err == nil {
			t.Errorf("%s was accepted as a description URL", loc)
		}
		if len(doer.got) != 0 {
			t.Errorf("%s was fetched anyway: %v", loc, doer.got)
		}
	}
}

// TestParseSSDPResponse covers the first of the three places a host arrives.
func TestParseSSDPResponse(t *testing.T) {
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 1900}

	tests := []struct {
		name    string
		payload string
		wantOK  bool
		wantLoc string
	}{{
		name:    "an ordinary answer",
		payload: "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=120\r\nLOCATION: http://192.168.1.1:5000/desc.xml\r\nSERVER: Router/1.0 UPnP/1.0\r\n\r\n",
		wantOK:  true,
		wantLoc: "http://192.168.1.1:5000/desc.xml",
	}, {
		// Devices announce themselves on this socket unprompted. An advertisement
		// is not an answer to our search, and reading it as one would make the
		// result depend on what happened to be shouting at the moment.
		name:    "a NOTIFY advertisement is not an answer",
		payload: "NOTIFY * HTTP/1.1\r\nLOCATION: http://192.168.1.1:5000/desc.xml\r\nNTS: ssdp:alive\r\n\r\n",
		wantOK:  false,
	}, {
		name:    "no LOCATION at all",
		payload: "HTTP/1.1 200 OK\r\nSERVER: Router/1.0\r\n\r\n",
		wantOK:  false,
	}, {
		name:    "a LOCATION that is not HTTP",
		payload: "HTTP/1.1 200 OK\r\nLOCATION: file:///etc/passwd\r\n\r\n",
		wantOK:  false,
	}, {
		// The whole point of the check: anything on the LAN may answer, so an
		// answer that names somebody else's address is refused.
		name:    "a LOCATION announcing another device's address",
		payload: "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.10.10/desc.xml\r\n\r\n",
		wantOK:  false,
	}, {
		// A datagram that stops after the last header, with no blank line to
		// close the block. Real devices send these and the headers are still
		// exactly what was sent.
		name:    "an unterminated header block",
		payload: "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.1.1:5000/desc.xml",
		wantOK:  true,
		wantLoc: "http://192.168.1.1:5000/desc.xml",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, ok := parseSSDPResponse([]byte(tc.payload), from)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (gateway %+v)", ok, tc.wantOK, g)
			}
			if ok && g.Location != tc.wantLoc {
				t.Errorf("location = %q, want %q", g.Location, tc.wantLoc)
			}
		})
	}
}
