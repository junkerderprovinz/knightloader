package api

// The two external programs a media download runs, yt-dlp and ffmpeg, and the
// one operation that changes which yt-dlp that is.
//
// The split into four routes keeps GET /api/mediatools free of outbound calls.
// The Resolvers settings page reads it on every load and buildDiagnostics on
// every bundle, so folding the release check into it would turn opening a
// settings page into a call to api.github.com nobody asked for. Asking GitHub
// is its own route, reached by a button or a toggle that ships off.
//
// No route installs on its own. Replacing the extractor unattended changes
// what a download produces, yt-dlp does ship regressions, and internal/update
// refuses the same step for this app's own binary. Fetching happens because
// somebody pressed "fetch this release".

import (
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// ytdlpInstalled is what a successful fetch answers with. Not the
// ManagedRecord itself: the page needs the path the file ended up at, and the
// record is a stored document whose shape should not be pinned by a response.
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
			// this route gives (Checked false plus GitHub's own words in
			// Detail), and a 500 would leave the page with "request failed".
			writeJSON(w, a.YtdlpLatest(r.Context()))
		})

	reg.Add(http.MethodPost, "/api/mediatools/ytdlp/update",
		"fetch yt-dlp's newest release, verify it against the release's own SHA2-256SUMS, prove it runs here, and put it in force",
		func(w http.ResponseWriter, r *http.Request) {
			// Blocks for the download plus the smoke test, the same shape
			// POST /api/system/update-install has. Nothing is replaced until
			// every step has passed, so a request the browser gives up on
			// leaves the working copy alone.
			rec, err := a.UpdateYtdlp(r.Context())
			if err != nil {
				// Verbatim: the package's sentence names the asset it tried
				// and the program's own failure, which this layer could not
				// reconstruct.
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
			// The resulting status, not an empty 200: the card has to say
			// which yt-dlp runs now, and a second request would draw a gap.
			writeJSON(w, st)
		})
}
