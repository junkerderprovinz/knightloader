package api

// The rule editor's two reads. Neither of them stores anything.
//
// The rule sets have no endpoint here, for the reason the connection list has
// none: they are fields of the settings document that GET and PUT
// /api/settings already carry, and a second write path is a second place to
// get the round trip wrong. On a link filter that shows up as links
// disappearing.
//
// What is left is the two questions a save cannot answer: what may a rule
// contain, and what would the set being edited right now do to a link.

import (
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// maxPreviewLinks bounds one dry run. The cost is rules times links and both
// halves come from the request, so one POST could otherwise run ten thousand
// rules against ten thousand samples while downloads wait for the same CPU.
const maxPreviewLinks = 50

// previewLink is one sample as the editor sends it. Not rules.Candidate
// itself, whose Added is the server's to fill in: the date variables preview
// against this machine's clock, or a folder named by <jd:year> reads back
// whatever the browser claimed.
type previewLink struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Source   string `json:"source"`
	Package  string `json:"package"`
	Filesize int64  `json:"filesize"`
	// Hoster and Filetype are absent because Candidate derives both. Accepting
	// them from the client would let a dry run disagree with what staging
	// computes.
}

// registerRules ignores the app, and that is the property worth keeping: both
// routes answer from the request body and the compiled-in grammar. A dry run
// that reached into the running instance would describe the stored set while
// the user is looking at an unsaved one.
func registerRules(reg *Registry, _ *app.App) {
	// The grammar is compiled in and identical for every client, but served
	// rather than kept as a constant in the bundle: a form offering an
	// operator this build refuses produces a rule that saves cleanly and
	// never fires.
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
