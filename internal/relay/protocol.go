package relay

// The wire protocol both ends of a relay connection speak. Nothing in this
// file knows which end it is running on: the relay's own server (server.go)
// and the client an instance dials it with both marshal these same types, so
// a renamed field can never land on one side only and silently stop matching
// the other.
//
// Bodies travel as []byte, which encoding/json carries as base64. That is
// paid deliberately: the relay forwards a proxy frame verbatim without ever
// looking inside it, and one JSON frame per message keeps a client that can
// only speak plain WebSocket text (the mobile companion app) able to speak
// this protocol with a JSON parser and nothing else.
//
// File bytes DO travel this channel, and the comment here used to say they
// never did. GET /api/tasks/{id}/file (internal/api/routes_files.go) is a
// route like any other, and the relay allowlist admits it under its blanket
// "tasks/" prefix (routes_relay.go's relayForwardable). The bytes are sealed
// like every other payload, so the relay cannot read them - but they are
// base64 in a JSON frame, the relay sees their exact length, and a large file
// is a large frame rather than a stream. Worth knowing before anybody sizes a
// buffer or repeats the old claim in a privacy statement.

import "encoding/json"

// The frame types. Everything on this socket is one of these; anything else
// is ignored rather than treated as an error, see Server.Route.
const (
	// TypeHello is the first frame a client sends, and the only one carrying
	// the relay key. See Hello.
	TypeHello = "hello"
	// TypeAnnounce is relay -> client only: who else is on this key. A client
	// never sends one, it states its own identity in Hello instead.
	TypeAnnounce = "announce"
	// TypePresence is relay -> client only: a sibling's connection went away.
	TypePresence = "presence"
	// TypeProxyRequest wraps one call to a sibling's own REST API.
	TypeProxyRequest = "proxy-request"
	// TypeProxyResponse is that call's answer, matched by request ID.
	TypeProxyResponse = "proxy-response"
)

// AccountService is the service id the relay key is sealed under in the
// encrypted credential store (internal/accounts.Store.Set/Get), the same bare
// single-secret path a TorBox or debrid key already uses.
//
// It lives in this package rather than beside either of its two users because
// both of them - the API route that writes the key and the client that reads
// it back to dial with - already import the protocol they speak, and an id
// spelled out by hand in two places is the id that eventually disagrees with
// itself and silently loses somebody's stored key.
//
// Deliberately absent from accounts.Catalogue: that list is what the Accounts
// page offers as a row to configure, and the relay is neither a hoster nor a
// debrid service. Store.Set takes any service string, so nothing has to be
// registered there for this to work - only for it to show up somewhere it
// does not belong.
const AccountService = "relay"

// Envelope is every frame on this socket: a type discriminator plus the
// payload belonging to that type.
//
// Deliberately the same {type,data} shape internal/hub already broadcasts to
// the web UI, rather than a second convention invented for this package - one
// frame shape for every WebSocket in this project means a reader who has seen
// one has seen both.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Hello authenticates and identifies a connection in one frame.
//
// The key rides the first frame rather than a query parameter because a
// relay normally sits behind a reverse proxy (Nginx Proxy Manager, in this
// project's own deployment) whose access log records the request URL - a
// secret in the URL is a secret in a log file on a box the relay operator may
// not even own. A header would avoid that too, but the browser WebSocket API
// cannot set one, which would lock out any future client that is not a native
// process. The first frame is the one mechanism every client can use without
// leaking the key anywhere, so it is the one this protocol uses.
//
// Announce is carried inside Hello, not sent as its own frame, so that the
// relay can strip the key before telling siblings who arrived: they hold the
// same key already, but a secret that is never re-broadcast is one that
// cannot be leaked by a future bug in the fan-out path.
type Hello struct {
	Key      string   `json:"key"`
	Announce Announce `json:"announce"`
}

// Announce is one instance introducing itself to everyone else on its key.
// Receiving it means that instance is online right now - there is no separate
// presence(online) frame, because an arrival that needed two messages could
// be observed half-applied.
//
// This is the IN-MEMORY shape, with every field readable. On the wire the
// identity fields travel inside Sealed and the plaintext ones are empty; the
// two conversions live at the socket boundary in client.go, so nothing
// outside this package ever handles a half-opened announce. See Identity for
// why the split falls exactly where it does.
type Announce struct {
	// InstanceID is the address a proxy-request is routed to. It is the
	// instance's own stable identifier, not the relay's: the relay assigns
	// nothing and remembers nothing across a restart.
	//
	// It is the ONE field that cannot be sealed. The relay matches it against
	// ProxyRequest.Target to pick a socket, and a router that cannot read the
	// address cannot route. Everything else here can be, and now is.
	InstanceID string `json:"instanceId"`
	// Sealed carries this instance's Identity, encrypted under the frame key
	// with InstanceID as additional data. Empty in an announce written by a
	// version from before this existed, and empty in the in-memory form.
	//
	// Binding to InstanceID is what stops a relay lifting one instance's
	// sealed identity and presenting it as another's: the receiver computes
	// the same additional data from the id it can see, and a swap fails the
	// tag rather than opening into a believable lie.
	Sealed []byte `json:"sealed,omitempty"`

	// The three fields below are the identity. In the in-memory form they are
	// filled; on the wire a current version leaves them empty and puts them
	// in Sealed. They keep their json tags because an instance running a
	// version from before the seal still sends them that way, and reading
	// that peer is worth more than a tidier struct - see openAnnounce.

	// Name is what the Instances page shows - InstanceName if the user set
	// one, else the hostname, the same precedence pairingSelf already uses.
	//
	// The hostname fallback is the reason this field is sealed and not merely
	// tidied. A machine named after the person who owns it introduced that
	// person to the relay operator by name, every time it connected.
	Name string `json:"name,omitempty"`
	// Deployment is "container" or "desktop" (buildinfo.Deployment), so the
	// UI can tell a NAS install from a laptop without a second round trip.
	Deployment string `json:"deployment,omitempty"`
	// Client marks a connection that USES the relay without being an instance
	// on it: the mobile companion app, which calls its siblings but serves no
	// API of its own for them to call back.
	//
	// Every connection has to announce - the relay needs an InstanceID before
	// it will join one to a key - so without this flag a phone would land in
	// every sibling's instance list (federation.Manager.reachable adds every
	// sibling it sees) as an entry somebody can open, and which then answers
	// 501 to everything because there is nothing behind it. This says "route
	// to me if you must, but do not offer me as somewhere to go".
	//
	// omitempty, and read as false when absent, so an instance keeps
	// announcing exactly the frame it announced before this existed.
	Client bool `json:"client,omitempty"`
}

// Identity is the part of an announce the relay never needed and no longer
// gets: who this instance is, what kind of machine it runs on, and whether it
// is an instance at all or only something calling one.
//
// The test for what belongs here is short, and every field was checked
// against it rather than assumed: does internal/relay's SERVER read it? It
// reads InstanceID, in exactly three places - matching a reconnect to the
// connection it replaces, addressing a presence frame, and picking the target
// of a proxy request. It has never read a name, a deployment or the client
// flag; those were forwarded verbatim to siblings and were in the clear only
// because nothing had encrypted them yet. That is the same sentence seal.go
// opens with, and this is the same fix applied one layer up.
//
// What that leaves visible to a relay operator is worth stating plainly,
// because it is not nothing: the group key, the public IP each member dials
// in from, an instance id, and the timing of arrivals and departures. The id
// is a pseudonym rather than a name, which is the difference this change
// buys. It does not make a relay blind, and no UI text should say it does.
type Identity struct {
	Name       string `json:"name,omitempty"`
	Deployment string `json:"deployment,omitempty"`
	Client     bool   `json:"client,omitempty"`
}

// announceAAD is the additional data an announce's seal is bound to: the one
// routing field that necessarily stays in the clear. Same construction, same
// separator and a distinct label from requestAAD and responseAAD next door,
// so a blob from one frame type can never be opened as another.
func announceAAD(instanceID string) string {
	return "announce\x00" + instanceID
}

// SealIdentity seals the identity half of an announce. Exported for the same
// reason SealCall is: the mobile app and the extension carry their own ports
// of this protocol, and their tests check them against this one.
func SealIdentity(key []byte, instanceID string, id Identity) ([]byte, error) {
	plain, err := json.Marshal(id)
	if err != nil {
		return nil, err
	}
	return seal(key, announceAAD(instanceID), plain)
}

// OpenIdentity reverses SealIdentity.
func OpenIdentity(key []byte, instanceID string, sealed []byte) (Identity, error) {
	plain, err := open(key, announceAAD(instanceID), sealed)
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(plain, &id); err != nil {
		return Identity{}, ErrSealed
	}
	return id, nil
}

// sealAnnounce converts an in-memory announce to its wire form: the identity
// fields move into Sealed and are blanked where they used to travel.
//
// Blanking them is the entire point and is worth being explicit about,
// because the compatible-looking alternative - send both, let old peers read
// the plaintext - would have left the hostname on the wire and produced a
// change that reads as a fix while fixing nothing. A peer too old to open the
// seal shows an unnamed instance until it is updated, which is a cosmetic
// regression that ends by itself. A leak that stayed for compatibility would
// not have.
func sealAnnounce(frameKey []byte, a Announce) (Announce, error) {
	sealed, err := SealIdentity(frameKey, a.InstanceID, Identity{
		Name:       a.Name,
		Deployment: a.Deployment,
		Client:     a.Client,
	})
	if err != nil {
		return Announce{}, err
	}
	return Announce{InstanceID: a.InstanceID, Sealed: sealed}, nil
}

// openAnnounce converts a wire announce back to the in-memory form.
//
// Three cases, and the third is why this returns no error. A frame with a
// seal that opens is filled from it. A frame with no seal is a peer running a
// version from before this change, and its plaintext fields are used as they
// always were. A frame whose seal does NOT open is a peer on a different
// frame key - already unable to exchange a single proxy call with this one -
// and it is listed under its id with no name rather than dropped, because the
// relay says it is there and pretending otherwise would hide a real peer
// behind a key mismatch nobody could then diagnose.
func openAnnounce(frameKey []byte, a Announce) Announce {
	if len(a.Sealed) == 0 {
		return a
	}
	id, err := OpenIdentity(frameKey, a.InstanceID, a.Sealed)
	if err != nil {
		return Announce{InstanceID: a.InstanceID}
	}
	return Announce{
		InstanceID: a.InstanceID,
		Name:       id.Name,
		Deployment: id.Deployment,
		Client:     id.Client,
	}
}

// Presence reports that a sibling's connection state changed. The relay only
// ever sends Online=false: an arrival is an Announce, which carries the name
// and deployment a bare presence flag could not.
type Presence struct {
	InstanceID string `json:"instanceId"`
	Online     bool   `json:"online"`
}

// ProxyRequest is the WIRE form of one call to a sibling: the two fields the
// relay routes on, and a sealed blob holding everything else.
//
// The split is the whole security model of this channel. A relay needs to
// know which connection a frame is for and which answer it belongs to; it has
// never needed to know what was being asked. Everything it does not need is
// in Sealed, encrypted under a key derived from the connection secret
// (DeriveFrameKey) which the relay does not have - see ProxyCall for what
// that covers and seal.go for how.
type ProxyRequest struct {
	// RequestID is chosen by the caller and matches the response back to it.
	// The relay holds it only until the answer comes back.
	RequestID string `json:"requestId"`
	// Target is the InstanceID this call is for. It must be a sibling on the
	// same key; anything else is answered with an error response rather than
	// silently dropped, so a caller never waits out its own timeout for a
	// peer that simply is not connected.
	Target string `json:"target"`
	// Sealed is a ProxyCall, JSON-encoded and then sealed under the frame key
	// with RequestID and Target as additional data - so a relay cannot
	// redirect a sealed call to a different instance and have it open.
	Sealed []byte `json:"sealed,omitempty"`
}

// ProxyCall is what a ProxyRequest actually asks, once opened: the same
// (method, path, body) shape federation.Manager.Proxy already speaks over
// direct HTTP. The relay is a second transport for that call, not a second
// API.
//
// This type never travels as itself. It is sealed into ProxyRequest.Sealed on
// the way out and opened on the way in, so the only processes that ever see
// these fields are the two instances that share the secret.
type ProxyCall struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   []byte `json:"body,omitempty"`
	// Authorization, when set, becomes the Authorization header of the request
	// the target replays against its own API - the one credential that API
	// accepts from a caller holding no session cookie (internal/api's own
	// bearerToken).
	//
	// It exists for the mobile companion app, and only it fills it in.
	// Instance-to-instance calls leave it empty and keep behaving exactly as
	// before: federation.Manager has never attached a credential to a peer
	// call over either transport, and this does not change that.
	//
	// The app needs it because the relay is its ONLY channel to the target -
	// there is no second connection it could authenticate over. Without this
	// field a relay-connected phone could reach password-less instances only,
	// which are precisely the instances one would not expose to a relay in the
	// first place.
	//
	// Deliberately this one named field rather than a general header map: the
	// target replays these calls against its real handler, so a map would let
	// a caller set Host, X-Forwarded-For or a cookie and have the target
	// believe them. One field that can only ever be one header cannot be
	// turned into that.
	//
	// This field is why the sealing above was worth building. It is the one
	// thing on this channel that is a reusable credential rather than data:
	// a relay operator reading a task list learns what somebody downloaded,
	// while a relay operator reading a token can keep using it afterwards.
	// It is now inside the sealed blob like everything else, so pointing a
	// phone at somebody else's relay no longer means handing that somebody a
	// token. It should still be a named, revocable one (internal/apitoken)
	// rather than the account password - a credential you can withdraw is
	// worth having whether or not anybody can currently read it.
	Authorization string `json:"authorization,omitempty"`
}

// ProxyResponse is the WIRE form of the answer to exactly one ProxyRequest.
type ProxyResponse struct {
	RequestID string `json:"requestId"`
	// Sealed is a ProxyResult, sealed under the frame key with RequestID as
	// additional data. Empty when Error is set - see below.
	Sealed []byte `json:"sealed,omitempty"`
	// Error is set only when the RELAY itself answers instead of the target
	// (nobody is connected under that ID, or the sender is over its pending
	// budget). A response the target produced leaves it empty, however bad
	// its status code is: a 500 from the peer and "there is no peer" are
	// different failures to the caller.
	//
	// This one field stays in the clear, and it has to: the relay writes it,
	// and the relay has no key. What that costs is bounded and worth stating
	// - a hostile relay can fabricate "nobody is there" for a peer that is,
	// which is a denial of service it could equally perform by dropping the
	// frame. It cannot fabricate an ANSWER, because a response with no Sealed
	// blob is never mistaken for one: see Client.Proxy, which reads Error
	// first and never treats an unsealed response as a result.
	Error string `json:"error,omitempty"`
}

// ProxyResult is what a ProxyResponse actually answers, once opened.
type ProxyResult struct {
	Status int    `json:"status"`
	Body   []byte `json:"body,omitempty"`
}

// requestAAD and responseAAD are the additional data each direction binds its
// sealed blob to: the routing fields that necessarily travel in the clear.
//
// Both ends compute these from the fields they can see, so a relay that
// rewrites one of them produces a frame that fails its tag instead of one
// that opens into something believable. The separator cannot occur in an
// instance id (federation's own name validation) and the label differs per
// direction, so a request's blob can never be replayed as a response.
func requestAAD(requestID, target string) string {
	return "proxy-request\x00" + requestID + "\x00" + target
}

func responseAAD(requestID string) string {
	return "proxy-response\x00" + requestID
}

// SealCall seals one call for the wire. Exported because the mobile companion
// app's own protocol tests check their TypeScript port against it.
func SealCall(key []byte, requestID, target string, call ProxyCall) ([]byte, error) {
	plain, err := json.Marshal(call)
	if err != nil {
		return nil, err
	}
	return seal(key, requestAAD(requestID, target), plain)
}

// OpenCall reverses SealCall.
func OpenCall(key []byte, requestID, target string, sealed []byte) (ProxyCall, error) {
	plain, err := open(key, requestAAD(requestID, target), sealed)
	if err != nil {
		return ProxyCall{}, err
	}
	var call ProxyCall
	if err := json.Unmarshal(plain, &call); err != nil {
		// An opened-but-unparseable blob means the key was right and the
		// content was not - which no honest sender produces. Reported as the
		// same ErrSealed, because the caller's move is identical.
		return ProxyCall{}, ErrSealed
	}
	return call, nil
}

// SealResult seals one answer for the wire.
func SealResult(key []byte, requestID string, result ProxyResult) ([]byte, error) {
	plain, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return seal(key, responseAAD(requestID), plain)
}

// OpenResult reverses SealResult.
func OpenResult(key []byte, requestID string, sealed []byte) (ProxyResult, error) {
	plain, err := open(key, responseAAD(requestID), sealed)
	if err != nil {
		return ProxyResult{}, err
	}
	var res ProxyResult
	if err := json.Unmarshal(plain, &res); err != nil {
		return ProxyResult{}, ErrSealed
	}
	return res, nil
}

// Encode marshals one frame ready to be written to the socket.
func Encode(typ string, data any) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: typ, Data: payload})
}

// Decode reads a frame's envelope without touching its payload, so a routing
// decision can be made on the type alone and the original bytes can then be
// forwarded verbatim.
func Decode(frame []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(frame, &env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// Into unmarshals the payload of an already-decoded frame.
func (e Envelope) Into(v any) error {
	return json.Unmarshal(e.Data, v)
}
