package afterdownload

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
)

// fakeEngine answers jobs.list, jobs.run, jobs.targets and jobs.create the
// way ArrowLoop does. mounted is the host folders its container sees.
type fakeEngine struct {
	jobs         []fakeJob
	running      map[string]bool
	runs         []string
	destinations []Destination
	mounted      []string
	created      []CreateRequest
}

type fakeJob struct {
	Name      string   `json:"name"`
	Direction string   `json:"direction"`
	Mode      string   `json:"mode"`
	Paused    bool     `json:"paused"`
	Running   bool     `json:"running"`
	LastRun   *LastRun `json:"lastRun,omitempty"`
}

func (e *fakeEngine) act(action string, body []byte) ([]byte, error) {
	switch action {
	case ListAction:
		out := []fakeJob{}
		for _, j := range e.jobs {
			j.Running = e.running[j.Name]
			out = append(out, j)
		}
		return json.Marshal(out)
	case RunAction:
		var req struct {
			Job string `json:"job"`
		}
		if json.Unmarshal(body, &req) != nil {
			return nil, &fortress.Refusal{Status: 400, Code: "bad_request"}
		}
		for _, j := range e.jobs {
			if j.Name != req.Job {
				continue
			}
			if j.Paused {
				return nil, &fortress.Refusal{Status: 409, Code: "job_paused"}
			}
			e.runs = append(e.runs, j.Name)
			status := "started"
			if e.running[j.Name] {
				status = "queued"
			}
			if e.running == nil {
				e.running = map[string]bool{}
			}
			e.running[j.Name] = true
			return json.Marshal(map[string]string{"job": j.Name, "status": status})
		}
		return nil, &fortress.Refusal{Status: 404, Code: "no_such_job"}
	case TargetsAction:
		out := append([]Destination{}, e.destinations...)
		return json.Marshal(out)
	case CreateAction:
		var req CreateRequest
		if json.Unmarshal(body, &req) != nil || req.Name == "" || req.Source == "" {
			return nil, &fortress.Refusal{Status: 400, Code: "bad_request"}
		}
		for _, j := range e.jobs {
			if j.Name == req.Name {
				return nil, &fortress.Refusal{Status: 409, Code: "name_taken"}
			}
		}
		if !slices.ContainsFunc(e.destinations, func(d Destination) bool { return d.ID == req.Target }) {
			return nil, &fortress.Refusal{Status: 404, Code: "no_such_target"}
		}
		if !slices.ContainsFunc(e.mounted, func(m string) bool { return req.Source == m || strings.HasPrefix(req.Source, m+"/") }) {
			return nil, &fortress.Refusal{Status: 422, Code: "source_not_mounted"}
		}
		direction := "leftToRight"
		if req.Mode == "sync" {
			direction = "both"
		}
		e.created = append(e.created, req)
		e.jobs = append(e.jobs, fakeJob{Name: req.Name, Mode: req.Mode, Direction: direction})
		return json.Marshal(map[string]string{"job": req.Name})
	}
	return nil, fortress.ErrNoSuchAction
}

// fakeFortress is a fortress with ArrowLoop instances in it, keyed by member
// id, and any other members it lists.
type fakeFortress struct {
	mu          sync.Mutex
	members     []fortress.Member
	engines     map[string]*fakeEngine
	unreachable map[string]bool
	off         map[string]bool
}

func newFakeFortress() *fakeFortress {
	return &fakeFortress{engines: map[string]*fakeEngine{}, unreachable: map[string]bool{}, off: map[string]bool{}}
}

func (f *fakeFortress) addEngine(id, device string, jobs ...fakeJob) *fakeEngine {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := &fakeEngine{jobs: jobs}
	f.engines[id] = e
	f.members = append(f.members, fortress.Member{
		ID: id, App: fortress.App{ID: EngineApp, Name: "ArrowLoop"}, Device: device,
		Direct: true, Actions: []string{ListAction, RunAction, TargetsAction, CreateAction}, LastSeen: time.Now(),
	})
	return e
}

func (f *fakeFortress) setUnreachable(id string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unreachable[id] = on
}

func (f *fakeFortress) runs(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.engines[id].runs...)
}

func (f *fakeFortress) Members() []fortress.Member {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fortress.Member(nil), f.members...)
}

func (f *fakeFortress) Act(ctx context.Context, id, action string, body []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.unreachable[id] {
		return nil, group.ErrUnreachable
	}
	if f.off[action] {
		return nil, fortress.ErrPermissionOff
	}
	e, ok := f.engines[id]
	if !ok {
		return nil, fortress.ErrNoSuchAction
	}
	return e.act(action, body)
}
