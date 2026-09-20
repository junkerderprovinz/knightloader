package portmap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// soapDoer answers a SOAP POST without a socket, and remembers what it was
// asked for so a test can inspect the request rather than only the answer.
type soapDoer struct {
	status int
	body   string
	// bodyReader, when set, is used instead of body, for the test that needs
	// a reader rather than a fixed string.
	bodyReader io.Reader

	gotAction      string // the SOAPAction header, verbatim
	gotRequestBody string
}

func (d *soapDoer) Do(req *http.Request) (*http.Response, error) {
	d.gotAction = req.Header.Get("SOAPAction")
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		d.gotRequestBody = string(b)
	}
	var rc io.ReadCloser
	if d.bodyReader != nil {
		rc = io.NopCloser(d.bodyReader)
	} else {
		rc = io.NopCloser(strings.NewReader(d.body))
	}
	return &http.Response{StatusCode: d.status, Body: rc, Request: req}, nil
}

const testServiceType = "urn:schemas-upnp-org:service:WANIPConnection:1"

func testService() reconnect.Service {
	return reconnect.Service{ServiceType: testServiceType, ControlURL: "http://192.168.1.1:5000/ctl/IPConn"}
}

// Why soapEnvelope takes a slice of pairs rather than a map: firmware that
// needs the arguments in the order the action's definition lists them would
// otherwise get Go's randomised map order. The description also has to survive
// XML metacharacters, since it is free text from a settings page.
func TestSoapEnvelopeOrdersArgumentsAndEscapesValues(t *testing.T) {
	args := []soapArg{
		{"NewExternalPort", "6881"},
		{"NewProtocol", "TCP"},
		{"NewPortMappingDescription", `My "Torrent" & <Client>`},
	}
	body, err := soapEnvelope(testServiceType, "AddPortMapping", args)
	if err != nil {
		t.Fatalf("soapEnvelope: %v", err)
	}

	iExt := strings.Index(body, "<NewExternalPort>")
	iProto := strings.Index(body, "<NewProtocol>")
	iDesc := strings.Index(body, "<NewPortMappingDescription>")
	if iExt < 0 || iProto < 0 || iDesc < 0 {
		t.Fatalf("not all arguments are present: %s", body)
	}
	if !(iExt < iProto && iProto < iDesc) {
		t.Errorf("arguments are out of order: %s", body)
	}
	if strings.Contains(body, `<Client>`) || strings.Contains(body, `"Torrent"`) {
		t.Errorf("the description was not escaped: %s", body)
	}
	if !strings.Contains(body, "&lt;Client&gt;") {
		t.Errorf("the escaped description is missing from the body: %s", body)
	}
	if !strings.Contains(body, `<u:AddPortMapping xmlns:u="`+testServiceType+`">`) {
		t.Errorf("the action element does not carry the service type: %s", body)
	}
	if !strings.HasSuffix(body, "</u:AddPortMapping></s:Body></s:Envelope>") {
		t.Errorf("the envelope is not closed correctly: %s", body)
	}
}

// The header shape router firmware refuses without: the quotes around the
// value are required by the specification.
func TestSoapCallSendsTheSpecifiedSOAPAction(t *testing.T) {
	d := &soapDoer{status: http.StatusOK, body: ""}
	if _, err := soapCall(context.Background(), d, testService(), "AddPortMapping", nil); err != nil {
		t.Fatalf("soapCall: %v", err)
	}
	want := `"` + testServiceType + `#AddPortMapping"`
	if d.gotAction != want {
		t.Errorf("SOAPAction = %q, want %q", d.gotAction, want)
	}
}

// What lets attemptOn act on a router's specific UPnP error code
// (upnpNoSuchEntry) instead of only displaying an opaque string.
func TestSoapCallReturnsAFaultErrorOnAFault(t *testing.T) {
	fault := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring>` +
		`<detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">` +
		`<errorCode>714</errorCode><errorDescription>NoSuchEntryInArray</errorDescription>` +
		`</UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	d := &soapDoer{status: http.StatusInternalServerError, body: fault}

	_, err := soapCall(context.Background(), d, testService(), "GetSpecificPortMappingEntry", nil)
	if err == nil {
		t.Fatal("soapCall: want an error for a 500 fault, got nil")
	}
	var fe *faultError
	if !errors.As(err, &fe) {
		t.Fatalf("soapCall error is not a *faultError: %v", err)
	}
	if fe.code != 714 || fe.desc != "NoSuchEntryInArray" {
		t.Errorf("fault = %+v, want code 714 desc NoSuchEntryInArray", fe)
	}
}

// A 500 whose body is not a UPnP fault at all, such as an HTML error page from
// a reverse proxy in front of the router's admin interface. Still an error,
// just not a *faultError.
func TestSoapCallRefusesAnUnparsableFailure(t *testing.T) {
	d := &soapDoer{status: http.StatusInternalServerError, body: "<html><body>Internal Server Error</body></html>"}
	_, err := soapCall(context.Background(), d, testService(), "AddPortMapping", nil)
	if err == nil {
		t.Fatal("soapCall: want an error, got nil")
	}
	var fe *faultError
	if errors.As(err, &fe) {
		t.Errorf("an HTML error page was read as a UPnP fault: %+v", fe)
	}
}

// hugeReader produces up to n bytes without allocating them at once, so the
// test that proves soapCall bounds its read can ask for far more than any real
// UPnP answer without itself being slow.
type hugeReader struct{ remaining int64 }

func (r *hugeReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'A'
	}
	r.remaining -= n
	return int(n), nil
}

// countingReader records how many bytes were pulled through it, so a bound
// enforced by io.LimitReader can be checked directly rather than inferred from
// how fast the test ran.
type countingReader struct {
	r     io.Reader
	total int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.total += int64(n)
	return n, err
}

// A gateway is a device on the LAN and not a trusted server, so a SOAP answer
// read without a cap is as much a denial-of-service vector as the device
// description fetch reconnect bounds with maxDescriptionBody. The response
// here offers 50MB, and the test counts what was consumed rather than waiting
// for it.
func TestSoapCallBoundsAnOversizedResponse(t *testing.T) {
	huge := &countingReader{r: &hugeReader{remaining: 50 << 20}}
	d := &soapDoer{status: http.StatusOK, bodyReader: huge}

	body, err := soapCall(context.Background(), d, testService(), "GetSpecificPortMappingEntry", nil)
	if err != nil {
		t.Fatalf("soapCall: %v", err)
	}
	if int64(len(body)) > maxSOAPBody {
		t.Errorf("soapCall returned %d bytes, want at most maxSOAPBody (%d)", len(body), maxSOAPBody)
	}
	if huge.total > maxSOAPBody {
		t.Errorf("soapCall read %d bytes off the wire, want at most maxSOAPBody (%d) - the reader offered 50MB and nothing capped it", huge.total, maxSOAPBody)
	}
}

// The ordinary case: a router answering about a mapping it has.
func TestParseSpecificEntryReadsClientAndPort(t *testing.T) {
	body := getEntryResponse("u", "192.168.1.50", "6881")
	client, port, ok := parseSpecificEntry([]byte(body))
	if !ok {
		t.Fatal("parseSpecificEntry: ok = false, want true")
	}
	if client != "192.168.1.50" || port != "6881" {
		t.Errorf("got client=%q port=%q, want 192.168.1.50/6881", client, port)
	}
}

// Firmware disagrees about whether the response element is prefixed u:,
// SOAP-ENV: or not at all, and encoding/xml matches on local name when the
// struct tag names no namespace, as in reconnect's soapFault.
func TestParseSpecificEntryIgnoresNamespacePrefix(t *testing.T) {
	for _, prefix := range []string{"u", "SOAP-ENV", ""} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			body := getEntryResponse(prefix, "10.0.0.5", "51413")
			client, port, ok := parseSpecificEntry([]byte(body))
			if !ok || client != "10.0.0.5" || port != "51413" {
				t.Errorf("prefix %q: got client=%q port=%q ok=%v, want 10.0.0.5/51413/true", prefix, client, port, ok)
			}
		})
	}
}

// Responses that must not read as a confirmed mapping: empty, truncated, and a
// body that is XML but not this response, such as the request echoed back,
// which old firmware does for an action it does not understand.
func TestParseSpecificEntryRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"not xml at all",
		`<?xml version="1.0"?><s:Envelope><s:Body><s:SomethingElse/></s:Body></s:Envelope>`,
		strings.Repeat("A", 200),
	}
	for i, body := range cases {
		if _, _, ok := parseSpecificEntry([]byte(body)); ok {
			t.Errorf("case %d: parseSpecificEntry accepted garbage as a confirmed entry: %q", i, body)
		}
	}
}

// getEntryResponse builds a GetSpecificPortMappingEntryResponse body with the
// given namespace prefix and internal client and port.
func getEntryResponse(prefix, client, port string) string {
	tag := "GetSpecificPortMappingEntryResponse"
	if prefix != "" {
		tag = prefix + ":" + tag
	}
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<s:Body><` + tag + ` xmlns:u="` + testServiceType + `">` +
		`<NewInternalPort>` + port + `</NewInternalPort>` +
		`<NewInternalClient>` + client + `</NewInternalClient>` +
		`<NewEnabled>1</NewEnabled>` +
		`</` + tag + `>` +
		`</s:Body></s:Envelope>`
}
