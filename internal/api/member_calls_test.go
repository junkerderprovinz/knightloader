package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// testSecret is the phrase secret groupOf puts an instance's group under when
// the test stored none.
const testSecret = "abababababababababababababababab"

// servers holds the server each app a test calls directly answers on.
var servers sync.Map

// serverOf is a server answering for a, started the first time a test asks.
func serverOf(t *testing.T, a *app.App) string {
	t.Helper()
	if srv, ok := servers.Load(a); ok {
		return srv.(*httptest.Server).URL
	}
	// The handler a test built already, since building another opens the
	// group again.
	h := a.SelfServeHandler()
	if h == nil {
		h = Handler(a)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		servers.Delete(a)
		srv.Close()
	})
	servers.Store(a, srv)
	return srv.URL
}

// groupOf is a's group, opened under testSecret when a holds no phrase yet.
func groupOf(t *testing.T, a *app.App) *group.Group {
	t.Helper()
	serverOf(t, a)
	if g := a.Federation.Group(); g != nil {
		return g
	}
	// The project relay is out of reach of a test.
	if a.Settings.Get().RelayModeOf() == settings.RelayModeProject {
		relayOff(t, a)
	}
	if groupSecret(a) == nil {
		if err := a.Accounts.Set(federation.SeedAccount, testSecret); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	applyRelay(a)
	g := a.Federation.Group()
	if g == nil {
		t.Fatal("the instance opened no group")
	}
	return g
}

// approve has a's group take the device label names as an approved member of
// kind, as if an instance of the group had passed the approval on.
func approve(t *testing.T, a *app.App, label string, kind member.Kind) {
	t.Helper()
	key := keyOf(label).Public().(ed25519.PublicKey)
	if err := groupOf(t, a).Import(group.Roster{Members: []group.Member{{ID: member.ID(kind, key), Kind: kind, Key: key, State: group.StateApproved}}}); err != nil {
		t.Fatal(err)
	}
}

// signedMember calls a's routes as the approved device of kind whose key
// label makes.
func signedMember(t *testing.T, a *app.App, label string, kind member.Kind) func(method, path string) (int, []byte) {
	t.Helper()
	approve(t, a, label, kind)
	return deviceAs(t, a, idAs(label, kind), kind, keyOf(label))
}

// asMember calls a's routes as an approved phone of the group.
func asMember(t *testing.T, a *app.App) func(relay.ProxyCall) (int, []byte) {
	t.Helper()
	call := signedBody(t, a, "phone-member", member.KindMobile)
	return func(c relay.ProxyCall) (int, []byte) { return call(c.Method, c.Path, c.Body) }
}

// signedBody is signedMember for calls that carry a body.
func signedBody(t *testing.T, a *app.App, label string, kind member.Kind) func(method, path string, body []byte) (int, []byte) {
	t.Helper()
	approve(t, a, label, kind)
	return func(method, path string, body []byte) (int, []byte) {
		return deviceCall(t, a, idAs(label, kind), kind, keyOf(label), relay.ProxyCall{Method: method, Path: path, Body: body})
	}
}

// deviceAs calls a's routes as member id signing as kind with key, approved or
// not. A nil key sends the calls unsigned.
func deviceAs(t *testing.T, a *app.App, id string, kind member.Kind, key ed25519.PrivateKey) func(method, path string) (int, []byte) {
	t.Helper()
	return func(method, path string) (int, []byte) {
		return deviceCall(t, a, id, kind, key, relay.ProxyCall{Method: method, Path: path})
	}
}

// deviceCall makes call to a on the direct route of its group, sealed and
// tagged under the group's keys and signed with key in id's name, the way a
// device holding the phrase on a's network sends it. It returns the answer.
func deviceCall(t *testing.T, a *app.App, id string, kind member.Kind, key ed25519.PrivateKey, call relay.ProxyCall) (int, []byte) {
	t.Helper()
	req, requestID := directRequest(t, a, id, kind, key, call)
	// Read before the call, which can take a out of its group.
	frameKey := federation.Domain.FrameKey(groupSecret(a))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, body
	}
	var out relay.ProxyResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the direct answer does not read: %s", body)
	}
	res, err := relay.OpenResult(frameKey, requestID, out.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	return res.Status, res.Body
}

// directRequest is call to a as deviceCall sends it, and its request id.
func directRequest(t *testing.T, a *app.App, id string, kind member.Kind, key ed25519.PrivateKey, call relay.ProxyCall) (*http.Request, string) {
	t.Helper()
	groupOf(t, a)
	secret, target := groupSecret(a), groupID(a)
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	requestID := hex.EncodeToString(raw)
	call.From, call.Kind = id, kind
	sealed, err := relay.SealCall(federation.Domain.FrameKey(secret), requestID, target, signedFor(key, requestID, target, call))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(relay.ProxyRequest{RequestID: requestID, Target: target, Sealed: sealed})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, serverOf(t, a)+group.DirectPath, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	now := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256(payload)
	mac := hmac.New(sha256.New, federation.Domain.PeerAuthKey(secret))
	mac.Write([]byte("direct\x00" + id + "\x00" + now + "\x00" + hex.EncodeToString(sum[:])))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Knightloader-Peer", id)
	req.Header.Set("X-Knightloader-Target", target)
	req.Header.Set("X-Knightloader-Time", now)
	req.Header.Set("X-Knightloader-Signature", hex.EncodeToString(mac.Sum(nil)))
	return req, requestID
}

// groupCall is a request as a phone of the group makes it once kit/group
// let it through.
func groupCall(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "127.0.0.1:8080"
	c := group.Caller{ID: idAs("phone-member", member.KindMobile), Kind: member.KindMobile}
	return r.WithContext(context.WithValue(r.Context(), memberCallKey{}, c))
}

// nasID is the member id of the instance servedMember stands for.
var nasID = strings.Repeat("d", 40)

// servedMember is a group whose one member, nas, is the instance at url,
// called over plain HTTP the way kit/group calls it sealed.
type servedMember struct{ url string }

func (m *servedMember) Members() []group.Member {
	return []group.Member{{ID: nasID, Kind: member.KindInstance, Name: "nas", Relay: true}}
}
func (m *servedMember) RelayConnected() bool { return true }
func (m *servedMember) Close() error         { return nil }
func (m *servedMember) Call(ctx context.Context, _, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, m.url+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
}
