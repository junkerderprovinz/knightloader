package api

// Other KnightLoader instances on the same network: registering them, and
// forwarding task operations to one.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// refuseFederationOff answers every call that would reach a peer while the
// module is switched off. The peers stay stored and come back with it. The
// code lets the interface say it in the reader's language.
func refuseFederationOff(w http.ResponseWriter) {
	writeRefusal(w, http.StatusServiceUnavailable, "federationOff",
		`"Instances" is switched off on the Modules page`, nil)
}

func registerFederation(reg *Registry, a *app.App) {
	reg.Add(http.MethodGet, "/api/instances", "the peer instances this one knows about; none while the module is switched off",
		func(w http.ResponseWriter, r *http.Request) {
			if a.ModuleOff("federation") {
				writeJSON(w, []federation.Instance{})
				return
			}
			writeJSON(w, a.Federation.List())
		})
	reg.Add(http.MethodPost, "/api/instances", "register a peer instance and report whether it answered",
		func(w http.ResponseWriter, r *http.Request) {
			var in federation.Instance
			if !decodeJSON(w, r, &in) {
				return
			}
			if a.ModuleOff("federation") {
				refuseFederationOff(w)
				return
			}
			if err := addPeer(a, in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Three outcomes, not two: "reached it and it refused this
			// instance's credentials" needs a different fix from "could not
			// reach it at all", and adding a peer by address exchanges no
			// credential. Collapsing both into "offline" makes a
			// password-locked peer look switched off. The answer is the
			// connection phrase, which authenticates both ends by the same
			// key with nothing to copy per peer; refused says which of the
			// two happened.
			err := a.Federation.Ping(r.Context(), in.Name)
			writeJSON(w, map[string]any{
				"name": in.Name, "url": in.URL,
				"online":  err == nil,
				"refused": errors.Is(err, federation.ErrUnauthorized),
			})
		})
	reg.Add(http.MethodDelete, "/api/instances/{name}", "forget a peer instance",
		func(w http.ResponseWriter, r *http.Request) {
			name := r.PathValue("name")
			if err := a.Federation.Remove(name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Removing a peer has to end the credentials too. Otherwise the
			// peer keeps a live full-power token on this instance, and the
			// token this instance held gets re-attached to whatever is
			// registered under that name next.
			forgetPeerCredentials(a, name)
			w.WriteHeader(http.StatusNoContent)
		})
	// Proxy task operations to a peer instance: only the task, link and queue
	// routes, the read of the unpackings and a clipboard watcher's lease are
	// forwarded, so a peer's settings/accounts stay local to that peer.
	reg.Add(AnyMethod, forwardPattern, "forward a task, link or queue request, a read of the unpackings or a clipboard watcher's lease to a peer; nothing else is forwarded, "+
		"and a token needs the right the forwarded call would need on this instance",
		func(w http.ResponseWriter, r *http.Request) {
			rest := r.PathValue("rest")
			if a.ModuleOff("federation") {
				refuseFederationOff(w)
				return
			}
			// rest arrives unescaped, so tasks%2F..%2Fsettings reads
			// tasks/../settings. It would pass the list below, and the peer's
			// mux would redirect it to /api/settings with the peer token still
			// attached.
			if path.Clean("/"+rest) != "/"+rest {
				http.Error(w, "route not proxied", http.StatusForbidden)
				return
			}
			// The queue travels with the task list, being that list's master
			// switch: showing a peer's downloads and then ordering, forcing or
			// stopping them on this box would act on the wrong machine.
			// Settings and accounts stay where they are configured. The
			// unpackings are read only, for the rows to show how far a
			// peer's archives have got. A clipboard watcher's lease travels
			// with its links, since it renews with the instance they go to.
			readsJobs := rest == "extract" && r.Method == http.MethodGet
			if !readsJobs && !clipLeaseCall(r.Method, rest) && rest != "links" && rest != "tasks" && rest != "queue" &&
				!strings.HasPrefix(rest, "tasks/") && !strings.HasPrefix(rest, "queue/") {
				http.Error(w, "route not proxied", http.StatusForbidden)
				return
			}
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			}
			// The peer takes the call on this instance's own full token, so a
			// narrower token is held to what the call would need here.
			if tok, ok := tokenOf(r); ok {
				if need := reg.scopeOfCall(r.Method, "/api/"+rest, body); !tok.Has(need) {
					refuseScope(w, need)
					return
				}
			}
			target := "/api/" + rest
			// The query is not forwarded, but a closing tab's lease call
			// needs it to keep a stop waiting for the tab's next renewal.
			if clipLeaseCall(r.Method, rest) && r.URL.Query().Get("pause") != "" {
				target += "?pause=1"
			}
			name := r.PathValue("name")
			resp, code, err := a.Federation.Proxy(r.Context(), name, r.Method, target, body)
			if err != nil {
				http.Error(w, err.Error(), code)
				return
			}
			if addsLinks(r.Method, target) && code/100 == 2 {
				a.Hub.Broadcast("handover", handover{Peer: name, Out: true})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(resp)
		})
}

// handover is the hub's "handover" message: links have gone from this instance
// to another, or come in from a member of its group. The picture of the group
// sends a parcel along that member's line.
type handover struct {
	// Peer is who the links went to or came from, as GET /api/instances names
	// an instance and GET /api/connect a phone or a browser extension.
	Peer string `json:"peer"`
	// Out is true for links this instance sent.
	Out bool `json:"out"`
}

// addsLinks reports whether a call hands links over, as opposed to reading or
// steering what is already there.
func addsLinks(method, target string) bool {
	route, _, _ := strings.Cut(target, "?")
	return method == http.MethodPost && route == "/api/links"
}

// announceArrivals tells the open pages when a member of the group has handed
// links to this instance. A phone and an extension say who they are in the
// call and a member on this network is named by its transport; an instance
// calling over the relay is not named by anything, so its links arrive
// unannounced.
func announceArrivals(a *app.App, serve relay.ProxyHandler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		status, out := serve(ctx, call)
		from := call.From
		if from == "" {
			from = federation.Sender(ctx)
		}
		if from != "" && addsLinks(call.Method, call.Path) && status/100 == 2 {
			a.Hub.Broadcast("handover", handover{Peer: from})
		}
		return status, out
	}
}
