package handover

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

type memStore struct {
	mu   sync.Mutex
	data []byte
}

func (s *memStore) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.data), nil
}

func (s *memStore) Save(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = bytes.Clone(b)
	return nil
}

func openOnRelay(t *testing.T, relayURL, app, device string, edit func(*fortress.Config)) (*fortress.Fortress, string) {
	t.Helper()
	seed := sha256.Sum256([]byte(device))
	id, err := member.FromSeed(member.KindInstance, seed[:])
	if err != nil {
		t.Fatal(err)
	}
	cfg := fortress.Config{
		App:      fortress.App{ID: app, Name: app, Version: "1.0.0"},
		Secret:   []byte("a phrase both hold"),
		Identity: id,
		Self:     group.Self{Name: device, Deployment: "container"},
		Relay:    group.RelayOwn,
		RelayURL: relayURL,
		Store:    &memStore{},
	}
	edit(&cfg)
	f, err := fortress.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, id.ID()
}

// The fake engine behind a real fortress, so what Run and Engines send is
// what crosses the relay to ArrowLoop.
func TestAHandOverReachesArrowLoopThroughARealFortress(t *testing.T) {
	rel := httptest.NewServer(relay.New())
	t.Cleanup(rel.Close)
	t.Cleanup(rel.CloseClientConnections)

	var mu sync.Mutex
	engine := &fakeEngine{jobs: photosAndInbox()}
	serve := func(action string) fortress.Action {
		return fortress.Action{Name: action, Run: func(_ context.Context, from fortress.Caller, body []byte) ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()
			if from.App != "knightloader" {
				t.Errorf("%s came from %+v", action, from)
			}
			return engine.act(action, body)
		}}
	}
	_, towerID := openOnRelay(t, rel.URL, EngineApp, "Tower", func(c *fortress.Config) {
		c.Actions = []fortress.Action{serve(ListAction), serve(RunAction)}
	})
	loader, _ := openOnRelay(t, rel.URL, "knightloader", "Attic", func(c *fortress.Config) { c.Uses = Uses })

	var engines []Engine
	deadline := time.Now().Add(20 * time.Second)
	for len(engines) == 0 || len(engines[0].Jobs) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("never listed the jobs on Tower: %+v", engines)
		}
		time.Sleep(20 * time.Millisecond)
		engines = Engines(context.Background(), loader)
	}
	if engines[0].ID != towerID || engines[0].Device != "Tower" || len(engines[0].Jobs) != 2 || engines[0].Jobs[1].Mode != "move" {
		t.Errorf("listed %+v", engines)
	}

	status, err := Run(context.Background(), loader, Target{Member: towerID, Job: "photos"})
	if err != nil || status != StatusStarted {
		t.Fatalf("Run = %q, %v", status, err)
	}
	if _, err := Run(context.Background(), loader, Target{Member: towerID, Job: "inbox"}); Code(err) != CodeJobPaused {
		t.Errorf("a paused job: %v (%s)", err, Code(err))
	}
}
