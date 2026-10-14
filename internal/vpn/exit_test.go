package vpn

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"
)

// TestExitServer runs a test server with exit forwarding until
// KL_VPN_EXIT_FOR runs out, for trying a real tunnel against a machine of
// one's own. KL_VPN_EXIT_KEY is its private key, KL_VPN_EXIT_PEER the
// client's public key and KL_VPN_EXIT_PORT its UDP port.
func TestExitServer(t *testing.T) {
	keyText := os.Getenv("KL_VPN_EXIT_KEY")
	if keyText == "" {
		t.Skip("KL_VPN_EXIT_KEY is not set")
	}
	key, err := ParseKey(keyText)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ParseKey(os.Getenv("KL_VPN_EXIT_PEER"))
	if err != nil {
		t.Fatal(err)
	}
	var port uint16 = 51820
	if p := os.Getenv("KL_VPN_EXIT_PORT"); p != "" {
		if _, err := fmt.Sscan(p, &port); err != nil {
			t.Fatal(err)
		}
	}
	runFor, err := time.ParseDuration(os.Getenv("KL_VPN_EXIT_FOR"))
	if err != nil {
		runFor = 15 * time.Minute
	}
	srv, err := startServer(serverOptions{key: key, peer: peer, listenPort: port, exit: true, logf: log.Printf})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()
	log.Printf("exit server on UDP %d at %s for %s, client key %s", srv.port, serverAddr, runFor, peer)
	time.Sleep(runFor)
}
