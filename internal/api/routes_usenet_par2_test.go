package api

// A release from the own Usenet server checked against its par2 set, which
// par2cmdline made (internal/par2/testdata/make.sh).

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/store"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

func postingFrom(name string, data []byte) posting {
	p := posting{name: name, data: data}
	const partSize = 1000
	total := (len(data) + partSize - 1) / partSize
	for n := range total {
		from, to := n*partSize, min((n+1)*partSize, len(data))
		p.parts = append(p.parts, yenc.Part{Name: name, FileSize: int64(len(data)), Number: n + 1, Total: total, Begin: int64(from), Data: data[from:to]})
	}
	return p
}

// showRelease is the par2cmdline set of two rar volumes, each file posted
// under its own name unless names says another.
type showRelease struct {
	index, part1, part2 posting
	volumes             []posting
}

func newShowRelease(t *testing.T, names map[string]string) showRelease {
	t.Helper()
	read := func(file string) posting {
		b, err := os.ReadFile(filepath.Join("..", "par2", "testdata", "posted", file))
		if err != nil {
			t.Fatal(err)
		}
		as := file
		if n := names[file]; n != "" {
			as = n
		}
		return postingFrom(as, b)
	}
	r := showRelease{index: read("show.par2"), part1: read("show.part1.rar"), part2: read("show.part2.rar")}
	for _, v := range []string{"show.vol00+1.par2", "show.vol01+2.par2", "show.vol03+4.par2", "show.vol07+8.par2", "show.vol15+9.par2"} {
		r.volumes = append(r.volumes, read(v))
	}
	return r
}

// nzb lists the index first, so with one download at a time it is here
// before the rest.
func (r showRelease) nzb() []byte {
	return nzbFor(append([]posting{r.index, r.part1, r.part2}, r.volumes...)...)
}

func (r showRelease) postVolumes(s *nntptest.Server) {
	for _, v := range r.volumes {
		v.post(s)
	}
}

func fetched(s *nntptest.Server, p posting) bool {
	for i := range p.parts {
		if s.Bodies(p.id(i+1)) > 0 {
			return true
		}
	}
	return false
}

func sameFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s holds %d bytes (%v), want the %d posted", filepath.Base(path), len(got), err, len(want))
	}
}

func TestADamagedReleaseIsRepairedFromJustTheVolumesItNeeds(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	r := newShowRelease(t, nil)
	r.index.post(s)
	// Two articles of one slice in the first part, one in the second.
	r.part1.post(s, 51, 52)
	r.part2.post(s, 10)
	r.postVolumes(s)
	_, srv, key := ownServerClient(t, s, nil)

	sabAddFile(t, srv, key, "Show.nzb", "tv-sonarr", r.nzb())
	row := historyRow(t, srv, key)
	if row["status"] != "Completed" {
		t.Fatalf("history slot = %+v, want it completed", row)
	}
	dir := row["storage"].(string)
	sameFile(t, filepath.Join(dir, "show.part1.rar"), r.part1.data)
	sameFile(t, filepath.Join(dir, "show.part2.rar"), r.part2.data)
	// Two blocks to rebuild: the volume with two, and none of the others.
	for i, v := range r.volumes {
		if got := fetched(s, v); got != (i == 1) {
			t.Errorf("%s fetched: %v", v.name, got)
		}
	}
}

func TestAFileUnderARandomNameGetsTheNameItsPar2SetGives(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	r := newShowRelease(t, map[string]string{"show.part2.rar": "f83a01c9e2"})
	r.index.post(s)
	r.part1.post(s)
	r.part2.post(s)
	r.postVolumes(s)
	a, srv, key := ownServerClient(t, s, nil)

	sabAddFile(t, srv, key, "Show.nzb", "tv-sonarr", r.nzb())
	row := historyRow(t, srv, key)
	if row["status"] != "Completed" {
		t.Fatalf("history slot = %+v, want it completed", row)
	}
	dir := row["storage"].(string)
	sameFile(t, filepath.Join(dir, "show.part2.rar"), r.part2.data)
	if _, err := os.Stat(filepath.Join(dir, "f83a01c9e2")); err == nil {
		t.Error("the file is still there under its random name")
	}
	for _, task := range a.Tasks() {
		if task.Name == "f83a01c9e2" {
			t.Error("the row still has the random name")
		}
	}
	for _, v := range r.volumes {
		if fetched(s, v) {
			t.Errorf("%s was fetched for a whole release", v.name)
		}
	}
}

func TestAReleaseBeyondRepairFailsBeforeTheRestIsFetched(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	r := newShowRelease(t, nil)
	r.index.post(s)
	// 300 KB of the first part are on no server: 37 blocks, and the volumes
	// hold 24.
	var gone []int
	for n := 1; n <= 300; n++ {
		gone = append(gone, n)
	}
	r.part1.post(s, gone...)
	r.part2.post(s)
	r.postVolumes(s)
	_, srv, key := ownServerClient(t, s, func(c *settings.Settings) { c.MaxConcurrent = 1 })

	sabAddFile(t, srv, key, "Show.nzb", "tv-sonarr", r.nzb())
	row := historyRow(t, srv, key)
	if row["status"] != "Failed" || !strings.Contains(row["fail_message"].(string), "24 recovery blocks") {
		t.Fatalf("history slot = %+v, want Failed naming the recovery blocks", row)
	}
	if fetched(s, r.part2) {
		t.Error("the second part was fetched for a release already known to be lost")
	}
	for _, v := range r.volumes {
		if fetched(s, v) {
			t.Errorf("%s was fetched for a release beyond repair", v.name)
		}
	}
}

func TestAReleaseUnderItsCheckIsNotCompleteForSonarr(t *testing.T) {
	t.Parallel()
	dc := &downloadClient{a: testApp(t)}
	g := sabGrab{ID: "SABnzbd_nzo_x", Name: "Show", Category: "tv", TaskIDs: []string{"t1", "t2"}}
	one := &core.Task{ID: "t1", Status: core.StatusDone, Enabled: true}
	two := &core.Task{ID: "t2", Status: core.StatusDone, Enabled: true, Repair: &core.RepairProgress{Stage: core.RepairWaiting}}
	live := map[string]*core.Task{"t1": one, "t2": two}
	for stage, want := range map[core.RepairStage]string{
		core.RepairWaiting:   "Verifying",
		core.RepairVerifying: "Verifying",
		core.RepairRepairing: "Repairing",
	} {
		two.Repair = &core.RepairProgress{Stage: stage}
		if v, _ := dc.view(g, live, nil); v.finished || v.status != want {
			t.Errorf("%s: the grab is %q (finished %v), want %s", stage, v.status, v.finished, want)
		}
	}
	two.Repair = nil
	if v, _ := dc.view(g, live, nil); !v.finished || v.status != "Completed" {
		t.Errorf("after the check the grab is %q, want it completed", v.status)
	}
}

func TestACheckARestartCutShortRunsAgain(t *testing.T) {
	t.Parallel()
	dataDir, downloads := t.TempDir(), t.TempDir()
	r := newShowRelease(t, nil)
	files := append([]posting{r.index, r.part1, r.part2}, r.volumes...)
	const remote = "0123456789abcdef"

	st, err := store.Open(filepath.Join(dataDir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, p := range files {
		task := &core.Task{
			ID: fmt.Sprintf("t%d", i), URL: local.FileLink(remote, i, p.name), Name: p.name, Resolver: local.ResolverID,
			Status: core.StatusDone, Enabled: true, Dir: downloads, CreatedAt: time.Now(), Size: int64(len(p.data)),
		}
		switch {
		case p.name == "show.vol01+2.par2", i < 3:
			task.File = filepath.Join(downloads, p.name)
			data := bytes.Clone(p.data)
			if p.name == r.part1.name {
				// Two slices' worth the restart left unrepaired.
				clear(data[50_000:58_000])
			}
			if err := os.WriteFile(task.File, data, 0o600); err != nil {
				t.Fatal(err)
			}
		default:
			task.Status, task.Enabled = core.StatusQueued, false
		}
		if err := st.Save(task); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(dataDir, "usenet", "own")
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, remote+".nzb"), nzbFor(files...), 0o600); err != nil {
		t.Fatal(err)
	}
	jobs, _ := json.Marshal([]usenet.Job{{
		ID: "job1", Name: "Show", State: usenet.StateStaged, Service: local.ResolverID, Remote: remote,
		TaskIDs: ids, Check: usenet.CheckPending, Added: time.Now(),
	}})
	if err := os.WriteFile(filepath.Join(dataDir, "usenet", "jobs.json"), jobs, 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := app.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	waitUntil(t, "the check to pass", func() bool {
		j, ok := a.UsenetJob("job1")
		return ok && j.Check == usenet.CheckPassed
	})
	sameFile(t, filepath.Join(downloads, r.part1.name), r.part1.data)
	for _, task := range a.Tasks() {
		if task.Repair != nil {
			t.Errorf("%s still shows %+v", task.Name, task.Repair)
		}
	}
}

func TestADamagedArchiveIsUnpackedOnlyOnceRepaired(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	read := func(name string) posting {
		b, err := os.ReadFile(filepath.Join("..", "par2", "testdata", "zipped", name))
		if err != nil {
			t.Fatal(err)
		}
		return postingFrom(name, b)
	}
	index, archive := read("episode.par2"), read("episode.zip")
	volumes := []posting{read("episode.vol0+1.par2"), read("episode.vol1+2.par2"), read("episode.vol3+3.par2")}
	index.post(s)
	archive.post(s, 3)
	for _, v := range volumes {
		v.post(s)
	}
	a, srv, key := ownServerClient(t, s, func(c *settings.Settings) { c.Extract = true })

	sabAddFile(t, srv, key, "Episode.nzb", "tv-sonarr", nzbFor(append([]posting{index, archive}, volumes...)...))
	row := historyRow(t, srv, key)
	if row["status"] != "Completed" {
		t.Fatalf("history slot = %+v, want it completed", row)
	}
	jobs := a.ExtractJobs()
	if len(jobs) != 1 || jobs[0].Status != app.ExtractDone {
		t.Fatalf("extractions = %+v, want one, after the repair", jobs)
	}
	z, err := zip.NewReader(bytes.NewReader(archive.data), int64(len(archive.data)))
	if err != nil {
		t.Fatal(err)
	}
	inner, _ := z.File[0].Open()
	want, _ := io.ReadAll(inner)
	var found bool
	filepath.WalkDir(row["storage"].(string), func(p string, d fs.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && d.Name() == "episode.mkv" {
			got, _ := os.ReadFile(p)
			found = bytes.Equal(got, want)
		}
		return nil
	})
	if !found {
		t.Fatal("the unpacked episode is not the one in the archive")
	}
}
