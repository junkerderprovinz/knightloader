package vpnconn

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// fwdURL is where cryptostorm's servers take a port to forward, inside the
// tunnel.
var fwdURL = "http://10.31.33.7/fwd"

// cryptostorm forwards ports from 30000 up.
const fwdLow, fwdHigh = 30000, 65535

// fwdPortFor is the torrent settings' port where cryptostorm can forward it,
// and a free pick of its range otherwise.
func fwdPortFor(manual int) uint16 {
	if manual >= fwdLow && manual <= fwdHigh {
		return uint16(manual)
	}
	return uint16(fwdLow + rand.IntN(fwdHigh-fwdLow+1))
}

// askForPort has cryptostorm's server forward a port to this tunnel. The
// server keeps it for as long as the connection's token is valid, so it is
// asked for once per connection, until the server answers.
func (m *Manager) askForPort(ctx context.Context, p Provider) {
	port := fwdPortFor(m.opts.ManualPort())
	client := &http.Client{Transport: &http.Transport{DialContext: m.tun.DialContext}}
	ask := func() error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fwdURL,
			strings.NewReader(url.Values{"port": {strconv.Itoa(int(port))}}.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("answered HTTP %d", resp.StatusCode)
		}
		return nil
	}
	if m.retryPort(ctx, p, ask) {
		m.setPort(port)
	}
}
