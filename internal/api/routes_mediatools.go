package api

// The two external programs a media download runs - yt-dlp and ffmpeg - and
// the one operation that changes which yt-dlp that is.
//
// FOUR ROUTES AND NOT ONE, AND THE SPLIT IS THE POINT. GET /api/mediatools is
// read by the Resolvers settings page on every load and by buildDiagnostics on
// every bundle, so it must never make an outbound call: folding "and ask GitHub
// what the newest release is" into it would turn opening a settings page into a
// call to api.github.com that nobody opted into, on a machine somebody runs
// themselves. Asking GitHub is its own route, reached by pressing a button or
// by switching on a toggle that ships off.
//
// There is deliberately NO route that installs on its own. An auto-install
// switch for yt-dlp was considered and explicitly not built (jdp, 2026-09-08):
// replacing the extractor unattended silently changes what a download produces,
// yt-dlp does ship regressions, and internal/update already refuses the same
// step for this app's own binary. Fetching happens because somebody pressed
// "fetch this release", and only then.

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// ytdlpInstalled is what a successful fetch answers with. A struct rather than
// the ManagedRecord itself, because the page needs one thing the record does
// not carry - where the file ended up - and because the record is a stored
// document whose shape should not be pinned by an HTTP response.
type ytdlpInstalled struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
}

func registerMediaTools(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/mediatools",
		"which yt-dlp, ffmpeg and ffprobe this instance runs, where they come from and what version they report; never calls out",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, a.MediaTools())
		})

	reg.Add(http.MethodGet, "/api/mediatools/ytdlp/latest",
		"ask github.com for yt-dlp's newest release and compare it against the installed one; the only route here that leaves the box",
		func(w http.ResponseWriter, r *http.Request) {
			// Never an error status: "GitHub could not be asked" is an answer
			// this route knows how to give (Checked:false plus GitHub's own
			// words in Detail), and a 500 here would lose that sentence and
			// leave the page with nothing but "request failed".
			writeJSON(w, a.YtdlpLatest(r.Context()))
		})

	reg.Add(http.MethodPost, "/api/mediatools/ytdlp/update",
		"fetch yt-dlp's newest release, verify it against the release's own SHA2-256SUMS, prove it runs here, and put it in force",
		func(w http.ResponseWriter, r *http.Request) {
			// Blocks: the download plus the smoke test is seconds to a minute
			// over a slow link, the same shape POST /api/system/update-install
			// already has. Nothing is replaced until every step has passed, so
			// a request the browser gives up on leaves the working copy alone.
			rec, err := a.UpdateYtdlp(r.Context())
			if err != nil {
				// The package's own sentence, verbatim. It names the asset it
				// tried, the program's own failure and what to do about it -
				// all of which a generic "update failed" would throw away, and
				// none of which this layer could reconstruct.
				writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, ytdlpInstalled{
				Tag:     rec.Tag,
				Version: rec.Version,
				// Read back rather than composed here, so the path in the
				// answer is the path the resolver just started using.
				Path:   a.MediaTools().Ytdlp.Path,
				Asset:  rec.Asset,
				SHA256: rec.SHA256,
			})
		})

	reg.Add(http.MethodPost, "/api/mediatools/ytdlp/revert",
		"delete the fetched yt-dlp and hand KL_YTDLP or PATH back the job",
		func(w http.ResponseWriter, r *http.Request) {
			st, err := a.RevertYtdlp()
			if err != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			// The resulting status, not an empty 200: the card has to say which
			// yt-dlp is running now, and a second request to find out would
			// draw a gap in between.
			writeJSON(w, st)
		})
}
