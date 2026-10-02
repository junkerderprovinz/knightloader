package nntp_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/nntp"
	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

func serverOf(s *nntptest.Server, level int) nntp.Server {
	return nntp.Server{ID: s.Addr(), Host: s.Host, Port: s.Port, Connections: 2, Level: level, Username: s.User, Password: s.Pass}
}

func part(n int) yenc.Part {
	data := bytes.Repeat([]byte{byte(n), 0, '=', '\n', '.'}, 200)
	return yenc.Part{Name: "f.bin", FileSize: int64(len(data) * 10), Number: n, Total: 10, Begin: int64((n - 1) * len(data)), Data: data}
}

func fetch(t *testing.T, c *nntp.Client, id string) (yenc.Part, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Fetch(ctx, id, time.Time{})
}

func TestFetchDecodesWithLogin(t *testing.T) {
	s := nntptest.New(t)
	s.User, s.Pass = "reader", "secret"
	s.AddPart("a@test", part(3))
	c := nntp.NewClient([]nntp.Server{serverOf(s, 0)}, nil)
	defer c.Close()
	got, err := fetch(t, c, "<a@test>")
	if err != nil {
		t.Fatal(err)
	}
	if want := part(3); !bytes.Equal(got.Data, want.Data) || got.Begin != want.Begin {
		t.Fatalf("got part at %d, want %d", got.Begin, want.Begin)
	}
	if _, err := fetch(t, c, "a@test"); err != nil {
		t.Fatal(err)
	}
	if s.Logins() != 1 {
		t.Fatalf("the second article reuses the logged-in connection, got %d logins", s.Logins())
	}
}

func TestFetchOverTLS(t *testing.T) {
	s := nntptest.NewTLS(t)
	s.AddPart("tls@test", part(1))
	cfg := serverOf(s, 0)
	cfg.TLS, cfg.TLSConfig = true, s.ClientTLS
	c := nntp.NewClient([]nntp.Server{cfg}, nil)
	defer c.Close()
	if _, err := fetch(t, c, "tls@test"); err != nil {
		t.Fatal(err)
	}
	// Without the test certificate the client must not talk to it.
	cfg.TLSConfig = nil
	if err := nntp.Check(context.Background(), cfg); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
}

func TestMissingArticleGoesToTheSameLevelThenTheNext(t *testing.T) {
	main1, main2, fill := nntptest.New(t), nntptest.New(t), nntptest.New(t)
	fill.AddPart("only-fill@test", part(2))
	main2.AddPart("only-main2@test", part(4))
	c := nntp.NewClient([]nntp.Server{serverOf(fill, 1), serverOf(main1, 0), serverOf(main2, 0)}, nil)
	defer c.Close()

	if _, err := fetch(t, c, "only-main2@test"); err != nil {
		t.Fatal(err)
	}
	if fill.Bodies("only-main2@test") != 0 {
		t.Fatal("the fill server was asked although a main server had the article")
	}
	if _, err := fetch(t, c, "only-fill@test"); err != nil {
		t.Fatal(err)
	}
	if main1.Bodies("only-fill@test") != 1 || main2.Bodies("only-fill@test") != 1 {
		t.Fatal("both main servers are asked before the fill server")
	}
	if _, err := fetch(t, c, "nowhere@test"); !errors.Is(err, nntp.ErrMissing) {
		t.Fatalf("an article no server has is ErrMissing, got %v", err)
	}
}

func TestDamagedArticleIsAskedForAgain(t *testing.T) {
	s := nntptest.New(t)
	s.AddPart("crc@test", part(5))
	s.Damage("crc@test", 2)
	c := nntp.NewClient([]nntp.Server{serverOf(s, 0)}, nil)
	defer c.Close()
	if _, err := fetch(t, c, "crc@test"); err != nil {
		t.Fatalf("the third copy is whole, got %v", err)
	}
	if n := s.Bodies("crc@test"); n != 3 {
		t.Fatalf("asked %d times, want 3", n)
	}

	s.Damage("crc@test", 100)
	if _, err := fetch(t, c, "crc@test"); !errors.Is(err, nntp.ErrDamaged) {
		t.Fatalf("an article that stays damaged is ErrDamaged, got %v", err)
	}
}

func TestDamagedArticleFallsBackToAnotherServer(t *testing.T) {
	bad, good := nntptest.New(t), nntptest.New(t)
	bad.AddPart("x@test", part(1))
	bad.Damage("x@test", 100)
	good.AddPart("x@test", part(1))
	c := nntp.NewClient([]nntp.Server{serverOf(bad, 0), serverOf(good, 1)}, nil)
	defer c.Close()
	if _, err := fetch(t, c, "x@test"); err != nil {
		t.Fatal(err)
	}
}

func TestUnreachableServer(t *testing.T) {
	down, up := nntptest.New(t), nntptest.New(t)
	down.Close()
	required := serverOf(down, 0)
	c := nntp.NewClient([]nntp.Server{required, serverOf(up, 1)}, nil)
	defer c.Close()
	if _, err := fetch(t, c, "gone@test"); !errors.Is(err, nntp.ErrUnavailable) {
		t.Fatalf("a required server that cannot be reached holds the article back, got %v", err)
	}

	optional := required
	optional.Optional = true
	c = nntp.NewClient([]nntp.Server{optional, serverOf(up, 1)}, nil)
	defer c.Close()
	if _, err := fetch(t, c, "gone@test"); !errors.Is(err, nntp.ErrMissing) {
		t.Fatalf("an optional server is passed over, got %v", err)
	}
}

func TestDroppedConnectionIsReplaced(t *testing.T) {
	s := nntptest.New(t)
	s.AddPart("a@test", part(1))
	s.AddPart("b@test", part(2))
	c := nntp.NewClient([]nntp.Server{serverOf(s, 0)}, nil)
	defer c.Close()
	if _, err := fetch(t, c, "a@test"); err != nil {
		t.Fatal(err)
	}
	// The idle connection is hung up on, as a provider does after a while.
	s.DropNext(1)
	if _, err := fetch(t, c, "b@test"); err != nil {
		t.Fatalf("a dropped idle connection is replaced by a new one, got %v", err)
	}
}

func TestWrongLogin(t *testing.T) {
	s := nntptest.New(t)
	s.User, s.Pass = "reader", "secret"
	cfg := serverOf(s, 0)
	cfg.Password = "wrong"
	if err := nntp.Check(context.Background(), cfg); !errors.Is(err, nntp.ErrAuth) {
		t.Fatalf("got %v, want ErrAuth", err)
	}
	cfg.Username = ""
	if err := nntp.Check(context.Background(), cfg); !errors.Is(err, nntp.ErrAuth) {
		t.Fatalf("a server that wants a login and gets none fails the check, got %v", err)
	}
	cfg = serverOf(s, 0)
	if err := nntp.Check(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionLimitHolds(t *testing.T) {
	s := nntptest.New(t)
	for i := range 20 {
		s.AddPart(fmt.Sprintf("%d@test", i), part(1+i%10))
	}
	cfg := serverOf(s, 0)
	cfg.Connections = 3
	c := nntp.NewClient([]nntp.Server{cfg}, nil)
	defer c.Close()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if _, err := fetch(t, c, fmt.Sprintf("%d@test", i)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if s.MaxOpen() > 3 {
		t.Fatalf("%d connections were open at once, the limit is 3", s.MaxOpen())
	}
}

func TestRetentionSkipsOldArticles(t *testing.T) {
	short, long := nntptest.New(t), nntptest.New(t)
	short.AddPart("old@test", part(1))
	long.AddPart("old@test", part(1))
	a := serverOf(short, 0)
	a.RetentionDays = 30
	c := nntp.NewClient([]nntp.Server{a, serverOf(long, 1)}, nil)
	defer c.Close()
	if _, err := c.Fetch(context.Background(), "old@test", time.Now().AddDate(0, 0, -100)); err != nil {
		t.Fatal(err)
	}
	if short.Bodies("old@test") != 0 {
		t.Fatal("a server was asked for an article older than its retention")
	}
}

func TestStat(t *testing.T) {
	s := nntptest.New(t)
	s.AddPart("here@test", part(1))
	c := nntp.NewClient([]nntp.Server{serverOf(s, 0)}, nil)
	defer c.Close()
	if err := c.Stat(context.Background(), "here@test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Stat(context.Background(), "gone@test"); !errors.Is(err, nntp.ErrMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestRefusesIDsThatWouldInjectCommands(t *testing.T) {
	s := nntptest.New(t)
	c := nntp.NewClient([]nntp.Server{serverOf(s, 0)}, nil)
	defer c.Close()
	for _, id := range []string{"a@b>\r\nQUIT", "a b@c", ""} {
		if _, err := fetch(t, c, id); !errors.Is(err, nntp.ErrMissing) {
			t.Errorf("%q: got %v", id, err)
		}
	}
}

func TestFetchStopsWithTheContext(t *testing.T) {
	s := nntptest.New(t)
	cfg := serverOf(s, 0)
	cfg.Connections = 1
	c := nntp.NewClient([]nntp.Server{cfg}, nil)
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Fetch(ctx, "x@test", time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
