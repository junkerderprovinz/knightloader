package proxycfg

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// serve runs handler over one accepted connection at a time until the test ends,
// and hands back the address. Each fake below speaks just enough of its protocol
// to answer one probe.
func serve(t *testing.T, handler func(net.Conn)) Entry {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				handler(c)
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(port)
	return Entry{ID: "1", Host: host, Port: n, Enabled: true}
}

// deadAddress is a port nothing is listening on: opened so the kernel picks a
// free one, then closed before anything can accept on it.
func deadAddress(t *testing.T) Entry {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	n, _ := strconv.Atoi(port)
	return Entry{ID: "1", Host: host, Port: n, Enabled: true}
}

func probe(t *testing.T, e Entry, target string) Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return Probe(ctx, e, target)
}

// --- an HTTP proxy that wants Basic credentials -----------------------------

// httpProxy answers a CONNECT. want is the credential it accepts; empty means it
// asks for none. The credential it was offered is reported on the channel, which
// is how the api test proves the stored password was the one sent.
func httpProxy(t *testing.T, want string, seen chan<- string) Entry {
	return serve(t, func(c net.Conn) {
		br := bufio.NewReader(c)
		var offered string
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if h := strings.TrimSpace(line); strings.HasPrefix(strings.ToLower(h), "proxy-authorization: basic ") {
				raw, _ := base64.StdEncoding.DecodeString(h[len("Proxy-Authorization: Basic "):])
				offered = string(raw)
			}
			if strings.TrimSpace(line) == "" {
				break
			}
		}
		if seen != nil {
			select {
			case seen <- offered:
			default:
			}
		}
		if want != "" && offered != want {
			_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
			return
		}
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
	})
}

func TestProbeHTTPChecksTheCredentialsAndNotJustThePort(t *testing.T) {
	e := httpProxy(t, "alice:secret", nil)
	e.Kind = KindHTTP
	e.Username, e.Password = "alice", "secret"

	t.Run("no target only proves the port is open, and says so", func(t *testing.T) {
		got := probe(t, e, "")
		if !got.OK || got.Stage != StageDial {
			t.Fatalf("got %+v, want a dial-stage success", got)
		}
		// The whole reason the stage is reported: a green tick that does not admit
		// it never looked at the password is the failure this replaces.
		if !strings.Contains(got.Detail, "Name a host") {
			t.Errorf("the report does not say what it left untested: %q", got.Detail)
		}
	})

	t.Run("a target carries the credentials", func(t *testing.T) {
		got := probe(t, e, "example.org")
		if !got.OK || got.Stage != StageConnect {
			t.Fatalf("got %+v, want a connect-stage success", got)
		}
	})

	t.Run("a wrong password is the proxy's own 407, not a generic failure", func(t *testing.T) {
		bad := e
		bad.Password = "wrong"
		got := probe(t, bad, "example.org")
		if got.OK || got.Stage != StageAuth {
			t.Fatalf("got %+v, want an auth-stage failure", got)
		}
		if !strings.Contains(got.Detail, "407") {
			t.Errorf("the report does not name the refusal: %q", got.Detail)
		}
	})

	t.Run("no credentials against a proxy that wants them", func(t *testing.T) {
		bare := e
		bare.Username, bare.Password = "", ""
		got := probe(t, bare, "example.org")
		if got.OK || !strings.Contains(got.Detail, "this row has none") {
			t.Errorf("got %+v, want it to say the row carries no credentials", got)
		}
	})
}

func TestProbeReportsAnAnswerThatIsNotHTTPAtAll(t *testing.T) {
	// A TLS proxy addressed as a plain one: the first thing back is a binary
	// alert, and "invalid response" would send somebody looking at the network
	// rather than at the type dropdown.
	e := serve(t, func(c net.Conn) {
		_, _ = bufio.NewReader(c).ReadString('\n')
		_, _ = c.Write([]byte{0x15, 0x03, 0x01, 0x00, 0x02}) // a TLS alert record
		time.Sleep(100 * time.Millisecond)
	})
	e.Kind = KindHTTP
	got := probe(t, e, "example.org")
	if got.OK || !strings.Contains(got.Detail, "https proxy addressed as http") {
		t.Errorf("got %+v, want the type mix-up named", got)
	}
}

// --- SOCKS5 -----------------------------------------------------------------

// socks5Proxy speaks the greeting, optionally RFC 1929, and one request.
func socks5Proxy(t *testing.T, wantUser, wantPass string, grant bool) Entry {
	return serve(t, func(c net.Conn) {
		head := make([]byte, 2)
		if _, err := io.ReadFull(c, head); err != nil {
			return
		}
		methods := make([]byte, head[1])
		if _, err := io.ReadFull(c, methods); err != nil {
			return
		}
		offers := func(m byte) bool {
			for _, x := range methods {
				if x == m {
					return true
				}
			}
			return false
		}
		if wantUser != "" {
			if !offers(methodUserPass) {
				_, _ = c.Write([]byte{5, methodNoneAcceptable})
				return
			}
			_, _ = c.Write([]byte{5, methodUserPass})
			ver := make([]byte, 2)
			if _, err := io.ReadFull(c, ver); err != nil {
				return
			}
			user := make([]byte, ver[1])
			_, _ = io.ReadFull(c, user)
			plen := make([]byte, 1)
			_, _ = io.ReadFull(c, plen)
			pass := make([]byte, plen[0])
			_, _ = io.ReadFull(c, pass)
			if string(user) != wantUser || string(pass) != wantPass {
				_, _ = c.Write([]byte{1, 1})
				return
			}
			_, _ = c.Write([]byte{1, 0})
		} else {
			_, _ = c.Write([]byte{5, methodNone})
		}
		// The request, when one comes. Only the verdict is written back; the
		// probe reads four bytes and hangs up.
		req := make([]byte, 4)
		if _, err := io.ReadFull(c, req); err != nil {
			return
		}
		if grant {
			_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		_, _ = c.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
	})
}

func TestProbeSOCKS5ChecksThePasswordWithNoTargetAtAll(t *testing.T) {
	// The point of doing this properly: SOCKS5 authenticates before anything is
	// named, so the credentials can be checked without involving a third party.
	e := socks5Proxy(t, "alice", "secret", true)
	e.Kind, e.Username, e.Password = KindSOCKS5, "alice", "secret"

	got := probe(t, e, "")
	if !got.OK || got.Stage != StageAuth {
		t.Fatalf("got %+v, want an auth-stage success with no target", got)
	}

	bad := e
	bad.Password = "wrong"
	if got := probe(t, bad, ""); got.OK || got.Stage != StageAuth {
		t.Errorf("got %+v, want a wrong password refused at the auth stage", got)
	}
}

func TestProbeSOCKS5DoesNotClaimToHaveCheckedAPasswordNobodyAskedFor(t *testing.T) {
	// A proxy that wants no authentication has not looked at this row's password,
	// so reporting success as "the credentials are fine" would be a green tick
	// for a value that has never been tested.
	e := socks5Proxy(t, "", "", true)
	e.Kind, e.Username, e.Password = KindSOCKS5, "alice", "secret"
	got := probe(t, e, "")
	if !got.OK {
		t.Fatalf("got %+v, want a success", got)
	}
	if !strings.Contains(got.Detail, "nothing here checked them") {
		t.Errorf("the report implies the credentials were checked: %q", got.Detail)
	}
}

func TestProbeSOCKS5ReportsWhyTheProxyWouldNotForward(t *testing.T) {
	e := socks5Proxy(t, "", "", false)
	e.Kind = KindSOCKS5
	got := probe(t, e, "example.org:443")
	if got.OK || got.Stage != StageConnect {
		t.Fatalf("got %+v, want a connect-stage failure", got)
	}
	if !strings.Contains(got.Detail, "not allowed by the proxy's rules") {
		t.Errorf("the reply code was not translated: %q", got.Detail)
	}
}

func TestProbeSOCKS5NamesTheVersionMixUp(t *testing.T) {
	// A SOCKS4 proxy with the row set to socks5. The first byte back is not 5,
	// and that is a dropdown mistake rather than a network fault.
	e := serve(t, func(c net.Conn) {
		_, _ = io.ReadFull(c, make([]byte, 3))
		_, _ = c.Write([]byte{0, 0x5a})
	})
	e.Kind = KindSOCKS5
	got := probe(t, e, "")
	if got.OK || !strings.Contains(got.Detail, "not 5") {
		t.Errorf("got %+v, want the version named", got)
	}
}

// --- SOCKS4 -----------------------------------------------------------------

func TestProbeSOCKS4SaysWhatADialAloneProves(t *testing.T) {
	// Nothing is exchanged in this protocol before a request, so a probe with no
	// target genuinely cannot say more than "something answered".
	e := serve(t, func(net.Conn) { time.Sleep(50 * time.Millisecond) })
	e.Kind = KindSOCKS4
	got := probe(t, e, "")
	if !got.OK || got.Stage != StageDial {
		t.Fatalf("got %+v, want a dial-stage success", got)
	}
	if !strings.Contains(got.Detail, "exchanges nothing before a request") {
		t.Errorf("the report overstates what it proved: %q", got.Detail)
	}
}

func TestProbeSOCKS4ASendsTheNameForTheProxyToResolve(t *testing.T) {
	got := make(chan string, 1)
	e := serve(t, func(c net.Conn) {
		head := make([]byte, 8)
		if _, err := io.ReadFull(c, head); err != nil {
			return
		}
		br := bufio.NewReader(c)
		userID, _ := br.ReadString(0)
		host, _ := br.ReadString(0)
		// 0.0.0.x with x non-zero is the SOCKS4a signal that a name follows.
		if head[4] == 0 && head[5] == 0 && head[6] == 0 && head[7] != 0 {
			got <- strings.TrimSuffix(host, "\x00") + "|" + strings.TrimSuffix(userID, "\x00")
		} else {
			got <- "resolved-here"
		}
		_, _ = c.Write([]byte{0, 0x5a, 0, 0, 0, 0, 0, 0})
	})
	e.Kind, e.Username = KindSOCKS4A, "alice"

	report := probe(t, e, "nowhere.invalid:1234")
	if !report.OK {
		t.Fatalf("got %+v, want a success", report)
	}
	select {
	case sent := <-got:
		// The whole difference between the two kinds: socks4a hands the name over
		// instead of resolving it on this machine, which is the point for anybody
		// proxying to reach names their own DNS cannot see.
		if sent != "nowhere.invalid|alice" {
			t.Errorf("the proxy was sent %q, want the unresolved name and the user id", sent)
		}
	case <-time.After(2 * time.Second):
		t.Error("the proxy was sent nothing")
	}
}

func TestProbeSOCKS4PointsAtSOCKS4AWhenTheNameWillNotResolveHere(t *testing.T) {
	e := serve(t, func(net.Conn) {})
	e.Kind = KindSOCKS4
	got := probe(t, e, "nowhere.invalid:1234")
	if got.OK || !strings.Contains(got.Detail, "socks4a") {
		t.Errorf("got %+v, want it to name the version that would work", got)
	}
}

// --- the rows that are not proxies ------------------------------------------

func TestProbeRefusesRowsThatNameNoEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		entry  Entry
		target string
		want   string
	}{
		{
			// none and direct are the pair the whole page has to keep apart, and
			// the refusal is where the difference is easiest to state.
			name:  "none is inert",
			entry: Entry{Kind: KindNone, Enabled: true},
			want:  "names no connection",
		},
		{
			name:  "direct has no endpoint of its own",
			entry: Entry{Kind: KindDirect, Enabled: true},
			want:  "has no endpoint of its own to test",
		},
		{
			// Verbatim from Validate, because Sanitize would drop this row on the
			// next save and a probe is the last chance to say so.
			name:  "an invalid row is refused in the validator's words",
			entry: Entry{Kind: KindHTTP, Host: "proxy.lan", Enabled: true},
			want:  "1-65535",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := probe(t, c.entry, c.target)
			if got.OK || got.Stage != StageRefused {
				t.Fatalf("got %+v, want a refusal", got)
			}
			if !strings.Contains(got.Detail, c.want) {
				t.Errorf("the refusal does not mention %q: %q", c.want, got.Detail)
			}
		})
	}
}

func TestProbeDirectAnswersTheOnlyQuestionADirectRowCanBeAsked(t *testing.T) {
	// A direct row exists to exclude a host from a whole-app proxy, so the useful
	// test is whether that host is reachable with no proxy in the way.
	e := serve(t, func(net.Conn) {})
	got := probe(t, Entry{Kind: KindDirect, Enabled: true}, net.JoinHostPort(e.Host, strconv.Itoa(e.Port)))
	if !got.OK || !strings.Contains(got.Detail, "no proxy in the way") {
		t.Errorf("got %+v, want a direct reachability success", got)
	}
}

func TestProbeReportsAnUnreachableProxyInWordsRatherThanInGoErrorText(t *testing.T) {
	e := deadAddress(t)
	e.Kind = KindHTTP
	got := probe(t, e, "")
	if got.OK || got.Stage != StageDial {
		t.Fatalf("got %+v, want a dial-stage failure", got)
	}
	if !strings.Contains(got.Detail, "nothing is listening on that port") {
		t.Errorf("the report does not name the actual failure: %q", got.Detail)
	}
	// Two things at once. "dial tcp 127.0.0.1:1: connectex: …" is three clauses
	// of transport plumbing around the one word that matters — and on Windows
	// that text is in the operating system's language, so letting it through puts
	// German in the middle of an English page.
	for _, leak := range []string{"dial tcp", "connectex", "connect:"} {
		if strings.Contains(got.Detail, leak) {
			t.Errorf("the report is raw operating-system error text: %q", got.Detail)
		}
	}
}

func TestProbeHonoursACancelledRequest(t *testing.T) {
	// A proxy that accepts and then says nothing. The browser going away has to
	// end this, or every abandoned test leaves a goroutine holding a socket.
	e := serve(t, func(net.Conn) { time.Sleep(30 * time.Second) })
	e.Kind, e.Username, e.Password = KindSOCKS5, "alice", "secret"

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := Probe(ctx, e, "")
	if got.OK {
		t.Fatalf("got %+v, want a failure", got)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the probe outlived its context by %s", time.Since(start))
	}
}
