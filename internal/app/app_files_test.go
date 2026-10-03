package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// newFilesTestApp returns an App with a real, writable download folder, since
// SafeTaskFile stats and resolves real paths.
func newFilesTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	base := t.TempDir()
	if _, err := a.ApplySettings(settings.Settings{MaxConcurrent: 2, MaxPerHost: 1, DownloadDir: base}); err != nil {
		t.Fatal(err)
	}
	return a, base
}

func writeTestFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSafeTaskFileHappyPath(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("hello world"))
	task := putTask(t, a, core.Task{URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	// The temp dir itself may sit behind a symlink (macOS /tmp).
	wantReal, err := filepath.EvalSymlinks(filepath.Join(base, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
	if got.Name != "movie.mkv" {
		t.Errorf("Name = %q, want movie.mkv", got.Name)
	}
	if got.Size != int64(len("hello world")) {
		t.Errorf("Size = %d, want %d", got.Size, len("hello world"))
	}
}

// A download that had to be saved beside a file of its name is served from
// where it was saved, still under the task's name.
func TestSafeTaskFileServesTheFileTheDownloadWrote(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("somebody else's"))
	writeTestFile(t, base, "movie (1).mkv", []byte("the download"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone,
		File: filepath.Join(base, "movie (1).mkv"),
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(base, "movie (1).mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal || got.Name != "movie.mkv" {
		t.Errorf("served %q as %q, want %q as movie.mkv", got.Path, got.Name, wantReal)
	}
}

func TestSafeTaskFilePerTaskDirWorksWithinTheDownloadRoot(t *testing.T) {
	a, base := newFilesTestApp(t)
	sub := filepath.Join(base, "Movies", "2026")
	writeTestFile(t, sub, "movie.mkv", []byte("x"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Dir: sub,
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(sub, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
}

// TestSafeTaskFileRefusesADirOutsideTheDownloadRoot: t.Dir is client-supplied,
// and joining a single-segment name onto it is always inside it, so only the
// root check stops a Dir pointed at the app's own settings.json.
func TestSafeTaskFileRefusesADirOutsideTheDownloadRoot(t *testing.T) {
	a, _ := newFilesTestApp(t)
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "settings.json", []byte(`{"reconnect":{"password":"not for a browser"}}`))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/settings.json", Name: "settings.json", Status: core.StatusDone, Dir: elsewhere,
	})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape because a Dir outside the download root must never be served", err)
	}
}

// TestSafeTaskFileRespectsKLBrowseRoots: a folder the chooser could offer under
// KL_BROWSE_ROOTS is served even outside the download tree.
func TestSafeTaskFileRespectsKLBrowseRoots(t *testing.T) {
	a, _ := newFilesTestApp(t)
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "movie.mkv", []byte("x"))
	t.Setenv("KL_BROWSE_ROOTS", elsewhere)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Dir: elsewhere,
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v, want success since KL_BROWSE_ROOTS allows this folder", err)
	}
	wantReal, err := filepath.EvalSymlinks(filepath.Join(elsewhere, "movie.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, wantReal)
	}
}

func TestSafeTaskFileKLBrowseRootsStillRefusesOutsideIt(t *testing.T) {
	a, _ := newFilesTestApp(t)
	allowed := t.TempDir()
	elsewhere := t.TempDir()
	writeTestFile(t, elsewhere, "movie.mkv", []byte("x"))
	t.Setenv("KL_BROWSE_ROOTS", allowed)
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Dir: elsewhere,
	})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape since elsewhere is not under KL_BROWSE_ROOTS", err)
	}
}

// TestSafeTaskFileSizeIsWhatIsOnDiskRightNow: Content-Length must be the bytes
// on disk, or a client waits for bytes that never come.
func TestSafeTaskFileSizeIsWhatIsOnDiskRightNow(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "movie.mkv", []byte("only nine"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv",
		Status: core.StatusRunning, Size: 9_000_000_000,
	})

	got, err := a.SafeTaskFile(task.ID)
	if err != nil {
		t.Fatalf("SafeTaskFile: %v", err)
	}
	if want := int64(len("only nine")); got.Size != want {
		t.Errorf("Size = %d, want the %d bytes actually on disk, not the task's expected total", got.Size, want)
	}
}

func TestSafeTaskFileUnknownTask(t *testing.T) {
	a, _ := newFilesTestApp(t)
	if _, err := a.SafeTaskFile("does-not-exist"); !errors.Is(err, ErrTaskFileNotFound) {
		t.Errorf("err = %v, want ErrTaskFileNotFound", err)
	}
}

// TestSafeTaskFileNotYetResolvedIsNoBytesNotAnEscape covers both ways filename()
// returns nothing: no name at all, and a name that is still the URL.
func TestSafeTaskFileNotYetResolvedIsNoBytesNotAnEscape(t *testing.T) {
	a, _ := newFilesTestApp(t)
	cases := map[string]core.Task{
		"empty name":         {URL: "https://host.example/x.bin", Status: core.StatusCollected},
		"name still the url": {URL: "https://host.example/x.bin", Name: "https://host.example/x.bin", Status: core.StatusCollected},
	}
	for name, tmpl := range cases {
		t.Run(name, func(t *testing.T) {
			task := putTask(t, a, tmpl)
			if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNoBytes) {
				t.Errorf("err = %v, want ErrTaskFileNoBytes", err)
			}
		})
	}
}

func TestSafeTaskFileNothingWrittenYet(t *testing.T) {
	a, _ := newFilesTestApp(t)
	task := putTask(t, a, core.Task{URL: "https://host.example/x.bin", Name: "movie.mkv", Status: core.StatusQueued})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNoBytes) {
		t.Errorf("err = %v, want ErrTaskFileNoBytes", err)
	}
}

func TestSafeTaskFileNotLocalRefusesAJDTask(t *testing.T) {
	a, base := newFilesTestApp(t)
	// A file at the joined path must not change the answer for a JD task.
	writeTestFile(t, base, "movie.mkv", []byte("x"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone, Resolver: "jd",
	})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileNotLocal) {
		t.Errorf("err = %v, want ErrTaskFileNotLocal", err)
	}
}

func TestSafeTaskFileNameWithSeparatorIsRefused(t *testing.T) {
	a, base := newFilesTestApp(t)
	// The file a naive join would serve.
	writeTestFile(t, filepath.Dir(base), "passwd", []byte("root:x:0:0"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/x", Name: "../passwd", Status: core.StatusDone,
	})
	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}

func TestSafeTaskFileSymlinkEscapeIsRefused(t *testing.T) {
	a, base := newFilesTestApp(t)
	outside := t.TempDir()
	writeTestFile(t, outside, "secret.bin", []byte("not for this task"))
	if err := os.Symlink(filepath.Join(outside, "secret.bin"), filepath.Join(base, "movie.mkv")); err != nil {
		// Windows needs a privilege to create symlinks.
		t.Skipf("symlinks are not available here: %v", err)
	}
	task := putTask(t, a, core.Task{URL: "https://host.example/movie.mkv", Name: "movie.mkv", Status: core.StatusDone})

	if _, err := a.SafeTaskFile(task.ID); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}

// torrentTask stages a finished torrent of three files in its own folder
// under base, the way the engine leaves one.
func torrentTask(t *testing.T, a *App, base string) *core.Task {
	t.Helper()
	root := filepath.Join(base, "Show")
	writeTestFile(t, filepath.Join(root, "S01"), "e01.mkv", []byte("episode one"))
	writeTestFile(t, filepath.Join(root, "S01"), "e02.mkv", []byte("episode two, longer"))
	writeTestFile(t, root, "info.nfo", []byte("notes"))
	return putTask(t, a, core.Task{
		URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Name: "Show",
		Status: core.StatusDone, File: root,
		TorrentFiles: []core.TorrentFile{
			{Path: "S01/e01.mkv", Size: 11, Selected: true},
			{Path: "S01/e02.mkv", Size: 19, Selected: true},
			{Path: "info.nfo", Size: 5, Selected: false},
		},
	})
}

func TestSafeTaskFileAtServesOneFileOfATorrent(t *testing.T) {
	a, base := newFilesTestApp(t)
	task := torrentTask(t, a, base)
	got, err := a.SafeTaskFileAt(task.ID, 1)
	if err != nil {
		t.Fatalf("SafeTaskFileAt: %v", err)
	}
	if got.Name != "e02.mkv" || got.Size != int64(len("episode two, longer")) {
		t.Errorf("got %s of %d bytes, want e02.mkv of the second episode's size", got.Name, got.Size)
	}
}

func TestSafeTaskFileAtRefusesAFileTheTorrentDoesNotFetch(t *testing.T) {
	a, base := newFilesTestApp(t)
	task := torrentTask(t, a, base)
	for _, index := range []int{2, 3} {
		if _, err := a.SafeTaskFileAt(task.ID, index); !errors.Is(err, ErrTaskFileNoSuchFile) {
			t.Errorf("file %d: err = %v, want ErrTaskFileNoSuchFile", index, err)
		}
	}
}

func TestSafeTaskFileAtRefusesATorrentPathOutOfItsFolder(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "elsewhere.mkv", []byte("not in the torrent"))
	task := putTask(t, a, core.Task{
		URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Name: "Show",
		Status: core.StatusDone, File: filepath.Join(base, "Show"),
		TorrentFiles: []core.TorrentFile{
			{Path: "../elsewhere.mkv", Size: 18, Selected: true},
			{Path: "e01.mkv", Size: 1, Selected: true},
		},
	})
	if err := os.MkdirAll(filepath.Join(base, "Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SafeTaskFileAt(task.ID, 0); !errors.Is(err, ErrTaskFileEscape) {
		t.Errorf("err = %v, want ErrTaskFileEscape", err)
	}
}

// Play on a torrent of several files means its largest selected video.
func TestOpenTaskFilePicksTheLargestSelectedMediaFile(t *testing.T) {
	a, base := newFilesTestApp(t)
	task := torrentTask(t, a, base)
	of, err := a.OpenTaskFile(task.ID, -1)
	if err != nil {
		t.Fatalf("OpenTaskFile: %v", err)
	}
	defer of.File.Close()
	body, err := io.ReadAll(of.File)
	if err != nil {
		t.Fatal(err)
	}
	if of.Name != "e02.mkv" || of.Index != 1 || string(body) != "episode two, longer" || of.Live {
		t.Errorf("opened %s (file %d, live %v) with %q, want the second episode from disk", of.Name, of.Index, of.Live, body)
	}
}

func TestOpenTaskFileOfATorrentWithoutMediaSaysSo(t *testing.T) {
	a, base := newFilesTestApp(t)
	root := filepath.Join(base, "Album")
	writeTestFile(t, root, "cover.jpg", []byte("art"))
	writeTestFile(t, root, "notes.txt", []byte("notes"))
	task := putTask(t, a, core.Task{
		URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Name: "Album",
		Status: core.StatusDone, File: root,
		TorrentFiles: []core.TorrentFile{
			{Path: "cover.jpg", Size: 3, Selected: true},
			{Path: "notes.txt", Size: 5, Selected: true},
		},
	})
	if _, err := a.OpenTaskFile(task.ID, -1); !errors.Is(err, ErrTaskFileNoMedia) {
		t.Errorf("err = %v, want ErrTaskFileNoMedia", err)
	}
}

// The engine sizes an HTTP download's file in full when it starts, so the
// file of a paused one is as long as the finished one and has holes.
func TestAPausedDownloadIsNotServedWithItsHoles(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "disc.iso", make([]byte, 64))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/disc.iso", Name: "disc.iso",
		Status: core.StatusPaused, Size: 64, Loaded: 32,
	})
	if _, err := a.OpenTaskFile(task.ID, -1); !errors.Is(err, ErrTaskFileIncomplete) {
		t.Errorf("err = %v, want ErrTaskFileIncomplete", err)
	}
}

// While a transfer the library finished short is being mended, the task runs
// but the engine hands out no reader, and the file on disk has its full size.
func TestARunningDownloadTheEngineCannotStreamIsNotServedWithItsHoles(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "disc.iso", make([]byte, 64))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/disc.iso", Name: "disc.iso",
		Status: core.StatusRunning, Size: 64, Loaded: 48,
	})
	if _, err := a.OpenTaskFile(task.ID, -1); !errors.Is(err, ErrTaskFileMending) {
		t.Errorf("err = %v, want ErrTaskFileMending", err)
	}
}

// A link that never started has nothing to play, which is not the same as a
// download that stopped halfway.
func TestALinkThatNeverStartedHasNothingToPlay(t *testing.T) {
	a, base := newFilesTestApp(t)
	cases := []core.Task{
		{URL: "https://host.example/disc.iso", Name: "disc.iso", Status: core.StatusCollected, Size: 64},
		{URL: "https://host.example/disc.iso", Name: "disc.iso", Status: core.StatusQueued, Size: 64},
		{
			URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Name: "film.mkv",
			Status: core.StatusQueued, File: filepath.Join(base, "film.mkv"),
			TorrentFiles: []core.TorrentFile{{Path: "film.mkv", Size: 64, Selected: true}},
		},
	}
	for _, c := range cases {
		task := putTask(t, a, c)
		if _, err := a.OpenTaskFile(task.ID, -1); !errors.Is(err, ErrTaskFileNoBytes) {
			t.Errorf("%s %s: err = %v, want ErrTaskFileNoBytes", c.Status, c.Name, err)
		}
	}
}

func TestAFinishedFileOfAStoppedDownloadIsServed(t *testing.T) {
	a, base := newFilesTestApp(t)
	writeTestFile(t, base, "disc.iso", []byte("complete"))
	task := putTask(t, a, core.Task{
		URL: "https://host.example/disc.iso", Name: "disc.iso",
		Status: core.StatusError, Size: 8, Loaded: 8,
	})
	of, err := a.OpenTaskFile(task.ID, -1)
	if err != nil {
		t.Fatalf("OpenTaskFile: %v", err)
	}
	of.File.Close()
}

// A paused torrent keeps an unfinished file under another name, which reads
// as stopped, not as a download that never began.
func TestAnUnfinishedFileOfAPausedTorrentIsReportedAsStopped(t *testing.T) {
	a, base := newFilesTestApp(t)
	task := torrentTask(t, a, base)
	if err := os.Rename(filepath.Join(base, "Show", "S01", "e02.mkv"), filepath.Join(base, "Show", "S01", "e02.mkv.part")); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.tasks[task.ID].Status = core.StatusPaused
	a.tasks[task.ID].Loaded = 20
	a.mu.Unlock()
	if _, err := a.OpenTaskFile(task.ID, -1); !errors.Is(err, ErrTaskFileIncomplete) {
		t.Errorf("err = %v, want ErrTaskFileIncomplete", err)
	}
	of, err := a.OpenTaskFile(task.ID, 0)
	if err != nil {
		t.Fatalf("the finished episode of the paused torrent: %v", err)
	}
	of.File.Close()
}

// A player still reading a finished file must not keep the rename and the
// delivery that follow the download from moving it.
func TestAFileBeingPlayedCanStillBeMoved(t *testing.T) {
	a, base := newFilesTestApp(t)
	id := putTask(t, a, core.Task{
		URL: "https://host.example/film.mkv", Name: "film.mkv", Status: core.StatusDone,
	}).ID
	writeTestFile(t, base, "film.mkv", []byte("frames"))
	of, err := a.OpenTaskFile(id, -1)
	if err != nil {
		t.Fatalf("OpenTaskFile: %v", err)
	}
	defer of.File.Close()
	if err := os.Rename(filepath.Join(base, "film.mkv"), filepath.Join(base, "renamed.mkv")); err != nil {
		t.Fatalf("rename while the file is open: %v", err)
	}
}
