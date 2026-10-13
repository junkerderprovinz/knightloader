package testenv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"

	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// RunWithoutProjectRelay runs the tests with every dial to the project relay
// refused and fails the run if any test tried one. Call it from TestMain in
// place of m.Run.
//
// An instance in a group dials relay.DefaultRelayURL unless its relay mode
// says otherwise, and a test app starts in that mode. A test that enters a
// group sets the mode to off, or to its own relay on an httptest server.
func RunWithoutProjectRelay(m *testing.M) int {
	var tries atomic.Int64
	relay.Dial = func(ctx context.Context, url string, opts *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
		if url == relay.DefaultRelayURL {
			tries.Add(1)
			return nil, nil, errors.New("a test may not dial the project relay")
		}
		return websocket.Dial(ctx, url, opts)
	}
	code := m.Run()
	if n := tries.Load(); n > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests dialled the project relay %s %d times\n", relay.DefaultRelayURL, n)
		return 1
	}
	return code
}
