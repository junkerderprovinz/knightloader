package signinbrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// chanTransport is one end of a DevTools connection held in channels.
type chanTransport struct {
	in   chan []byte
	out  chan []byte
	done chan struct{}
}

func newChanTransport() *chanTransport {
	return &chanTransport{in: make(chan []byte, 64), out: make(chan []byte, 64), done: make(chan struct{})}
}

func (c *chanTransport) read() ([]byte, error) {
	select {
	case b := <-c.in:
		return b, nil
	case <-c.done:
		return nil, io.EOF
	}
}

func (c *chanTransport) write(b []byte) error {
	select {
	case c.out <- b:
		return nil
	case <-c.done:
		return io.ErrClosedPipe
	}
}

func (c *chanTransport) close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

func TestACallGetsTheAnswerWithItsOwnID(t *testing.T) {
	tr := newChanTransport()
	c := newClient(tr)
	defer c.close()
	go func() {
		var m message
		_ = json.Unmarshal(<-tr.out, &m)
		// Another call's answer and an event arrive first.
		tr.in <- []byte(`{"id":999,"result":{"product":"wrong"}}`)
		tr.in <- []byte(`{"method":"Page.loadEventFired","params":{},"sessionId":"S"}`)
		tr.in <- []byte(`{"id":` + jsonInt(m.ID) + `,"result":{"product":"Chrome/158"}}`)
	}()
	var got struct{ Product string }
	if err := c.call(context.Background(), "", "Browser.getVersion", nil, &got); err != nil {
		t.Fatal(err)
	}
	if got.Product != "Chrome/158" {
		t.Errorf("product = %q, want the answer to this call", got.Product)
	}
	select {
	case ev := <-c.events():
		if ev.method != "Page.loadEventFired" || ev.session != "S" {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("the event never arrived")
	}
}

func TestAnErrorFromChromiumIsTheCallsError(t *testing.T) {
	tr := newChanTransport()
	c := newClient(tr)
	defer c.close()
	go func() {
		var m message
		_ = json.Unmarshal(<-tr.out, &m)
		tr.in <- []byte(`{"id":` + jsonInt(m.ID) + `,"error":{"code":-32601,"message":"'Nope.nope' wasn't found"}}`)
	}()
	err := c.call(context.Background(), "", "Nope.nope", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "wasn't found") {
		t.Errorf("err = %v, want Chromium's message", err)
	}
}

func TestACallEndsWhenTheBrowserGoes(t *testing.T) {
	tr := newChanTransport()
	c := newClient(tr)
	go func() {
		<-tr.out
		tr.close()
	}()
	if err := c.call(context.Background(), "", "Browser.getVersion", nil, nil); !errors.Is(err, errGone) {
		t.Errorf("err = %v, want errGone", err)
	}
}

// Events keep coming while nobody reads them, so the reader never stalls an
// answer behind them.
func TestEventsQueueWithoutHoldingUpAnswers(t *testing.T) {
	tr := newChanTransport()
	c := newClient(tr)
	defer c.close()
	go func() {
		var m message
		_ = json.Unmarshal(<-tr.out, &m)
		for range 500 {
			tr.in <- []byte(`{"method":"Network.dataReceived","params":{}}`)
		}
		tr.in <- []byte(`{"id":` + jsonInt(m.ID) + `,"result":{}}`)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.call(ctx, "", "Page.enable", nil, nil); err != nil {
		t.Fatal(err)
	}
	for i := range 500 {
		select {
		case <-c.events():
		case <-time.After(time.Second):
			t.Fatalf("only %d of 500 events arrived", i)
		}
	}
}

func TestThePipeCarriesMessagesEndedByANullByte(t *testing.T) {
	var sent bytes.Buffer
	p := newPipe(strings.NewReader(`{"id":1}`+"\x00"+`{"id":2}`+"\x00"), nopCloser{&sent}, nil)
	for _, want := range []string{`{"id":1}`, `{"id":2}`} {
		got, err := p.read()
		if err != nil || string(got) != want {
			t.Errorf("read %q, %v, want %q", got, err, want)
		}
	}
	if _, err := p.read(); err == nil {
		t.Error("a read past the end succeeded")
	}
	if err := p.write([]byte(`{"id":3}`)); err != nil {
		t.Fatal(err)
	}
	if sent.String() != `{"id":3}`+"\x00" {
		t.Errorf("wrote %q, want the message and a null byte", sent.String())
	}
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestASentCommandGoesOutWithoutWaitingForItsAnswer(t *testing.T) {
	tr := newChanTransport()
	c := newClient(tr)
	defer c.close()
	if err := c.send("S", "Page.enable", nil); err != nil {
		t.Fatal(err)
	}
	var m message
	if err := json.Unmarshal(<-tr.out, &m); err != nil || m.Method != "Page.enable" || m.SessionID != "S" || m.ID == 0 {
		t.Errorf("sent %+v, %v", m, err)
	}
	// Its answer, when it comes, is dropped.
	tr.in <- []byte(`{"id":` + jsonInt(m.ID) + `,"result":{}}`)
}
