package api

// Asking the router for a new public address.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// reconnectState is what the settings page shows above the run button: the two
// booleans the app derives, plus the reason behind the first of them.
//
// The reason is here rather than in app.ReconnectState because it is a sentence
// for a person, not a fact the app acts on. It is Validate's own message, never
// a second opinion about it: a form that decided for itself which field was
// missing would disagree with the save endpoint the moment either side changed,
// and the user would be looking at two different answers about one page.
type reconnectState struct {
	app.ReconnectState
	// Reason is the English sentence. It stays because it is what a log, a
	// scripted client and `curl` all want, and because a code the interface has
	// not learned yet must still say something.
	Reason string `json:"reason,omitempty"`
	// ReasonCode is the same fact as a value, for an interface that has words of
	// its own in forty-two languages. The sentence cannot be translated on this
	// side: the server would need the reader's language on a settings request,
	// and the log would then be written in whoever asked last.
	ReasonCode string `json:"reasonCode,omitempty"`
	// The one detail each code needs: the offending request's position, the
	// unrecognised method as typed, the variable with no value.
	ReasonN      int    `json:"reasonN,omitempty"`
	ReasonMethod string `json:"reasonMethod,omitempty"`
	ReasonVar    string `json:"reasonVar,omitempty"`
}

func registerReconnect(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/reconnect", "whether a reconnect is configured, whether one is running, and what is missing if it is not",
		func(w http.ResponseWriter, r *http.Request) {
			st := reconnectState{ReconnectState: a.ReconnectState()}
			if err := a.Settings.Get().Reconnect.Validate(); err != nil {
				// Safe to hand over: Validate names fields and methods, and never
				// reaches the password. Every other error out of this package goes
				// through Config.redact for exactly that reason.
				st.Reason = err.Error()
				// The typed half, when the error carries one. Anything else that
				// ever comes back from Validate keeps the sentence and no code,
				// which the page renders verbatim rather than showing a blank.
				var p *reconnect.ConfigProblem
				if errors.As(err, &p) {
					st.ReasonCode, st.ReasonN = p.Code, p.N
					st.ReasonMethod, st.ReasonVar = p.Method, p.Var
				}
			}
			writeJSON(w, st)
		})
	reg.Add(http.MethodPost, "/api/reconnect", "run one reconnect now and report the addresses either side of it",
		func(w http.ResponseWriter, r *http.Request) {
			// A second request while one is running is refused rather than queued
			// behind it: the reconnect package would make the caller wait for the
			// first run's verdict, and an HTTP request that hangs for two minutes with
			// no explanation is worse than a plain "already running".
			if a.ReconnectState().Busy {
				http.Error(w, "a reconnect is already running", http.StatusConflict)
				return
			}
			res, err := a.Reconnect(r.Context())
			if err != nil {
				// Safe to hand back verbatim: the reconnect package filters the router
				// password out of every error on its way out.
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{
				"oldIp":  res.OldIP.String(),
				"newIp":  res.NewIP.String(),
				"checks": res.Checks,
				"tookMs": res.Took.Milliseconds(),
			})
		})

	// The router address the form offers. It is a route rather than a value in
	// the settings document because the answer is about the machine this instance
	// is running on right now: moved to another host, or run in a container on a
	// different bridge, the stored answer would be a plausible address on
	// somebody else's network with the router password pointed at it.
	reg.Add(http.MethodGet, "/api/reconnect/router", "the default gateway of this machine, for the router address field",
		func(w http.ResponseWriter, r *http.Request) {
			addr, err := reconnect.DefaultGateway()
			if err != nil {
				// 404, not 500: there being no gateway to read here is an ordinary
				// answer on a platform whose routing table this package cannot open,
				// and a 500 would put a line in the log of every reverse proxy in
				// front of the app for a question that was answered correctly. The
				// message says which of the two it was, and the field shows it -
				// a blank box that stays blank teaches nobody anything.
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, addr)
		})

	// Parse only, exactly like the proxy list import: nothing is stored, because
	// the requests belong in the settings draft the page is already holding and a
	// second writer for one field is how the two come apart. It also means a
	// script can be pasted, read and thrown away without touching a working
	// configuration.
	reg.Add(http.MethodPost, "/api/reconnect/import",
		"read a pasted LiveHeader or curl reconnect script into requests, naming every line it refuses and why; stores nothing",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Text string `json:"text"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			imp, err := reconnect.ImportScript(body.Text)
			out := reconnectImport{Import: imp}
			if err != nil {
				out.Error = err.Error()
			}
			// 200 with the refusal in the body, not a 4xx. The request was
			// perfectly good - it is the script that was not - and everything the
			// user needs to fix it is in this body: what mapped, what did not, and
			// on which line. A status code cannot carry any of that, and a 4xx
			// would have the browser log the one response somebody is meant to read.
			writeJSON(w, out)
		})
}

// reconnectImport is an import plus the one sentence the two lists cannot say.
//
// Both halves always travel: ImportScript fills Requests as far as it got even
// when it refuses the script, and showing only the problems leaves somebody
// guessing whether the login block was understood. Error is set whenever the
// import must not be stored, which includes the failures that produce no
// per-line problem at all - a file with no request blocks in it, or one longer
// than the parser will read.
type reconnectImport struct {
	reconnect.Import
	Error string `json:"error,omitempty"`
}
