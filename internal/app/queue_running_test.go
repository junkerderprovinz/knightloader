package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// lastQueueRunning is the in-flight count of the newest queue state fc was
// sent, or -1 when it was sent none.
func lastQueueRunning(fc *activityFakeConn) int {
	running := -1
	for _, raw := range fc.snapshot() {
		var env struct {
			Type string     `json:"type"`
			Data QueueState `json:"data"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Type == "queue" {
			running = env.Data.Running
		}
	}
	return running
}

// The shell's Stop button is enabled by the queue state's in-flight count, so
// every surface has to hear when a transfer starts or ends, not only when the
// switch is pressed.
func TestTheQueueStateFollowsTransfersStartingAndStopping(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })

	a := newQueueApp(t)
	fc := &activityFakeConn{}
	a.Hub.Add(fc)

	created := a.AddLinks([]string{slow.URL + "/big.bin"}, "Batch")
	if len(created) != 1 {
		t.Fatalf("staged %d links, want 1", len(created))
	}
	a.StartTasks(nil)
	waitFor(t, "a queue state with the started transfer in flight", func() bool {
		return lastQueueRunning(fc) == 1
	})

	a.Pause(created[0].ID)
	waitFor(t, "a queue state with nothing in flight", func() bool {
		return lastQueueRunning(fc) == 0
	})
}
