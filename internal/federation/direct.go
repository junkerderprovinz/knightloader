package federation

// The direct transport: members of one phrase group on the same network call
// each other over plain HTTP, and the relay is only needed across networks.
// A member is found by its tagged announce (internal/discovery). Every call
// is sealed under the frame key, as over the relay, and signed with the
// peer-auth key, so neither the network in between nor a stranger who reaches
// the route can read, forge or replay one.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/discovery"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// DirectPath is the route a member answers direct calls on.
const DirectPath = "/api/group/call"

const (
	headerPeer      = "X-Knightloader-Peer"
	headerTarget    = "X-Knightloader-Target"
	headerTime      = "X-Knightloader-Time"
	headerSignature = "X-Knightloader-Signature"
	// maxDirectCall caps a direct call as it arrives, before anything in it is
	// checked. Adding links is the largest call a member makes.
	maxDirectCall = 1 << 20
	// maxDirectCalls is how many direct calls are served at once.
	maxDirectCalls = 16
)

// LocalNetwork is the discovery service as the direct transport uses it.
type LocalNetwork interface {
	// Members is the members of this instance's group announcing on this
	// network.
	Members() []discovery.Peer
	// SetGroup tags this instance's announces with sign and accepts an
	// announce as a member's when isMember says so; nil for both leaves.
	SetGroup(sign func(discovery.Peer) string, isMember func(discovery.Peer) bool)
}

type groupKeys struct {
	frameKey []byte
	peerAuth []byte
}

// SetDiscovery installs the service members are found through, and hands it
// this instance's group if it is in one.
func (m *Manager) SetDiscovery(ln LocalNetwork) {
	m.mu.Lock()
	m.local = ln
	keys := m.keys
	m.mu.Unlock()
	configureLocal(ln, keys)
}

// SetGroup puts this instance into the group secret belongs to, or takes it
// out of any with a nil secret, for the direct transport. selfID is the id
// this instance answers direct calls under.
func (m *Manager) SetGroup(secret []byte, selfID string) {
	var keys *groupKeys
	if len(secret) > 0 {
		keys = &groupKeys{frameKey: relay.DeriveFrameKey(secret), peerAuth: relay.DerivePeerAuthKey(secret)}
	}
	m.mu.Lock()
	m.keys, m.selfID = keys, selfID
	ln := m.local
	m.mu.Unlock()
	configureLocal(ln, keys)
}

func configureLocal(ln LocalNetwork, keys *groupKeys) {
	if ln == nil {
		return
	}
	if keys == nil {
		ln.SetGroup(nil, nil)
		return
	}
	ln.SetGroup(func(p discovery.Peer) string { return announceTag(keys.peerAuth, p) }, func(p discovery.Peer) bool {
		if d := time.Since(time.Unix(p.Sent, 0)); d > relay.ClockSkew || d < -relay.ClockSkew {
			return false
		}
		return hmac.Equal([]byte(p.Tag), []byte(announceTag(keys.peerAuth, p)))
	})
}

// Replay is the guard every call into this instance passes, over the relay
// and directly, so a call captured on one path cannot run on the other.
func (m *Manager) Replay() *relay.ReplayGuard { return m.replay }

// announceTag signs everything an announce claims, its time included, so a
// device on the network can neither point members at an address of its
// choosing nor keep a departed member listed by playing its announces back.
func announceTag(peerAuth []byte, p discovery.Peer) string {
	mac := hmac.New(sha256.New, peerAuth)
	mac.Write([]byte("announce\x00" + p.ID + "\x00" + p.URL + "\x00" + p.Name + "\x00" + p.Deployment + "\x00" + strconv.FormatInt(p.Sent, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// directSignature binds a direct call's sender, time and exact body to the
// peer-auth key.
func directSignature(peerAuth []byte, sender, unix string, body []byte) string {
	sum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, peerAuth)
	mac.Write([]byte("direct\x00" + sender + "\x00" + unix + "\x00" + hex.EncodeToString(sum[:])))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *Manager) localMembers() []discovery.Peer {
	m.mu.Lock()
	ln, keys := m.local, m.keys
	m.mu.Unlock()
	if ln == nil || keys == nil {
		return nil
	}
	return ln.Members()
}

// callDirect makes one sealed, signed call to a member at url.
func (m *Manager) callDirect(ctx context.Context, url, target, method, path string, body []byte, authorization string) ([]byte, int, error) {
	m.mu.Lock()
	keys, self := m.keys, m.selfID
	m.mu.Unlock()
	if keys == nil {
		return nil, http.StatusNotFound, errors.New("federation: this instance is in no group")
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	id := hex.EncodeToString(raw)
	sealed, err := relay.SealCall(keys.frameKey, id, target, relay.ProxyCall{
		Method: method, Path: path, Body: body, Authorization: authorization,
	})
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	payload, err := json.Marshal(relay.ProxyRequest{RequestID: id, Target: target, Sealed: sealed})
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/")+DirectPath, bytes.NewReader(payload))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerPeer, self)
	req.Header.Set(headerTarget, target)
	req.Header.Set(headerTime, now)
	req.Header.Set(headerSignature, directSignature(keys.peerAuth, self, now, payload))

	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s is not reachable directly: %w", target, err)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s refused the direct call with HTTP %d", target, resp.StatusCode)
	}
	var out relay.ProxyResponse
	if err := json.Unmarshal(answer, &out); err != nil || out.RequestID != id {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s answered unreadably", target)
	}
	result, err := relay.OpenResult(keys.frameKey, id, out.Sealed)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("federation: %s answered unreadably: %w", target, err)
	}
	return result.Body, result.Status, nil
}

// ServeDirect answers a member's direct call with serve. Outside a group it
// answers like an instance without the feature. A call that is unsigned,
// stale, replayed, addressed to another instance or does not open is refused
// without saying which, since the reason would only help a guesser.
func (m *Manager) ServeDirect(w http.ResponseWriter, r *http.Request, serve relay.ProxyHandler) {
	m.mu.Lock()
	keys, self := m.keys, m.selfID
	m.mu.Unlock()
	if keys == nil {
		http.NotFound(w, r)
		return
	}
	// The route is open, so whatever a stranger can send without the key is
	// checked before the body is read.
	sender, unix := r.Header.Get(headerPeer), r.Header.Get(headerTime)
	sent, err := strconv.ParseInt(unix, 10, 64)
	if err != nil || sender == "" || r.Header.Get(headerTarget) != self || r.ContentLength > maxDirectCall {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if d := time.Since(time.Unix(sent, 0)); d > relay.ClockSkew || d < -relay.ClockSkew {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDirectCall))
	if err != nil || !hmac.Equal([]byte(r.Header.Get(headerSignature)), []byte(directSignature(keys.peerAuth, sender, unix, body))) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req relay.ProxyRequest
	if json.Unmarshal(body, &req) != nil || req.RequestID == "" || req.Target != self {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	call, err := relay.OpenCall(keys.frameKey, req.RequestID, self, req.Sealed)
	if err != nil || !m.replay.Admit(call) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	status, out := serve(r.Context(), call)
	sealed, err := relay.SealResult(keys.frameKey, req.RequestID, relay.ProxyResult{Status: status, Body: out})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(relay.ProxyResponse{RequestID: req.RequestID, Sealed: sealed})
}
