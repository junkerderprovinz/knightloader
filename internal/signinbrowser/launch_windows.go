package signinbrowser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// launch starts Chromium with DevTools on a loopback port it picks itself.
// Go cannot hand a Windows child the pipe Chromium wants, so this serves
// development on Windows, and the container speaks over the pipe. The port's
// browser endpoint carries a random id that only the profile's
// DevToolsActivePort file names.
func launch(ctx context.Context, bin, profile string, args []string) (transport, func(), error) {
	portFile := filepath.Join(profile, "DevToolsActivePort")
	_ = os.Remove(portFile)
	cmd := exec.CommandContext(ctx, bin, append(args, "--remote-debugging-port=0", "about:blank")...)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	stop := reaper(cmd)
	deadline := time.Now().Add(20 * time.Second)
	for {
		b, err := os.ReadFile(portFile)
		if lines := strings.Fields(string(b)); err == nil && len(lines) == 2 {
			c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+lines[0]+lines[1], nil)
			if err != nil {
				stop()
				return nil, nil, err
			}
			// Screencast frames and cookie lists outgrow the default limit.
			c.SetReadLimit(64 << 20)
			return &socket{c: c}, stop, nil
		}
		if time.Now().After(deadline) {
			stop()
			return nil, nil, errors.New("signinbrowser: Chromium did not open its DevTools port")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type socket struct{ c *websocket.Conn }

func (s *socket) read() ([]byte, error) {
	_, b, err := s.c.Read(context.Background())
	return b, err
}

func (s *socket) write(b []byte) error {
	return s.c.Write(context.Background(), websocket.MessageText, b)
}

func (s *socket) close() error { return s.c.CloseNow() }
