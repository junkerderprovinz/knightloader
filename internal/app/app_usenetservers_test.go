package app

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

func TestASavedServerChangeReachesADownloadUnderWay(t *testing.T) {
	wrong, right := nntptest.New(t), nntptest.New(t)
	right.AddPart("a.1@kl.test", yenc.Part{Name: "a.bin", FileSize: 3, Number: 1, Total: 1, Data: []byte("abc")})
	a := newCrawlApp(t, false)
	save := func(s *nntptest.Server, conns int) {
		t.Helper()
		cfg := a.Settings.Get()
		cfg.UsenetServers = []settings.UsenetServer{{ID: "main", Host: s.Host, Port: s.Port, Connections: conns, Enabled: true}}
		if _, err := a.ApplySettings(cfg); err != nil {
			t.Fatal(err)
		}
	}
	save(wrong, 4)
	// What a file that started before the change goes on fetching with.
	client := a.nntpClient()

	save(right, 1)
	if _, err := client.Fetch(context.Background(), "a.1@kl.test", time.Time{}); err != nil {
		t.Errorf("the next article still goes to the old address: %v", err)
	}
	if n := client.Connections(); n != 1 {
		t.Errorf("the download may hold %d connections, want the 1 saved", n)
	}
}
