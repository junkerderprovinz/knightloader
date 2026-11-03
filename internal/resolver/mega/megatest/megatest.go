// Package megatest runs a stand-in for MEGA's API and download servers, with
// content encrypted the way MEGA encrypts it, for tests that must not leave
// the machine.
package megatest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Server answers the API calls and serves the downloads public links make.
type Server struct {
	URL string
	srv *httptest.Server

	mu            sync.Mutex
	files         map[string]*File
	folders       map[string]*folder
	quotaLeft     int64
	transferQuota bool
	challenge     string
	ranges        []string
}

// File is one shared file: its name, its content and its encrypted content,
// and the 32-byte key a link to it carries.
type File struct {
	Name   string
	Plain  []byte
	Cipher []byte
	Key    []byte
}

type folder struct {
	key   []byte
	nodes []Node
}

// Node is one entry of a shared folder.
type Node struct {
	handle, parent, name string
	file                 *File
	key                  []byte
}

// New starts a server that stops when the test ends.
func New(t testing.TB) *Server {
	s := &Server{files: map[string]*File{}, folders: map[string]*folder{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cs", s.api)
	mux.HandleFunc("GET /dl/{id}/{span}", s.download)
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// Client is an HTTP client that reaches the server.
func (s *Server) Client() *http.Client { return s.srv.Client() }

// Random is n random bytes.
func Random(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// NewFile encrypts plain under a fresh key whose last 8 bytes are the MAC of
// plain, as MEGA's own upload does.
func NewFile(name string, plain []byte) *File {
	aesKey, nonce := Random(16), Random(8)
	b, _ := aes.NewCipher(aesKey)
	iv := make([]byte, 16)
	copy(iv, nonce)
	ct := make([]byte, len(plain))
	cipher.NewCTR(b, iv).XORKeyStream(ct, plain)

	key := make([]byte, 32)
	copy(key[16:], nonce)
	copy(key[24:], MAC(aesKey, nonce, plain))
	subtle.XORBytes(key[:16], aesKey, key[16:])
	return &File{Name: name, Plain: plain, Cipher: ct, Key: key}
}

// MAC is MEGA's condensed MAC of plain, worked out over the whole content at
// once.
func MAC(aesKey, nonce, plain []byte) []byte {
	b, _ := aes.NewCipher(aesKey)
	iv := append(append([]byte{}, nonce...), nonce...)
	file := make([]byte, 16)
	for pos, n := 0, 1; pos < len(plain); n++ {
		size := 1 << 20
		if n <= 8 {
			size = n * 128 << 10
		}
		size = min(size, len(plain)-pos)
		chunk := make([]byte, (size+15)/16*16)
		copy(chunk, plain[pos:pos+size])
		out := make([]byte, len(chunk))
		cipher.NewCBCEncrypter(b, iv).CryptBlocks(out, chunk)
		subtle.XORBytes(file, file, out[len(out)-16:])
		b.Encrypt(file, file)
		pos += size
	}
	out := make([]byte, 8)
	subtle.XORBytes(out[:4], file[0:4], file[4:8])
	subtle.XORBytes(out[4:], file[8:12], file[12:16])
	return out
}

func (f *File) attrs() string {
	k := make([]byte, 16)
	subtle.XORBytes(k, f.Key[:16], f.Key[16:])
	return sealAttrs(k, f.Name)
}

func sealAttrs(key []byte, name string) string {
	raw, _ := json.Marshal(map[string]string{"n": name})
	plain := append([]byte("MEGA"), raw...)
	plain = append(plain, make([]byte, (16-len(plain)%16)%16)...)
	b, _ := aes.NewCipher(key)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(b, make([]byte, 16)).CryptBlocks(out, plain)
	return base64.RawURLEncoding.EncodeToString(out)
}

func sealKey(kek, key []byte) string {
	b, _ := aes.NewCipher(kek)
	out := make([]byte, len(key))
	for i := 0; i < len(key); i += 16 {
		b.Encrypt(out[i:], key[i:])
	}
	return base64.RawURLEncoding.EncodeToString(out)
}

// Share publishes f under handle and returns the link to it.
func (s *Server) Share(handle string, f *File) string {
	s.mu.Lock()
	s.files[handle] = f
	s.mu.Unlock()
	return "https://mega.nz/file/" + handle + "#" + base64.RawURLEncoding.EncodeToString(f.Key)
}

// ShareFolder publishes a folder under handle and returns the link to it.
// Each node names its parent; the root's parent is a handle that is not
// listed.
func (s *Server) ShareFolder(handle string, nodes ...Node) string {
	key := Random(16)
	for i := range nodes {
		if nodes[i].file == nil {
			nodes[i].key = Random(16)
		}
	}
	s.mu.Lock()
	s.folders[handle] = &folder{key: key, nodes: nodes}
	s.mu.Unlock()
	return "https://mega.nz/folder/" + handle + "#" + base64.RawURLEncoding.EncodeToString(key)
}

// Folder is a folder node.
func Folder(handle, parent, name string) Node {
	return Node{handle: handle, parent: parent, name: name}
}

// FileNode is a file node.
func FileNode(handle, parent string, f *File) Node {
	return Node{handle: handle, parent: parent, name: f.Name, file: f}
}

// SpendQuota makes every request for a download address answer that the
// transfer quota is used up for seconds more.
func (s *Server) SpendQuota(seconds int64) {
	s.mu.Lock()
	s.quotaLeft = seconds
	s.mu.Unlock()
}

// SpendBandwidth makes the download server answer 509, two minutes to go.
func (s *Server) SpendBandwidth() {
	s.mu.Lock()
	s.transferQuota = true
	s.mu.Unlock()
}

// Demand asks a proof of work, "1:easiness:time:token", of the next API call.
func (s *Server) Demand(challenge string) {
	s.mu.Lock()
	s.challenge = challenge
	s.mu.Unlock()
}

// Ranges are the byte ranges downloads asked for, in order.
func (s *Server) Ranges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.challenge != "" {
		if !s.solved(r.Header.Get("X-Hashcash")) {
			w.Header().Set("X-Hashcash", s.challenge)
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		s.challenge = ""
	}
	var cmds []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&cmds); err != nil || len(cmds) != 1 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cmd := cmds[0]
	answer := func(v any) { _ = json.NewEncoder(w).Encode([]any{v}) }
	shared := s.folders[r.URL.Query().Get("n")]

	switch cmd["a"] {
	case "f":
		if shared == nil {
			answer(-9)
			return
		}
		var list []map[string]any
		for _, n := range shared.nodes {
			e := map[string]any{"h": n.handle, "p": n.parent}
			if n.file != nil {
				e["t"], e["s"], e["a"], e["k"] = 0, len(n.file.Plain), n.file.attrs(), "owner:"+sealKey(shared.key, n.file.Key)
			} else {
				e["t"], e["a"], e["k"] = 1, sealAttrs(n.key, n.name), "owner:"+sealKey(shared.key, n.key)
			}
			list = append(list, e)
		}
		answer(map[string]any{"f": list})
	case "g":
		var f *File
		var id string
		if p, ok := cmd["p"].(string); ok {
			f, id = s.files[p], p
		} else if h, ok := cmd["n"].(string); ok && shared != nil {
			f, id = s.node(h), h
		}
		if f == nil {
			answer(-9)
			return
		}
		if cmd["g"] != nil && s.quotaLeft > 0 {
			answer(map[string]any{"e": -17, "tl": s.quotaLeft})
			return
		}
		a := map[string]any{"s": len(f.Plain), "at": f.attrs()}
		if cmd["g"] != nil {
			a["g"] = s.URL + "/dl/" + id
		}
		answer(a)
	default:
		answer(-2)
	}
}

// node finds a file in any shared folder. Caller holds s.mu.
func (s *Server) node(handle string) *File {
	for _, f := range s.folders {
		for _, n := range f.nodes {
			if n.handle == handle {
				return n.file
			}
		}
	}
	return nil
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transferQuota {
		w.Header().Set("X-MEGA-Time-Left", "120")
		w.WriteHeader(509)
		return
	}
	id := r.PathValue("id")
	f := s.files[id]
	if f == nil {
		f = s.node(id)
	}
	from, to, ok := strings.Cut(r.PathValue("span"), "-")
	start, err1 := strconv.Atoi(from)
	end, err2 := strconv.Atoi(to)
	if f == nil || !ok || err1 != nil || err2 != nil || start > end || end >= len(f.Cipher) {
		http.NotFound(w, r)
		return
	}
	s.ranges = append(s.ranges, r.PathValue("span"))
	_, _ = w.Write(f.Cipher[start : end+1])
}

// solved checks a proof of work the way MEGA does. Caller holds s.mu.
func (s *Server) solved(header string) bool {
	parts := strings.Split(header, ":")
	want := strings.Split(s.challenge, ":")
	if len(parts) != 3 || len(want) != 4 || parts[1] != want[3] {
		return false
	}
	prefix, err := base64.RawURLEncoding.DecodeString(parts[2])
	token, _ := base64.RawURLEncoding.DecodeString(want[3])
	if err != nil || len(prefix) != 4 {
		return false
	}
	e, _ := strconv.Atoi(want[1])
	buf := make([]byte, 4+262144*48)
	copy(buf, prefix)
	for i := range 262144 {
		copy(buf[4+i*48:4+(i+1)*48], token)
	}
	sum := sha256.Sum256(buf)
	return binary.BigEndian.Uint32(sum[:4]) <= uint32((e&63)<<1+1)<<((e>>6)*7+3)
}
