package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/cnl"
)

// otherKnightLoader holds a free Click'n'Load port the way a second instance
// on the same computer would, and returns the port.
func otherKnightLoader(t *testing.T, name, instance string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	l := cnl.NewListener(nil, port)
	l.SetIdentity(func() cnl.Identity { return cnl.Identity{Name: name, Instance: instance, Kind: "desktop"} })
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Stop)
	return port
}

// secondOnThePort is an app whose listener finds port taken.
func secondOnThePort(t *testing.T, port int) *app.App {
	t.Helper()
	a := testApp(t)
	a.CnL = cnl.NewListener(a, port)
	t.Cleanup(a.CnL.Stop)
	if err := a.CnL.Start(); err == nil {
		t.Fatal("the second listener bound a port the first one holds")
	}
	return a
}

func TestTheClickNLoadRowNamesTheKnightLoaderHoldingThePort(t *testing.T) {
	a := secondOnThePort(t, otherKnightLoader(t, "Desk", "inst-1"))

	f := featureRow(t, a, "cnl")
	if f.Enabled || f.DetailCode != "cnlHeldBy" || f.DetailArgs["name"] != "Desk" {
		t.Errorf("enabled %v, code %q, args %v; want off and cnlHeldBy naming Desk", f.Enabled, f.DetailCode, f.DetailArgs)
	}
}

func TestTheClickNLoadRowSaysWhenAnotherProgramHoldsThePort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "JDownloader\r\n")
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	a := secondOnThePort(t, ln.Addr().(*net.TCPAddr).Port)

	if f := featureRow(t, a, "cnl"); f.DetailCode != "cnlHeldByOther" {
		t.Errorf("code %q, want cnlHeldByOther", f.DetailCode)
	}
	var refusal *cnlRefusal
	if err := takeOverCnL(context.Background(), a); !errors.As(err, &refusal) || refusal.code != "cnlHeldByOther" {
		t.Errorf("take-over: %v, want the cnlHeldByOther refusal", err)
	}
}

// Only a member of the group can be asked to let go of the port.
func TestATakeOverFromAKnightLoaderOutsideTheGroupIsRefused(t *testing.T) {
	a := secondOnThePort(t, otherKnightLoader(t, "Desk", "inst-1"))

	var refusal *cnlRefusal
	err := takeOverCnL(context.Background(), a)
	if !errors.As(err, &refusal) || refusal.code != "cnlHolderOutsideGroup" || refusal.params["name"] != "Desk" {
		t.Fatalf("take-over: %v, want cnlHolderOutsideGroup naming Desk", err)
	}
	if a.CnL.Port() != 0 {
		t.Error("the refused take-over left this listener up")
	}
}

func TestAFreePortIsTakenOverAtOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	a := testApp(t)
	a.CnL = cnl.NewListener(a, port)
	t.Cleanup(a.CnL.Stop)

	if err := takeOverCnL(context.Background(), a); err != nil || a.CnL.Port() != port {
		t.Errorf("take-over of a free port: %v, listening on %d", err, a.CnL.Port())
	}
}

func TestReleaseLetsGoOfThePortAndTheRefusalCarriesItsCode(t *testing.T) {
	a := secondOnThePort(t, otherKnightLoader(t, "Desk", "inst-1"))
	reg := newRegistry()
	registerCnL(reg, a)
	h := http.NewServeMux()
	reg.attach(h, http.NotFoundHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/cnl/takeover", ""))
	var body struct {
		Code   string
		Params map[string]string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusBadRequest || body.Code != "cnlHolderOutsideGroup" {
		t.Errorf("take-over answered %d %q, want 400 cnlHolderOutsideGroup", rec.Code, rec.Body.String())
	}

	if !groupRoutes["POST /api/cnl/release"] {
		t.Error("a group member cannot reach the release route")
	}
}

func TestReleaseClosesThisListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	a := testApp(t)
	a.CnL = cnl.NewListener(a, port)
	t.Cleanup(a.CnL.Stop)
	if err := a.CnL.Start(); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	registerCnL(reg, a)
	h := http.NewServeMux()
	reg.attach(h, http.NotFoundHandler())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonRequest(http.MethodPost, "/api/cnl/release", ""))
	if rec.Code != http.StatusNoContent || a.CnL.Port() != 0 {
		t.Errorf("release answered %d and the listener is on %d, want 204 and closed", rec.Code, a.CnL.Port())
	}
}
