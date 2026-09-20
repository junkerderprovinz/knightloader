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
// unauthenticated multicast search, so every host it names is a claim. These
// tests cover the three places a host can arrive: the SSDP LOCATION, the
// description's URLBase and an absolute controlURL.

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

// TestWANServicesKeepsTheControlURLOnTheHostThatAnswered: the description can
// name a host in URLBase and in an absolute controlURL, and neither may send
// the SOAP call to a host other than the one that answered the search.
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
		// Firmware often serves the description on one port and control on
		// another.
		name:       "URLBase may move the port",
		urlBase:    "http://192.168.1.1:49000/",
		controlURL: "ctl/IPConn",
		want:       "http://192.168.1.1:49000/ctl/IPConn",
	}, {
		name:       "URLBase may not move the host",
		urlBase:    "http://192.168.10.10/",
		controlURL: "/ctl/IPConn",
		// The URLBase is ignored and the location resolves the relative URL.
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
		// The base moves the host while the control URL looks relative, so a
		// guard on the controlURL string alone would miss it.
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

			svcs, err := WANServices(context.Background(), r.http, Gateway{Location: gateway})
			if err != nil {
				t.Fatalf("WANServices: %v", err)
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

// TestWANServicesReadsTheDescriptionFromTheLocation: if the fetch went
// elsewhere, the test above would pass for the wrong reason.
func TestWANServicesReadsTheDescriptionFromTheLocation(t *testing.T) {
	doer := &descDoer{body: description("", "/ctl")}
	r, err := New(Options{Config: func() Config { return Config{} }, HTTP: doer})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := WANServices(context.Background(), r.http, Gateway{Location: "http://192.168.1.1:5000/desc.xml"}); err != nil {
		t.Fatalf("WANServices: %v", err)
	}
	if len(doer.got) != 1 || doer.got[0] != "http://192.168.1.1:5000/desc.xml" {
		t.Errorf("fetched %v, want just the location", doer.got)
	}
}

func TestWANServicesRefusesANonHTTPLocation(t *testing.T) {
	for _, loc := range []string{"file:///etc/passwd", "ftp://192.168.1.1/desc.xml", "gopher://192.168.1.1/"} {
		doer := &descDoer{body: description("", "/ctl")}
		r, err := New(Options{Config: func() Config { return Config{} }, HTTP: doer})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := WANServices(context.Background(), r.http, Gateway{Location: loc}); err == nil {
			t.Errorf("%s was accepted as a description URL", loc)
		}
		if len(doer.got) != 0 {
			t.Errorf("%s was fetched anyway: %v", loc, doer.got)
		}
	}
}

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
		name:    "a LOCATION announcing another device's address",
		payload: "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.10.10/desc.xml\r\n\r\n",
		wantOK:  false,
	}, {
		// Real devices send datagrams without the closing blank line.
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
