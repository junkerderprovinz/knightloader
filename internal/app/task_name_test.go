package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// climbingName is a file name a server, a resolver or a container can hand
// over. A task's file is found by joining its name to a folder.
const climbingName = "../../outside/victim.txt"

type climbingResolver struct{}

func (climbingResolver) Info() resolver.Info { return resolver.Info{ID: "climbing", Prio: 95} }

func (climbingResolver) Match(raw string) bool { return strings.Contains(raw, "climbing.example") }

func (climbingResolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	return resolver.Result{DirectURL: req.URL, Name: climbingName}, nil
}

func fileOf(a *App, id string) (file, dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tasks[id]
	return a.fileOfLocked(t), a.workDirFor(t)
}

func TestABackendCannotNameATasksFileOutsideItsFolder(t *testing.T) {
	a := retryApp(t, func(*settings.Settings) {})
	runningOn(a, "t1", plainHost, simpleResolverID)

	a.onUpdate("t1", core.Update{Status: core.StatusRunning, Name: climbingName})
	if got := liveTask(a, "t1").Name; got != "victim.txt" {
		t.Fatalf("the task is named %q, want victim.txt", got)
	}
	if file, dir := fileOf(a, "t1"); file != filepath.Join(dir, "victim.txt") {
		t.Errorf("the task's file is %s, outside %s", file, dir)
	}

	a.onUpdate("t1", core.Update{Status: core.StatusRunning, Name: ".."})
	if got := liveTask(a, "t1").Name; got != "victim.txt" {
		t.Errorf("a name with nothing left of it renamed the task to %q", got)
	}
}

func TestALinkCannotNameATasksFileOutsideItsFolder(t *testing.T) {
	for name, add := range map[string]func(a *App) []*core.Task{
		"resolver": func(a *App) []*core.Task {
			a.Registry.Register(climbingResolver{})
			return a.AddLinks([]string{"https://climbing.example/file.bin"}, "")
		},
		"container": func(a *App) []*core.Task {
			return a.AddResolvedLinksFrom([]resolver.Result{{DirectURL: "https://plain.example/boxed.bin", Name: climbingName}},
				"Container", OriginContainer)
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := retryApp(t, func(*settings.Settings) {})
			created := add(a)
			if len(created) != 1 {
				t.Fatalf("staged %d tasks, want 1", len(created))
			}
			id := created[0].ID
			if got := liveTask(a, id).Name; got != "victim.txt" {
				t.Fatalf("the task is named %q, want victim.txt", got)
			}
			if file, dir := fileOf(a, id); file != filepath.Join(dir, "victim.txt") {
				t.Errorf("the task's file is %s, outside %s", file, dir)
			}
		})
	}
}

func TestAStoredNameComesBackAsAFileInItsFolder(t *testing.T) {
	// A link stands for the name until the download names the file.
	const link = "https://plain.example/../../../../outside/link.bin"
	f := newBootFixture(t, nil,
		core.Task{ID: "climbing", URL: "https://plain.example/a.bin", Name: climbingName, Status: core.StatusDone, Enabled: true},
		core.Task{ID: "dots", URL: "https://plain.example/b.bin", Name: "..", Status: core.StatusDone, Enabled: true},
		core.Task{ID: "link", URL: link, Name: link, Status: core.StatusCollected, Enabled: true},
	)
	a := f.boot(t)

	if got := taskOf(t, a, "climbing").Name; got != "victim.txt" {
		t.Errorf("the stored name came back as %q, want victim.txt", got)
	}
	if got := taskOf(t, a, "link").Name; got != link {
		t.Errorf("the link a task is named after until it has a name came back as %q", got)
	}
	for _, id := range []string{"climbing", "dots", "link"} {
		if file, dir := fileOf(a, id); filepath.Dir(file) != dir {
			t.Errorf("task %s's file is %s, outside %s", id, file, dir)
		}
	}
	rows, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "climbing" && row.Name != "victim.txt" {
			t.Errorf("the store still holds %q", row.Name)
		}
	}
}

func TestEveryTaskNameJoinsAsOneElement(t *testing.T) {
	for _, name := range []string{
		"movie.mkv", climbingName, `..\..\victim.txt`, "..", ".", "/", "https://host.example/../../x.bin",
	} {
		got := diskName(name)
		if got == "." || !filepath.IsLocal(got) || filepath.Base(got) != got {
			t.Errorf("diskName(%q) = %q, which is not one name inside a folder", name, got)
		}
	}
	if got := diskName("movie.mkv"); got != "movie.mkv" {
		t.Errorf("diskName changed an ordinary name to %q", got)
	}
}
