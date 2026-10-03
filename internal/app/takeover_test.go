package app

// A file taken over from the browser stays the file the browser was
// downloading: written under the browser's name, kept off yt-dlp on every
// later start, and never at the cost of another download's file.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// takeOver stages link as a file the browser was downloading under name.
func takeOver(t *testing.T, a *App, link, name string) core.Task {
	t.Helper()
	created, err := a.AddLinksWithOptions([]string{link}, "", OriginCnL, LinkBatchOptions{
		File: true, FileName: name, KeepCollected: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("staged %d rows, want the one file", len(created))
	}
	return *created[0]
}

// takeoverApp is an app on which yt-dlp claims every link, as it does on a
// real install, so a takeover routed by the ranking alone would land there.
func takeoverApp(t *testing.T, mutate func(s *settings.Settings)) *App {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Extract, s.VerifyChecksums = false, false
		mutate(s)
	})
	fake, _ := newFakeYtdlp()
	wireYtdlp(a, fake)
	return a
}

func routedTo(a *App, id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if res := a.resolverForTaskLocked(a.tasks[id]); res != nil {
		return res.Info().ID
	}
	return ""
}

func TestARestartedTakeoverStaysOffYtdlp(t *testing.T) {
	a := takeoverApp(t, func(*settings.Settings) {})
	staged := takeOver(t, a, "https://files.example/nocd", "report.pdf")
	editTask(a, staged.ID, func(x *core.Task) { x.Status = core.StatusDone })
	// Held, so the restart routes the task afresh and starts nothing.
	a.SetHalted(true)

	a.RestartTasks([]string{staged.ID})

	if got := routedTo(a, staged.ID); got != "http" {
		t.Errorf("the restarted takeover would start on %q, want the plain download", got)
	}
}

func TestARecheckKeepsATakeoverOffYtdlp(t *testing.T) {
	a := takeoverApp(t, func(*settings.Settings) {})
	staged := takeOver(t, a, "https://files.example/nocd", "report.pdf")

	a.RecheckTasks([]string{staged.ID})

	if got := liveTask(a, staged.ID).Resolver; got != "http" {
		t.Errorf("the recheck moved the takeover to %q", got)
	}
}

func TestATakeoverHeldByAFilterRuleKeepsItsNameAndStaysOffYtdlpOnceRestored(t *testing.T) {
	for _, field := range []rules.Field{rules.FieldURL, rules.FieldHoster} {
		t.Run(string(field), func(t *testing.T) {
			a := takeoverApp(t, func(s *settings.Settings) {
				s.LinkFilter = rules.Set{Rules: []rules.Rule{{
					Name:       "not from there",
					Conditions: []rules.Condition{{Field: field, Op: rules.OpContains, Value: "files.example"}},
					Action:     rules.Action{Reject: true},
				}}}
			})
			held := takeOver(t, a, "https://files.example/nocd", "Quarterly Report.pdf")
			if !held.Skipped {
				t.Fatal("the rule did not hold the link")
			}
			if held.Name != "Quarterly Report.pdf" {
				t.Errorf("the held row reads %q, want the browser's name", held.Name)
			}

			a.RestoreFiltered([]string{held.ID})
			waitFor(t, "the restored link to be routed", func() bool { return liveTask(a, held.ID).Resolver != "" })

			if live := liveTask(a, held.ID); live.Resolver != "http" || live.Name != "Quarterly Report.pdf" {
				t.Errorf("restored as %q on %q, want the browser's name on the plain download", live.Name, live.Resolver)
			}
			if got := routedTo(a, held.ID); got != "http" {
				t.Errorf("the restored takeover would start on %q", got)
			}
		})
	}
}

// fileServer serves body under any path with no name of its own, as a
// download behind a script does.
func fileServer(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// downloadTakeover takes link over under name, starts it and waits for it to
// settle.
func downloadTakeover(t *testing.T, a *App, link, name string) core.Task {
	t.Helper()
	staged := takeOver(t, a, link, name)
	a.StartTasks([]string{staged.ID})
	waitFor(t, "the download to settle", func() bool {
		s := liveTask(a, staged.ID).Status
		return s == core.StatusDone || s == core.StatusError
	})
	return liveTask(a, staged.ID)
}

func TestAFileTakenOverFromTheBrowserLandsUnderTheBrowsersName(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	a := takeoverApp(t, func(*settings.Settings) {})
	dir := a.Settings.Get().DownloadDir
	body := []byte("the quarterly report")

	got := downloadTakeover(t, a, fileServer(t, body)+"/nocd", "Quarterly Report.pdf")

	if got.Status != core.StatusDone || got.Error != "" {
		t.Fatalf("the download ended %s: %s", got.Status, got.Error)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "Quarterly Report.pdf")); err != nil || !bytes.Equal(data, body) {
		t.Errorf("Quarterly Report.pdf = %q, %v; want the download under the browser's name", data, err)
	}
	if got.Name != "Quarterly Report.pdf" {
		t.Errorf("the row reads %q", got.Name)
	}
}

func TestATakeoverWhoseNameIsTakenFollowsTheCollisionPolicy(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	body := []byte("the quarterly report")
	for _, tc := range []struct {
		policy collide.Policy
		status core.Status
		landed string
	}{
		{collide.Rename, core.StatusDone, "Quarterly Report (2).pdf"},
		{collide.Skip, core.StatusError, ""},
		{collide.Overwrite, core.StatusDone, "Quarterly Report.pdf"},
	} {
		t.Run(string(tc.policy), func(t *testing.T) {
			a := takeoverApp(t, func(s *settings.Settings) { s.CollisionPolicy = string(tc.policy) })
			dir := a.Settings.Get().DownloadDir
			there := filepath.Join(dir, "Quarterly Report.pdf")
			if err := os.WriteFile(there, []byte("last year's report"), 0o644); err != nil {
				t.Fatal(err)
			}

			got := downloadTakeover(t, a, fileServer(t, body)+"/nocd", "Quarterly Report.pdf")

			if got.Status != tc.status {
				t.Fatalf("the download ended %s (%s), want %s", got.Status, got.Error, tc.status)
			}
			if tc.landed == "" {
				if data, _ := os.ReadFile(there); string(data) != "last year's report" {
					t.Errorf("the file already there reads %q", data)
				}
				return
			}
			if got.Error != "" {
				t.Errorf("the finished row carries %q", got.Error)
			}
			if got.Name != tc.landed {
				t.Errorf("the row reads %q, want %q", got.Name, tc.landed)
			}
			if data, err := os.ReadFile(filepath.Join(dir, tc.landed)); err != nil || !bytes.Equal(data, body) {
				t.Errorf("%s = %q, %v; want the download", tc.landed, data, err)
			}
			if fileExists(filepath.Join(dir, "nocd")) {
				t.Error("the download was also left under the server's name")
			}
		})
	}
}

func TestRestartingOrRemovingATakeoverSparesAnotherTakeoverOfTheSameName(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	for _, drop := range []string{"restart", "remove"} {
		t.Run(drop, func(t *testing.T) {
			a := takeoverApp(t, func(*settings.Settings) {})
			srv := fileServer(t, []byte("one of two reports"))
			first := downloadTakeover(t, a, srv+"/first/nocd", "report.pdf")
			second := downloadTakeover(t, a, srv+"/second/nocd", "report.pdf")
			if first.File == "" || second.File == "" || first.File == second.File {
				t.Fatalf("the two downloads landed at %q and %q", first.File, second.File)
			}

			if drop == "restart" {
				a.SetHalted(true)
				a.RestartTasks([]string{first.ID})
			} else {
				a.Remove(first.ID, true)
			}

			if !fileExists(second.File) {
				t.Errorf("%s of the first download deleted the second one's %s", drop, second.File)
			}
		})
	}
}
