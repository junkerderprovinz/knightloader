package cnl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
)

func TestAListeningKnightLoaderNamesItself(t *testing.T) {
	port := freePort(t)
	l := NewListener(&recorder{}, port)
	l.SetIdentity(func() Identity { return Identity{Name: "Desk", Instance: "inst-1", Kind: "desktop"} })
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Stop)

	id, err := Holder(context.Background(), port)
	if err != nil {
		t.Fatal(err)
	}
	if id != (Identity{App: "KnightLoader", Name: "Desk", Instance: "inst-1", Kind: "desktop"}) {
		t.Errorf("holder = %+v", id)
	}
}

// A website reaches the listener too, so who this is carries no CORS header
// and a page cannot read it.
func TestTheIdentityIsNotReadableByAWebsite(t *testing.T) {
	port := freePort(t)
	l := NewListener(&recorder{}, port)
	l.SetIdentity(func() Identity { return Identity{Name: "Desk"} })
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Stop)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/knightloader", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want none", got)
	}
}

func TestAnotherProgramOnThePortIsNotTakenForKnightLoader(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "JDownloader\r\n")
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	if _, err := Holder(context.Background(), ln.Addr().(*net.TCPAddr).Port); !errors.Is(err, ErrNotKnightLoader) {
		t.Errorf("err = %v, want ErrNotKnightLoader", err)
	}
}

func TestAFreePortHasNoHolder(t *testing.T) {
	_, err := Holder(context.Background(), freePort(t))
	if err == nil || errors.Is(err, ErrNotKnightLoader) {
		t.Errorf("err = %v, want the connection's own error", err)
	}
}
