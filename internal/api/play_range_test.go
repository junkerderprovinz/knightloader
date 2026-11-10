package api

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

func getRange(t *testing.T, srvURL, id, query string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(srvURL + "/api/tasks/" + id + "/range?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func TestARangeIsExactlyTheBytesItNames(t *testing.T) {
	t.Parallel()
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "film.mkv", []byte("0123456789abcdef"))

	resp, body := getRange(t, srv.URL, id, "from=3&length=4")
	if resp.StatusCode != http.StatusOK || string(body) != "3456" {
		t.Fatalf("range = %d %q, want 200 %q", resp.StatusCode, body, "3456")
	}
	if got := resp.Header.Get("Content-Length"); got != "4" {
		t.Errorf("Content-Length = %q, want 4", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
}

func TestARangeStopsAtTheEndOfTheFile(t *testing.T) {
	t.Parallel()
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "film.mkv", []byte("0123456789"))

	if resp, body := getRange(t, srv.URL, id, "from=7&length=100"); resp.StatusCode != http.StatusOK || string(body) != "789" {
		t.Errorf("a range over the end = %d %q, want 200 %q", resp.StatusCode, body, "789")
	}
	if resp, _ := getRange(t, srv.URL, id, "from=10&length=1"); resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("a range past the end = %d, want 416", resp.StatusCode)
	}
}

func TestARangeIsNoLargerThanARelayAnswerHolds(t *testing.T) {
	t.Parallel()
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "film.mkv", bytes.Repeat([]byte{7}, maxPlayRange+10))

	resp, body := getRange(t, srv.URL, id, "from=0&length="+strconv.Itoa(maxPlayRange+10))
	if resp.StatusCode != http.StatusOK || len(body) != maxPlayRange {
		t.Errorf("a long range = %d with %d bytes, want 200 with %d", resp.StatusCode, len(body), maxPlayRange)
	}
	if maxPlayRange >= maxRelayAnswer {
		t.Errorf("a range of %d bytes does not fit a relay answer of %d", maxPlayRange, maxRelayAnswer)
	}
}

func TestARangeThatDoesNotReadIsRefused(t *testing.T) {
	t.Parallel()
	a, base, srv := filesServer(t)
	id := putFile(t, a, base, "film.mkv", []byte("0123456789"))

	for _, q := range []string{"", "from=-1&length=2", "from=1", "from=x&length=2", "from=1&length=0"} {
		if resp, _ := getRange(t, srv.URL, id, q); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("range %q = %d, want 400", q, resp.StatusCode)
		}
	}
}

// joinedFilesApp is an instance in a group with one file to play, which a
// phone of the group calls.
func joinedFilesApp(t *testing.T, name string, data []byte) (string, func(method, path string) (int, []byte), func(path string) (int, []byte)) {
	t.Helper()
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	id := putFile(t, a, base, name, data)
	if err := a.Federation.SetJoined(time.Now()); err != nil {
		t.Fatal(err)
	}
	phone := signedMember(t, a, "phone-player", member.KindMobile)
	stream := func(path string) (int, []byte) {
		return streamCall(t, a, idAs("phone-player", member.KindMobile), keyOf("phone-player"), path)
	}
	return id, phone, stream
}

func TestAPlayAnswerSaysHowLongTheFileIsAndWhatItIs(t *testing.T) {
	t.Parallel()
	id, phone, _ := joinedFilesApp(t, "film.mkv", []byte("0123456789"))

	status, body := phone(http.MethodPost, "/api/tasks/"+id+"/play")
	var got struct {
		Path, Type string
		Size       int64
	}
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
		t.Fatalf("play = %d %s", status, body)
	}
	if got.Size != 10 || got.Type != "video/x-matroska" || got.Path == "" {
		t.Errorf("play = %+v, want the path, size 10 and video/x-matroska", got)
	}
}

func TestAPhoneReadsARangeOverTheRelay(t *testing.T) {
	t.Parallel()
	id, phone, _ := joinedFilesApp(t, "film.mkv", []byte("0123456789"))

	if status, body := phone(http.MethodGet, "/api/tasks/"+id+"/range?from=2&length=3"); status != http.StatusOK || string(body) != "234" {
		t.Errorf("a phone's range = %d %q, want 200 %q", status, body, "234")
	}
}

func TestAPhoneStreamsARangeOnTheLocalNetwork(t *testing.T) {
	t.Parallel()
	data := bytes.Repeat([]byte("frames of a film "), 20000)
	id, _, stream := joinedFilesApp(t, "film.mkv", data)

	status, body := stream("/api/tasks/" + id + "/range?from=5&length=200000")
	if status != http.StatusOK || !bytes.Equal(body, data[5:200005]) {
		t.Errorf("the stream = %d with %d bytes, want 200 with bytes 5 to 200005", status, len(body))
	}
}

// The phone learns from the hello where the instance takes calls on its
// network, and asks for it like any call.
func TestAPhoneReadsTheHelloOfTheInstance(t *testing.T) {
	t.Parallel()
	_, phone, _ := joinedFilesApp(t, "film.mkv", []byte("0123456789"))

	status, body := phone(http.MethodGet, "/kit/group/v1/hello")
	var hello struct{ ID string }
	if status != http.StatusOK || json.Unmarshal(body, &hello) != nil || hello.ID == "" {
		t.Errorf("the hello = %d %s, want 200 with the instance's id", status, body)
	}
}

// streamCall asks a for path as a stream on the direct route, the way a phone
// on its network does, and opens what comes back: a sealed head with the
// stream's key, the pieces sealed under that key, and the end.
func streamCall(t *testing.T, a *app.App, id string, key ed25519.PrivateKey, path string) (int, []byte) {
	t.Helper()
	req, requestID := directRequest(t, a, id, member.KindMobile, key, relay.ProxyCall{Method: http.MethodGet, Path: "/kit/group/v1/stream" + path})
	frameKey := federation.Domain.FrameKey(groupSecret(a))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	open := func(raw []byte) relay.ProxyResult {
		var out relay.ProxyResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("the answer does not read: %s", raw)
		}
		res, err := relay.OpenResult(frameKey, requestID, out.Sealed)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if resp.Header.Get("Content-Type") != "application/x-kit-stream" {
		raw, _ := io.ReadAll(resp.Body)
		res := open(raw)
		return res.Status, res.Body
	}
	piece := func() (byte, []byte) {
		var head [5]byte
		if _, err := io.ReadFull(resp.Body, head[:]); err != nil {
			t.Fatalf("the stream broke off: %v", err)
		}
		b := make([]byte, binary.BigEndian.Uint32(head[1:]))
		if _, err := io.ReadFull(resp.Body, b); err != nil {
			t.Fatalf("the stream broke off: %v", err)
		}
		return head[0], b
	}
	kind, raw := piece()
	if kind != 'h' {
		t.Fatalf("the stream began with a piece of kind %q", kind)
	}
	var head struct {
		Length int64
		Key    []byte
	}
	if res := open(raw); res.Status != http.StatusOK || json.Unmarshal(res.Body, &head) != nil {
		t.Fatalf("the head of the stream = %d %s", res.Status, res.Body)
	}
	block, err := aes.NewCipher(head.Key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	for n := uint64(0); ; n++ {
		kind, sealed := piece()
		nonce := make([]byte, 12)
		binary.BigEndian.PutUint64(nonce[4:], n)
		plain, err := aead.Open(nil, nonce, sealed, []byte{kind})
		if err != nil {
			t.Fatalf("piece %d does not open: %v", n, err)
		}
		if kind == 'e' {
			break
		}
		data = append(data, plain...)
	}
	if int64(len(data)) != head.Length {
		t.Fatalf("the stream carried %d bytes and announced %d", len(data), head.Length)
	}
	return http.StatusOK, data
}
