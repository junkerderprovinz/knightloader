package script

import (
	"strings"
	"testing"
	"time"
)

func TestStore_OpenMissingFileIsEmpty(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if got := st.list(); len(got) != 0 {
		t.Fatalf("list() on a fresh store = %v, want empty", got)
	}
}

func TestStore_SaveListGetDelete(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	saved, err := st.save(Script{Name: "greet", Trigger: TriggerTaskDone, Code: "notify('hi');", Enabled: true})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("save did not assign an ID")
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatalf("save left timestamps zero: %+v", saved)
	}

	got, ok := st.get(saved.ID)
	if !ok || got.Name != "greet" {
		t.Fatalf("get(%q) = %+v, %v; want the saved script", saved.ID, got, ok)
	}

	list := st.list()
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("list() = %+v, want one entry matching %q", list, saved.ID)
	}

	if err := st.delete(saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := st.get(saved.ID); ok {
		t.Fatal("get still finds a deleted script")
	}
	if got := st.list(); len(got) != 0 {
		t.Fatalf("list() after delete = %v, want empty", got)
	}
}

func TestStore_DeleteUnknownIDErrors(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if err := st.delete("does-not-exist"); err == nil {
		t.Fatal("delete of an unknown id should error, not silently succeed")
	}
}

func TestStore_SavePreservesCreatedAtOnEdit(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	first, err := st.save(Script{Name: "one", Trigger: TriggerQueueIdle, Code: "1;", Enabled: false})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	time.Sleep(2 * time.Millisecond) // ensure UpdatedAt can actually move
	second, err := st.save(Script{ID: first.ID, Name: "one renamed", Trigger: TriggerQueueIdle, Code: "2;", Enabled: true})
	if err != nil {
		t.Fatalf("save (edit): %v", err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("edit changed CreatedAt: first=%v second=%v", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("edit did not move UpdatedAt forward: first=%v second=%v", first.UpdatedAt, second.UpdatedAt)
	}
	if got := st.list(); len(got) != 1 {
		t.Fatalf("editing by ID should replace, not add a row: list() = %+v", got)
	}
}

func TestStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	st1, err := openStore(dir)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	saved, err := st1.save(Script{Name: "survives", Trigger: TriggerTaskFailed, Code: "notify('x');", Enabled: true})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	st2, err := openStore(dir)
	if err != nil {
		t.Fatalf("re-openStore: %v", err)
	}
	got, ok := st2.get(saved.ID)
	if !ok {
		t.Fatal("a freshly opened store did not find the script the previous one saved")
	}
	if got.Name != "survives" || got.Code != "notify('x');" {
		t.Fatalf("reopened script = %+v, want the one that was saved", got)
	}
}

func TestStore_SaveRejects(t *testing.T) {
	cases := []struct {
		name   string
		script Script
		want   string // substring expected in the error
	}{
		{"empty name", Script{Trigger: TriggerTaskDone, Code: "1;"}, "name"},
		{"name too long", Script{Name: strings.Repeat("x", MaxNameBytes+1), Trigger: TriggerTaskDone, Code: "1;"}, "name"},
		{"unknown trigger", Script{Name: "a", Trigger: "task.exploded", Code: "1;"}, "trigger"},
		{"empty code", Script{Name: "a", Trigger: TriggerTaskDone}, "code"},
		{"code too long", Script{Name: "a", Trigger: TriggerTaskDone, Code: strings.Repeat("x", MaxCodeBytes+1)}, "source"},
		{"does not compile", Script{Name: "a", Trigger: TriggerTaskDone, Code: "function( {"}, "compile"},
		{"timeout below minimum", Script{Name: "a", Trigger: TriggerTaskDone, Code: "1;", TimeoutMS: 1}, "timeoutMs"},
		{"timeout above maximum", Script{Name: "a", Trigger: TriggerTaskDone, Code: "1;", TimeoutMS: int(MaxTimeout.Milliseconds()) + 1000}, "timeoutMs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := openStore(t.TempDir())
			if err != nil {
				t.Fatalf("openStore: %v", err)
			}
			_, err = st.save(c.script)
			if err == nil {
				t.Fatalf("save(%+v) succeeded, want an error containing %q", c.script, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("save error = %q, want it to mention %q", err.Error(), c.want)
			}
			if got := st.list(); len(got) != 0 {
				t.Fatalf("a refused save must write nothing: list() = %+v", got)
			}
		})
	}
}

func TestStore_SaveTrimsName(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	saved, err := st.save(Script{Name: "  padded  ", Trigger: TriggerTaskDone, Code: "1;"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Name != "padded" {
		t.Fatalf("Name = %q, want trimmed", saved.Name)
	}
}
