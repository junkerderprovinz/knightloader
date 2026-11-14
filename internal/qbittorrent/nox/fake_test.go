package nox

import (
	"bufio"
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/qbittest"
)

// The test binary plays qbittorrent-nox when fakeEnv names how. It reads the
// port and the login from the qBittorrent.conf KnightLoader wrote, checks a
// login against the PBKDF2 hash there, and quits on app/shutdown.
const fakeEnv = "NOX_FAKE"

const (
	fakeGood          = "good"
	fakeCrashOnce     = "crash-once"
	fakeDeafToRequest = "deaf"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeEnv); mode != "" {
		fakeMain(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeMain(mode string) {
	var profile string
	for _, a := range os.Args[1:] {
		if a == "--version" {
			fmt.Println("qBittorrent v4.5.5")
			os.Exit(0)
		}
		if v, ok := strings.CutPrefix(a, "--profile="); ok {
			profile = v
		}
	}
	if profile == "" {
		fmt.Fprintln(os.Stderr, "no --profile")
		os.Exit(2)
	}
	if mode == fakeCrashOnce {
		marker := filepath.Join(profile, "crashed-once")
		if _, err := os.Stat(marker); err != nil {
			os.WriteFile(marker, nil, 0o600)
			fmt.Fprintln(os.Stderr, "fake crash")
			os.Exit(3)
		}
	}
	conf, err := os.ReadFile(ConfigPath(profile))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	values := iniValues(conf)
	port := values[`WebUI\Port`]
	user := values[`WebUI\Username`]
	m := regexp.MustCompile(`^"@ByteArray\(([^:]+):([^)]+)\)"$`).FindStringSubmatch(values[`WebUI\Password_PBKDF2`])
	if m == nil {
		fmt.Fprintln(os.Stderr, "no password hash")
		os.Exit(2)
	}
	salt, _ := base64.StdEncoding.DecodeString(m[1])
	want, _ := base64.StdEncoding.DecodeString(m[2])
	q := qbittest.New(user, "")
	q.Check = func(u, p string) bool {
		got, _ := pbkdf2.Key(sha512.New, p, salt, pbkdf2Rounds, pbkdf2KeyLen)
		return u == user && bytes.Equal(got, want)
	}
	if mode != fakeDeafToRequest {
		q.OnShutdown = func() {
			go func() {
				time.Sleep(50 * time.Millisecond)
				os.Exit(0)
			}()
		}
	}
	ln, err := net.Listen("tcp", values[`WebUI\Address`]+":"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	fmt.Fprintf(os.Stderr, "pid %d listening on %s\n", os.Getpid(), ln.Addr())
	http.Serve(ln, q)
}

func iniValues(conf []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(conf))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[k] = v
		}
	}
	return out
}

// fakeBinary is the program an Instance starts as its qBittorrent.
func fakeBinary(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv(fakeEnv, mode)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

func portOf(t *testing.T, s Status) string {
	t.Helper()
	if s.Port == 0 {
		t.Fatalf("status %+v has no port", s)
	}
	return strconv.Itoa(s.Port)
}
