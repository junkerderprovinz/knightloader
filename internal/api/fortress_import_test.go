package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/lan"
	"github.com/junkerderprovinz/parleyport/kit/member"
)

// backupKeeperIn opens a backup app of the fortress secret makes on the relay
// at relayURL. It meets the members on the network it returns, which is never
// started: the test files their announces itself.
func backupKeeperIn(t *testing.T, relayURL string, secret []byte) (*kitfortress.Fortress, *lan.Service) {
	t.Helper()
	seed := sha256.Sum256([]byte("bombvault"))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	network := lan.New(lan.Config{Group: netip.MustParseAddrPort("239.255.77.250:1")})
	t.Cleanup(func() { _ = network.Close() })
	f, err := kitfortress.Open(kitfortress.Config{
		App:      kitfortress.App{ID: "bombvault", Name: "BombVault", Version: "1.0.0"},
		Secret:   secret,
		Identity: id,
		Self:     group.Self{Name: "Tower", Deployment: "container"},
		Relay:    group.RelayOwn,
		RelayURL: relayURL,
		LAN:      network,
		Store:    &memoryStore{},
		Uses:     []string{kitfortress.ImportAction},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, network
}

// announceInFortress is the announce of member id at url, signed with the
// fortress's keys.
func announceInFortress(secret []byte, id, url string) lan.Peer {
	p := lan.Peer{ID: id, URL: url, Name: "Attic", Deployment: "container", Sent: time.Now().Unix()}
	mac := hmac.New(sha256.New, kitfortress.Domain.PeerAuthKey(secret))
	mac.Write([]byte("announce\x00" + p.ID + "\x00" + p.URL + "\x00" + p.Name + "\x00" + p.Deployment + "\x00" + p.Address + "\x00" + strconv.FormatInt(p.Sent, 10)))
	p.Tag = hex.EncodeToString(mac.Sum(nil))
	return p
}

func TestABackupAppHandsTheBackupBackThroughTheGroupCallRoute(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	relayURL := ownRelay(t, a, srv.URL)
	secret, words := newWords(t)
	keeper, network := backupKeeperIn(t, relayURL, secret)

	if code, raw := postJSON(t, http.MethodPost, srv.URL+"/api/fortress/join", map[string]string{"phrase": words}); code != http.StatusOK {
		t.Fatalf("join answered %d: %s", code, raw)
	}
	if code, raw := postJSON(t, http.MethodPut, srv.URL+"/api/fortress/permissions", fortressPermissionView{
		From: "bombvault", To: appID, Action: kitfortress.ImportAction, On: true,
	}); code != http.StatusOK {
		t.Fatalf("the permission answered %d: %s", code, raw)
	}
	loader := memberKey(a).ID()
	waitFor(t, "the backup app to know KnightLoader on this network", func() bool {
		network.Observe(announceInFortress(secret, loader, srv.URL))
		for _, m := range keeper.Members() {
			if m.ID == loader && m.App.ID == appID && m.Direct {
				return true
			}
		}
		return false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	export, n, err := a.FortressExport(ctx, kitfortress.Caller{App: "bombvault", Device: "Tower"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(export)
	_ = export.Close()
	if err != nil || int64(len(raw)) != n {
		t.Fatalf("the export read %d of %d bytes: %v", len(raw), n, err)
	}
	if err := keeper.Import(ctx, loader, bytes.NewReader(raw), n); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.DataDir, "restore-pending")); err != nil {
		t.Errorf("the backup handed back was not staged: %v", err)
	}
}
