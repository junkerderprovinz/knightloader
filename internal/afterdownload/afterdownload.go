// Package afterdownload hands a finished package to ArrowLoop: once its
// files are in their folder, KnightLoader asks an ArrowLoop instance of the
// fortress to run the job a category names. Only the job's name crosses, never
// a path, since the two containers mount the same share under different names.
package afterdownload

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/group"
)

// The app and the actions on the other side.
const (
	EngineApp  = "arrowloop"
	ListAction = "jobs.list"
	RunAction  = "jobs.run"
	// TargetsAction lists the destinations ArrowLoop already knows, and
	// CreateAction makes a job from this category's folder to one of them.
	TargetsAction = "jobs.targets"
	CreateAction  = "jobs.create"
)

// Uses are the actions KnightLoader calls, for fortress.Config.Uses.
var Uses = []string{ListAction, RunAction, TargetsAction, CreateAction}

// Fortress is what a hand-over needs of *fortress.Fortress.
type Fortress interface {
	Members() []fortress.Member
	Act(ctx context.Context, id, action string, body []byte) ([]byte, error)
}

// Target is one job on one ArrowLoop instance.
type Target struct {
	Member string `json:"member"`
	Job    string `json:"job"`
}

// Job is a job as jobs.list describes it.
type Job struct {
	Name string `json:"name"`
	// Direction is both, leftToRight or rightToLeft.
	Direction string `json:"direction"`
	// Mode is sync, mirror or move.
	Mode    string   `json:"mode"`
	Paused  bool     `json:"paused"`
	Running bool     `json:"running"`
	LastRun *LastRun `json:"lastRun,omitempty"`
}

// LastRun is when a job last ended and how: done, quiet, left or failed.
type LastRun struct {
	At      time.Time `json:"at"`
	Outcome string    `json:"outcome"`
}

// Engine is one ArrowLoop instance with its jobs, or the code of why they
// could not be listed.
type Engine struct {
	ID     string `json:"id"`
	Device string `json:"device"`
	Jobs   []Job  `json:"jobs"`
	Error  string `json:"error,omitempty"`
}

// What jobs.run answers.
const (
	StatusStarted = "started"
	StatusQueued  = "queued"
)

// The codes Code gives an error, for the interface to word.
const (
	CodeNoFortress       = "noFortress"
	CodeGone             = "gone"
	CodeUnreachable      = "unreachable"
	CodePermissionOff    = "permissionOff"
	CodeNoSuchAction     = "noSuchAction"
	CodeNoSuchJob        = "noSuchJob"
	CodeJobPaused        = "jobPaused"
	CodeNameTaken        = "nameTaken"
	CodeNoSuchTarget     = "noSuchTarget"
	CodeSourceNotMounted = "sourceNotMounted"
	CodeBadRequest       = "badRequest"
	CodeFailed           = "failed"
)

var (
	// ErrNoFortress is a hand-over on an instance that has not joined one.
	ErrNoFortress = errors.New("after download: this instance is in no fortress")
	// ErrBadRequest is a new job this side already knows ArrowLoop refuses.
	ErrBadRequest = errors.New("after download: a new job needs a name, a folder, a target and one of the three modes")
	// ErrGone is an instance the fortress no longer lists.
	ErrGone = errors.New("after download: that ArrowLoop instance has left the fortress")
)

// Code names why a call failed, and "" for no error.
func Code(err error) string {
	var refusal *fortress.Refusal
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNoFortress):
		return CodeNoFortress
	case errors.Is(err, ErrGone), errors.Is(err, group.ErrRemoved):
		return CodeGone
	case errors.Is(err, group.ErrUnreachable), errors.Is(err, context.DeadlineExceeded):
		return CodeUnreachable
	case errors.Is(err, fortress.ErrPermissionOff):
		return CodePermissionOff
	case errors.Is(err, fortress.ErrNoSuchAction):
		return CodeNoSuchAction
	case errors.As(err, &refusal) && refusal.Code == "no_such_job":
		return CodeNoSuchJob
	case errors.As(err, &refusal) && refusal.Code == "job_paused":
		return CodeJobPaused
	case errors.As(err, &refusal) && refusal.Code == "name_taken":
		return CodeNameTaken
	case errors.As(err, &refusal) && refusal.Code == "no_such_target":
		return CodeNoSuchTarget
	case errors.As(err, &refusal) && refusal.Code == "source_not_mounted":
		return CodeSourceNotMounted
	case errors.Is(err, ErrBadRequest), errors.As(err, &refusal) && refusal.Code == "bad_request":
		return CodeBadRequest
	}
	return CodeFailed
}

// callTimeout bounds one call, which goes over the relay when the instance is
// not on the same network.
const callTimeout = 20 * time.Second

// Engines lists the ArrowLoop instances of the fortress that take a job to
// run, by device, each with its jobs. A nil fortress has none.
func Engines(ctx context.Context, f Fortress) []Engine {
	out := []Engine{}
	if f == nil {
		return out
	}
	for _, m := range f.Members() {
		if m.App.ID != EngineApp || !slices.Contains(m.Actions, RunAction) {
			continue
		}
		e := Engine{ID: m.ID, Device: m.Device, Jobs: []Job{}}
		if err := list(ctx, f, m.ID, &e.Jobs); err != nil {
			e.Jobs, e.Error = []Job{}, Code(err)
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Engine) int {
		return cmp.Or(cmp.Compare(a.Device, b.Device), cmp.Compare(a.ID, b.ID))
	})
	return out
}

func list(ctx context.Context, f Fortress, id string, into *[]Job) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	raw, err := f.Act(ctx, id, ListAction, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("after download: %s answered %s with %q: %w", id, ListAction, raw, err)
	}
	return nil
}

// Run asks t's instance to run t's job and returns StatusStarted or, while
// the job is already running, StatusQueued: ArrowLoop runs it once more
// afterwards.
func Run(ctx context.Context, f Fortress, t Target) (string, error) {
	if err := present(f, t.Member); err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]string{"job": t.Job})
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	raw, err := f.Act(ctx, t.Member, RunAction, body)
	if err != nil {
		return "", err
	}
	var answer struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return "", fmt.Errorf("after download: %s answered %s with %q: %w", t.Member, RunAction, raw, err)
	}
	return answer.Status, nil
}

// Destination is a place ArrowLoop already knows and can copy to, without the
// login it may need.
type Destination struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is share, disk or remote.
	Kind string `json:"kind"`
	// Where is a short place to tell two alike apart.
	Where string `json:"where"`
}

// Destinations lists what ArrowLoop on member can copy to.
func Destinations(ctx context.Context, f Fortress, member string) ([]Destination, error) {
	if err := present(f, member); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	raw, err := f.Act(ctx, member, TargetsAction, nil)
	if err != nil {
		return nil, err
	}
	out := []Destination{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("after download: %s answered %s with %q: %w", member, TargetsAction, raw, err)
	}
	return out, nil
}

// CreateRequest is a new job: from Source, a folder as the host sees it, to
// the destination Target, in Mode mirror, sync or move.
type CreateRequest struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
}

// Create makes the job in ArrowLoop on member and returns its name.
func Create(ctx context.Context, f Fortress, member string, req CreateRequest) (string, error) {
	if err := present(f, member); err != nil {
		return "", err
	}
	if req.Name == "" || req.Source == "" || req.Target == "" || !slices.Contains([]string{"mirror", "sync", "move"}, req.Mode) {
		return "", ErrBadRequest
	}
	body, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	raw, err := f.Act(ctx, member, CreateAction, body)
	if err != nil {
		return "", err
	}
	var answer struct {
		Job string `json:"job"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return "", fmt.Errorf("after download: %s answered %s with %q: %w", member, CreateAction, raw, err)
	}
	return answer.Job, nil
}

func present(f Fortress, member string) error {
	if f == nil {
		return ErrNoFortress
	}
	if !slices.ContainsFunc(f.Members(), func(m fortress.Member) bool { return m.ID == member }) {
		return ErrGone
	}
	return nil
}
