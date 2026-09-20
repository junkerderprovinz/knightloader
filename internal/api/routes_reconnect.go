package api

// Asking the router for a new public address.

import (
	"errors"
	"net/http"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// reconnectState is what the settings page shows above the run button: the two
// booleans the app derives, plus the reason behind the first of them. The
// reason is Validate's own message rather than a second opinion, so the form
// and the save endpoint cannot give two answers about one page.
type reconnectState struct {
	app.ReconnectState
	// Reason is the English sentence, for a log, a scripted client and curl,
	// and for a code the interface has not learned yet.
	Reason string `json:"reason,omitempty"`
	// ReasonCode is the same fact as a value, for an interface with words of
	// its own. The sentence cannot be translated on this side: the server
	// would need the reader's language on a settings request, and the log
	// would then be written in whoever asked last.
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
				// Safe to hand over: Validate names fields and methods and
				// never reaches the password. Every other error out of this
				// package goes through Config.redact.
				st.Reason = err.Error()
				// The typed half, when the error carries one. Anything else
				// keeps the sentence and no code, which the page renders
				// verbatim rather than showing a blank.
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
			// Refused rather than queued behind the running one: the reconnect
			// package would make the caller wait for the first run's verdict,
			// and a request that hangs for two minutes says less than
			// "already running".
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

	// The router address the form offers. A route rather than a stored value,
	// because it describes the machine this instance runs on: moved to another
	// host, or into a container on a different bridge, a stored answer would
	// be a plausible address on somebody else's network with the router
	// password pointed at it.
	reg.Add(http.MethodGet, "/api/reconnect/router", "the default gateway of this machine, for the router address field",
		func(w http.ResponseWriter, r *http.Request) {
			addr, err := reconnect.DefaultGateway()
			if err != nil {
				// 404, not 500: no gateway to read is an ordinary answer on a
				// platform whose routing table this package cannot open, and a
				// 500 would put a line in every reverse proxy's log for a
				// question that was answered. The message says which of the
				// two it was, and the field shows it.
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, addr)
		})

	// Parse only, like the proxy list import: the requests belong in the
	// settings draft the page is holding, and a second writer for one field is
	// how the two come apart. A script can be pasted, read and thrown away
	// without touching a working configuration.
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
			// good and the script was not, and what mapped, what did not and
			// on which line is all in this body. A 4xx would have the browser
			// log the one response somebody is meant to read.
			writeJSON(w, out)
		})
}

// reconnectImport is an import plus the one sentence the two lists cannot say.
// Both halves travel: ImportScript fills Requests as far as it got even when
// it refuses the script, and showing only the problems leaves somebody
// guessing whether the login block was understood. Error is set whenever the
// import must not be stored, including the failures that produce no per-line
// problem, such as a file with no request blocks.
type reconnectImport struct {
	reconnect.Import
	Error string `json:"error,omitempty"`
}
