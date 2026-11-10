package federation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/phrase"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// What the phones, the browsers and the instances already in the field
// compute. A change here leaves every existing group unable to reach the
// updated instance. mobile/check-member-key.mjs, mobile/check-relay-url.mjs,
// mobile/check-pairing-code.mjs and extension/check-relay-seal.mjs read their
// cases from this file.

var (
	vectorSeed   = bytes.Repeat([]byte{0x2a}, ed25519.SeedSize)
	vectorTarget = "office"
	vectorCall   = relay.ProxyCall{
		ID: "req-7", Method: "POST", Path: "/api/links", Body: []byte(`{"urls":["https://example.com/a"]}`),
		From: "phone-member", Kind: member.KindMobile, Sent: 1760000000,
	}
)

const (
	vectorKey = "GX9rI+FshTLGq8g4+s1ep4m+DHaykgM0A5v6iz02jWE="
	vectorSig = "2DRpY1IMpnl6W0mqYgRv6Nq2SDimJFXhcYv2I4d0LDAPGzf3vdgfuf2ty0nb1bchk9xZXmMdHkTGFQOHhGvhDg=="
)

const (
	vectorInstanceID = "b600306cfa76723fdec395e53a9b3d9fdb78b1e2"
	vectorPhoneID    = "phone-b600306cfa76723fdec395e53a9b3d9fdb78b1e2d7a23c32fcbcd2dc6d0c4092"
	vectorBrowserID  = "browser-b600306cfa76723fdec395e53a9b3d9fdb78b1e2d7a23c32fcbcd2dc6d0c4092"
)

// The answer and the hello the vector seed signs. The answer is to the call
// that arrived as vectorSealedCall, which it names by the hash of those bytes
// alone, so any bytes serve.
var (
	vectorResult     = relay.ProxyResult{Status: 410, Body: []byte("removed")}
	vectorRelayKey   = "relay-key-of-the-vector-0123456789"
	vectorRelayHost  = "relay.example.com"
	vectorChallenge  = "XFxcXFxcXFxcXFxcXFxcXFxcXFxcXFxcXFxcXFxcXFw="
	vectorSealedCall = "MzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMw=="
)

const (
	vectorResultSig   = "efDaJoYEIIwKg5Z98dMHRSb3VtLTEx/lrphppJjTcHvDTTdEHRa9WZuRGNfTzdqgIXFzVed4Hq/TdRjWFncHBQ=="
	vectorHelloSig    = "U/kIXyBTKTsSTbXyWn3IqdWTgkEKOfsDtY9+asOM8WTn7jG9XHeF9/toZ1+60J+ap70GId2ySigv7ZFT3PhvBg=="
	vectorFingerprint = "b600 306c fa76 723f"
)

func decoded(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func vectorIdentity(t *testing.T, kind member.Kind) member.Identity {
	t.Helper()
	id, err := member.FromSeed(kind, vectorSeed)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The values are the SHA-256 of the label followed by the secret, computed
// with sha256sum, such as printf 'knightloader/relay/frame-key/v1' | sha256sum.
func TestTheKeysAPhraseMakesStayTheSame(t *testing.T) {
	walk := []byte{0x00, 0x07, 0x0e, 0x15, 0x1c, 0x23, 0x2a, 0x31, 0x38, 0x3f, 0x46, 0x4d, 0x54, 0x5b, 0x62, 0x69}
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"group key of the zero secret", Domain.GroupKey(make([]byte, 16)), "293fa85653adb9e7195717c1a6f34e3f433f0a610cd247bb9ff73b8642e7fc15"},
		{"group key of a walk over the bytes", Domain.GroupKey(walk), "57d28da6bdc93397c9a3a8355b9c3e322a7e134b9985ae7e76c3667d240b5b52"},
		{"frame key of nothing", hex.EncodeToString(Domain.FrameKey(nil)), "a94ace9821d4aa96f2f8c4bb250e2937a59655f53a14ba74c3e589d5f71f17b9"},
		{"frame key of a word", hex.EncodeToString(Domain.FrameKey([]byte("knightloader"))), "e04113452443266a47195c6b866225201d6312a88a6cfc0e2af1ecf5a4beeb88"},
		{"peer-auth key of nothing", hex.EncodeToString(Domain.PeerAuthKey(nil)), "d5fd8bbc11c8356f31352cd5c560e5d04a4ade1eba27c80fa7e8c05ddefd78b9"},
		{"peer-auth key of a word", hex.EncodeToString(Domain.PeerAuthKey([]byte("knightloader"))), "21790e0c4ac07b8589440547a85bb3f6243a99d55190d2a118065cc0d230e739"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %s, want %s", tc.name, tc.got, tc.want)
		}
	}
}

func TestTheAllZeroSecretIsTheCanonicalPhrase(t *testing.T) {
	words, err := phrase.Encode(make([]byte, phrase.SecretLen))
	if err != nil {
		t.Fatal(err)
	}
	if want := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"; words != want {
		t.Fatalf("the zero secret is %q, want %q", words, want)
	}
}

// The Pairing tab names a mistyped word before anything is sent, from its own
// copy of the list, so a word it lets through has to be one the server takes.
func TestTheWebUIChecksWordsAgainstThisList(t *testing.T) {
	web, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "bip39-english.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(phrase.Wordlist(), "\n") + "\n"; string(web) != want {
		t.Fatal("web/src/lib/bip39-english.txt differs from the word list the server decodes with")
	}
}

func TestAMembersIDIsMadeFromItsKey(t *testing.T) {
	for kind, want := range map[member.Kind]string{member.KindInstance: vectorInstanceID, member.KindMobile: vectorPhoneID, member.KindExtension: vectorBrowserID} {
		if got := vectorIdentity(t, kind).ID(); got != want {
			t.Errorf("the %s id = %s, want %s", kind, got, want)
		}
	}
}

func TestTheMemberCallVectorStaysTheSame(t *testing.T) {
	id := vectorIdentity(t, member.KindMobile)
	if got := base64.StdEncoding.EncodeToString(id.Public()); got != vectorKey {
		t.Errorf("public key = %s, want %s", got, vectorKey)
	}
	if got := base64.StdEncoding.EncodeToString(id.Sign(relay.CallMessage(Signing, vectorTarget, vectorCall))); got != vectorSig {
		t.Errorf("signature = %s, want %s; the app and the extension sign another message", got, vectorSig)
	}
}

func TestTheAnswerAndHelloVectorsStayTheSame(t *testing.T) {
	instance := vectorIdentity(t, member.KindInstance)
	signed := relay.SignResult(Signing, &instance, vectorCall.ID, decoded(t, vectorSealedCall), vectorResult)
	if got := base64.StdEncoding.EncodeToString(signed.Sig); got != vectorResultSig {
		t.Errorf("answer signature = %s, want %s; the app and the extension check another message", got, vectorResultSig)
	}
	if !relay.VerifyResult(Signing, vectorCall.ID, vectorInstanceID, decoded(t, vectorSealedCall), signed) {
		t.Error("the vector answer does not verify as the instance's")
	}
	phone := vectorIdentity(t, member.KindMobile)
	proof := phone.ProveHello(vectorRelayKey, vectorRelayHost, decoded(t, vectorChallenge))
	if got := base64.StdEncoding.EncodeToString(proof.Sig); got != vectorHelloSig {
		t.Errorf("hello signature = %s, want %s; the app proves its id with another message", got, vectorHelloSig)
	}
}

func TestTheFingerprintVectorStaysTheSame(t *testing.T) {
	if got := member.Fingerprint(vectorIdentity(t, member.KindMobile).Public()); got != vectorFingerprint {
		t.Errorf("fingerprint = %q, want %q; the app and the extension show their keys in another form", got, vectorFingerprint)
	}
}

// The frame and the identity were sealed by the phone's port
// (mobile/src/api/relayFrame.ts) with fixed nonces, so they fail if either
// side changes the framing, the additional data, the label or a field name.
func TestOpensWhatThePhoneSealed(t *testing.T) {
	key := Domain.FrameKey([]byte("cross-implementation vector"))
	call, err := relay.OpenCall(key, Signing, "req-2", "alpha", decoded(t,
		"AwMDAwMDAwMDAwMDAXqxEhwJlzwnjSCTaEIl6yMvi98geuQjUvrQqyH8fBq7GmILoC34oL2V8ZfcU3G2JuI/liRh+cyXI7fOVBKffbJbA+V7b5nzgkzepCne/45/VHY="))
	if err != nil {
		t.Fatalf("a frame the phone sealed does not open: %v", err)
	}
	if call.Method != "GET" || call.Path != "/api/tasks" || call.ID != "req-2" || call.Sent != 1_800_000_000 {
		t.Errorf("opened %+v, want the GET /api/tasks the phone sealed as req-2", call)
	}
	id, err := relay.OpenIdentity(key, "phone", decoded(t,
		"BwcHBwcHBwcHBwcHKowzi1tX9hS/RbpFD36F1jz5pHjOvE8p9pXq7oULX/Xf0cCMQxbXtTdsUR7tCWHualBstwHbZzUY0vpg/urebU1me215Eg=="))
	if err != nil {
		t.Fatalf("an identity the phone sealed does not open: %v", err)
	}
	if id.Name != "Pixel 8" || id.Deployment != "mobile" || !id.Client {
		t.Errorf("opened %+v, want the phone's own announce", id)
	}
}

func TestABrokenSignatureKeepsItsRefusal(t *testing.T) {
	want := relay.RefusalBody("memberSignature", "the signature on this call does not match the key it came with")
	if !bytes.Equal(relay.SignatureRefused, want) {
		t.Errorf("a broken signature is refused with %s, want %s", relay.SignatureRefused, want)
	}
}

func TestPairingCode(t *testing.T) {
	const phrase = "abandon ability able about above absent absorb abstract absurd abuse access accident"
	tests := []struct {
		name  string
		relay string
		want  string
	}{
		{"no relay of its own", "", phrase},
		{"the project relay", relay.DefaultURL, phrase},
		{"the project relay as typed", "https://parleyport.halleluja.design", phrase},
		{"an address the instance cannot dial", "relay.example.com", phrase},
		{"its own relay", "https://relay.example.com", phrase + "\nwss://relay.example.com/relay/connect"},
		{"its own relay under a path", "https://example.com/kl", phrase + "\nwss://example.com/kl/relay/connect"},
		{"a relay without TLS is named as it is dialled", "http://192.168.20.30:8760", phrase + "\nws://192.168.20.30:8760/relay/connect"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := relay.PairingCode(phrase, tc.relay); got != tc.want {
				t.Fatalf("PairingCode(%q) = %q, want %q", tc.relay, got, tc.want)
			}
		})
	}
}

// TestConnectURL: the address a relay setting is dialled at, "" for one that
// cannot be dialled. The pairing code names it, which is how it is read here.
func TestConnectURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"https gets the path appended", "https://relay.example.com", "wss://relay.example.com/relay/connect"},
		{"a trailing slash is a bare host too", "https://relay.example.com/", "wss://relay.example.com/relay/connect"},
		{"http and a port", "http://192.168.20.30:8760", "ws://192.168.20.30:8760/relay/connect"},
		{"whitespace from a settings field", "  https://relay.example.com  ", "wss://relay.example.com/relay/connect"},
		{"a ws url is already one", "ws://127.0.0.1:8760/relay/connect", "ws://127.0.0.1:8760/relay/connect"},
		{"a mounted path is kept", "https://home.example.com/kl-relay/connect", "wss://home.example.com/kl-relay/connect"},
		{"an instance under a path has the relay below it", "https://example.com/kl", "wss://example.com/kl/relay/connect"},
		{"and with a trailing slash", "https://example.com/apps/kl/", "wss://example.com/apps/kl/relay/connect"},
		{"a full address under a path is kept", "https://example.com/kl/relay/connect", "wss://example.com/kl/relay/connect"},
		{"a slash after the socket goes", "https://example.com/kl/relay/connect/", "wss://example.com/kl/relay/connect"},
		{"no scheme", "relay.example.com", ""},
		{"a scheme nobody can dial", "ftp://relay.example.com", ""},
		{"empty", "", ""},
	}
	const words = "words"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := words
			if tc.want != "" {
				want += "\n" + tc.want
			}
			if got := relay.PairingCode(words, tc.in); got != want {
				t.Errorf("%q is dialled as %q, want %q", tc.in, strings.TrimPrefix(got, words+"\n"), tc.want)
			}
		})
	}
}

// TestRelayHost: the relay a hello's proof names.
func TestRelayHost(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a dialled address", "wss://relay.example.com/relay/connect", "relay.example.com"},
		{"the port goes", "ws://192.168.20.30:8760/relay/connect", "192.168.20.30"},
		{"in lower case", "https://Relay.Example.COM", "relay.example.com"},
		{"without the dot of a full name", "https://relay.example.com./kl", "relay.example.com"},
		{"an IPv6 literal without its brackets", "ws://[::1]:8760/relay/connect", "::1"},
		{"a bare host", "relay.example.com", "relay.example.com"},
		{"a bare host and a port", "127.0.0.1:8760", "127.0.0.1"},
		{"whitespace from a setting", "  https://relay.example.com  ", "relay.example.com"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		if got := relay.RelayHost(tc.in); got != tc.want {
			t.Errorf("%s: RelayHost(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// kitRefusals are the codes kit/group turns a member's call away with. The
// refusal checks of the phone and the extension read them from here, since
// the kit's source is not in this repository.
var kitRefusals = []string{
	"answerTooLarge",
	"groupFull",
	"groupJoinsBusy",
	"memberBusy",
	"memberKeyChanged",
	"memberKind",
	"memberKindRefused",
	"memberNotPinned",
	"memberPending",
	"memberSignature",
	"memberUnnamed",
	"memberUnsigned",
}

func TestTheKitRefusesWithTheListedCodes(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/junkerderprovinz/parleyport/kit").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir := strings.TrimSpace(string(out))
	code := regexp.MustCompile(`(?:refuse\([^,]+,|RefusalBody\()\s*"([A-Za-z]+)"`)
	found := map[string]bool{}
	for _, pkg := range []string{"group", "relay"} {
		files, err := filepath.Glob(filepath.Join(dir, pkg, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range code.FindAllStringSubmatch(string(b), -1) {
				found[m[1]] = true
			}
		}
	}
	got := slices.Sorted(maps.Keys(found))
	if !slices.Equal(got, kitRefusals) {
		t.Errorf("the kit refuses with %v, kitRefusals lists %v", got, kitRefusals)
	}
}
