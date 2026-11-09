package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	kit "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// inbox is a webhook server that keeps every body it was sent.
type inbox struct {
	mu     sync.Mutex
	bodies []string
	got    chan struct{}
	srv    *httptest.Server
}

func newInbox(t *testing.T, status int) *inbox {
	t.Helper()
	in := &inbox{got: make(chan struct{}, 64)}
	in.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		in.mu.Lock()
		in.bodies = append(in.bodies, string(b))
		in.mu.Unlock()
		w.WriteHeader(status)
		in.got <- struct{}{}
	}))
	t.Cleanup(in.srv.Close)
	return in
}

func (in *inbox) wait(t *testing.T) string {
	t.Helper()
	select {
	case <-in.got:
	case <-time.After(10 * time.Second):
		t.Fatal("nothing arrived")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.bodies[len(in.bodies)-1]
}

func webhook(id, url string, on kit.On, events ...string) kit.Channel {
	return kit.Channel{
		ID: id, Kind: kit.KindWebhook, Enabled: true, On: on, Events: events, Attempts: 1,
		Webhook: &kit.Webhook{URL: url, Method: "POST", Headers: map[string]string{"Content-Type": "application/json"},
			Body: `{"name":"%%task.name%%","event":"%%event%%","instance":"%%instance%%"}`},
	}
}

func dispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	d := New(Options{InstanceName: func() string { return "Tower" }})
	t.Cleanup(func() { d.Close() })
	return d
}

func taskDone(name string) script.Firing {
	return script.Firing{Trigger: script.TriggerTaskDone, At: time.Now(), Task: &script.TaskView{Name: name}}
}

func TestATemplateKeepsItsPlaceholders(t *testing.T) {
	in := newInbox(t, http.StatusOK)
	d := dispatcher(t)
	d.Set([]kit.Channel{webhook("1", in.srv.URL, kit.OnAlways, "task.done")})
	d.On(taskDone(`Der "Direktor".mkv`))

	var got map[string]string
	if err := json.Unmarshal([]byte(in.wait(t)), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got["name"] != `Der "Direktor".mkv` || got["event"] != "task.done" || got["instance"] != "Tower" {
		t.Errorf("body = %v", got)
	}
}

func TestOnlyTheEventsAChannelTakesAreSent(t *testing.T) {
	in := newInbox(t, http.StatusOK)
	d := dispatcher(t)
	d.Set([]kit.Channel{
		webhook("picky", in.srv.URL+"/picky", kit.OnAlways, "task.failed"),
		webhook("failures", in.srv.URL+"/failures", kit.OnFailure),
		{ID: "off", Kind: kit.KindWebhook, Enabled: false, On: kit.OnAlways, Webhook: &kit.Webhook{URL: in.srv.URL}},
	})
	d.On(taskDone("a"))
	d.On(script.Firing{Trigger: script.TriggerTaskFailed, At: time.Now(), Task: &script.TaskView{Name: "b"}})
	in.wait(t)
	in.wait(t)
	select {
	case <-in.got:
		t.Error("a third message arrived")
	case <-time.After(300 * time.Millisecond):
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, b := range in.bodies {
		var got map[string]string
		_ = json.Unmarshal([]byte(b), &got)
		if got["name"] != "b" {
			t.Errorf("a channel got %q", got["name"])
		}
	}
}

func TestHealthCountsWhatArrivedAndWhatWasRefused(t *testing.T) {
	ok := newInbox(t, http.StatusOK)
	refused := newInbox(t, http.StatusUnauthorized)
	d := dispatcher(t)
	d.Set([]kit.Channel{webhook("ok", ok.srv.URL, kit.OnAlways), webhook("refused", refused.srv.URL, kit.OnAlways)})
	d.On(taskDone("a"))
	ok.wait(t)
	refused.wait(t)

	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := map[string]Health{}
		for _, h := range d.Health() {
			rows[h.ChannelID] = h
		}
		if rows["ok"].Sent == 1 && rows["refused"].Attempts == 1 {
			if rows["refused"].LastCode != kit.CodeAuth || rows["refused"].LastStatus != http.StatusUnauthorized || rows["refused"].LastOK.IsZero() == false {
				t.Errorf("refused = %+v", rows["refused"])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("health = %+v", rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSetKeepsTheHealthOfAChannelThatStays(t *testing.T) {
	in := newInbox(t, http.StatusOK)
	d := dispatcher(t)
	ch := webhook("1", in.srv.URL, kit.OnAlways)
	d.Set([]kit.Channel{ch})
	d.On(taskDone("a"))
	in.wait(t)
	for len(d.Health()) == 0 || d.Health()[0].Sent == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	ch.Name = "renamed"
	d.Set([]kit.Channel{ch})
	if h := d.Health(); len(h) != 1 || h[0].Sent != 1 {
		t.Errorf("health after a save = %+v", h)
	}
	ch.Enabled = false
	d.Set([]kit.Channel{ch})
	if h := d.Health(); len(h) != 0 {
		t.Errorf("a switched off channel keeps health %+v", h)
	}
}
