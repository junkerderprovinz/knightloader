package api

// The rule editor's two reads. Neither of them stores anything.
//
// The rule sets themselves have no endpoint here, for the same reason the
// connection list has none: they are fields of the settings document, GET and
// PUT /api/settings already carry them, and a second write path is a second
// place to get the round trip wrong. The symptom of getting it wrong on a link
// filter is links disappearing, which is the one failure this whole subsystem
// exists to prevent.
//
// What is left is the two questions a save cannot answer: what may a rule
// contain, and what would this set — the one being edited right now, not the one
// on disk — do to a link.

import (
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// maxPreviewLinks bounds one dry run.
//
// The cost is rules × links and both halves come from the request, so a single
// POST could otherwise ask this instance to run ten thousand rules against ten
// thousand samples while somebody's downloads are waiting for the same CPU. The
// test box is a place to try three or four links; a limit far above what the
// interface offers, refused out loud, is enough.
const maxPreviewLinks = 50

// previewLink is one sample as the editor sends it. It is not rules.Candidate
// itself because Candidate carries Added as a time, which is the server's to
// fill in: the date variables have to preview against this machine's clock, or
// a folder named by <jd:year> would read back whatever the browser was told to
// claim.
type previewLink struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Source   string `json:"source"`
	Package  string `json:"package"`
	Filesize int64  `json:"filesize"`
	// Hoster and Filetype are deliberately absent. Candidate derives both, and a
	// preview that accepted them from the client could be made to disagree with
	// what staging will really compute — which is the one thing a dry run must
	// never do.
}

// The app is not used: both routes answer from the request body and the
// compiled-in grammar, and that is the property worth keeping. A dry run that
// reached into the running instance would be answering about the stored set
// while the user is looking at an unsaved one.
func registerRules(reg *Registry, _ *app.App) {
	// Static: the grammar is compiled in and identical for every client. It is a
	// route rather than a constant in the bundle so that a form can never offer an
	// operator this build refuses — a mismatch there produces a rule that saves
	// cleanly and then silently never fires.
	reg.Add(http.MethodGet, "/api/rules/grammar",
		"the fields, operators, actions, variables, categories and bounds a rule may be built from",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, rules.Describe())
		})

	reg.Add(http.MethodPost, "/api/rules/preview",
		"dry-run a rule set against sample links and report what each rule and each link would do; stores nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Set   rules.Set     `json:"set"`
				Links []previewLink `json:"links"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if len(body.Links) > maxPreviewLinks {
				http.Error(w, "too many sample links for one dry run", http.StatusBadRequest)
				return
			}
			// One clock read for the whole batch, so two samples in the same run
			// cannot land either side of midnight and produce two different folders
			// from one template.
			now := time.Now()
			cands := make([]rules.Candidate, 0, len(body.Links))
			for _, l := range body.Links {
				cands = append(cands, rules.Candidate{
					URL:      l.URL,
					Filename: l.Filename,
					Source:   l.Source,
					Package:  l.Package,
					Filesize: l.Filesize,
					Added:    now,
				})
			}
			// Preview builds its own Matcher, so the live one's <jd:append> counter
			// is untouched: previewing three links must not make the next real
			// download call itself "_4".
			writeJSON(w, rules.Preview(body.Set, cands))
		})
}
