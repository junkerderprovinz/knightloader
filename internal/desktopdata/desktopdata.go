// Package desktopdata is where the desktop app keeps its data, and what its
// installer fetches into it so the first start finds ffmpeg, the Java runtime
// and JDownloader in place. The installer is a module of its own, so the
// folders live here, where it and the app read them from the same code.
package desktopdata

import (
	"context"
	"os"
	"path/filepath"

	"github.com/junkerderprovinz/knightloader/internal/jre"
	"github.com/junkerderprovinz/knightloader/internal/mediatools"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

// Dir is the data folder of the desktop app named product: KL_DATA when it is
// set, else product under the user's config folder.
func Dir(product string) string {
	if v := os.Getenv("KL_DATA"); v != "" {
		return v
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, product)
	}
	return "kl-data"
}

// JDDir is the home of the app's own JDownloader.
func JDDir(dataDir string) string { return filepath.Join(dataDir, "jd") }

// Component is one thing the installer fetches. Target is the file whose
// presence means the app has it already.
type Component struct {
	ID     string
	Name   string
	Target string
	Fetch  func(ctx context.Context, progress func(done, total int64)) error
}

// Components are fetched the way the app itself fetches them, with the same
// checks and records, so it takes each copy as its own and keeps it current.
func Components(dataDir string) []Component {
	jd := provision.New(JDDir(dataDir))
	return []Component{
		{
			ID:     "ffmpeg",
			Name:   "FFmpeg",
			Target: mediatools.FFmpegPath(dataDir),
			Fetch: func(ctx context.Context, progress func(done, total int64)) error {
				// One on PATH is the person's own, which the app runs instead.
				if mediatools.ResolveFFmpeg(dataDir).FFmpegSource == mediatools.SourcePath {
					return nil
				}
				_, err := mediatools.InstallFFmpeg(ctx, dataDir, progress)
				return err
			},
		},
		{
			ID:     "java",
			Name:   "Java",
			Target: jre.Java(dataDir),
			Fetch: func(ctx context.Context, progress func(done, total int64)) error {
				// The runtime is staged inside the data folder, which a first
				// install has yet to create.
				if err := os.MkdirAll(dataDir, 0o755); err != nil {
					return err
				}
				_, err := jre.Ensure(ctx, dataDir, progress)
				return err
			},
		},
		{
			ID:     "jdownloader",
			Name:   "JDownloader",
			Target: jd.JarPath(),
			Fetch: func(ctx context.Context, progress func(done, total int64)) error {
				return jd.EnsureJar(ctx, progress)
			},
		},
	}
}
