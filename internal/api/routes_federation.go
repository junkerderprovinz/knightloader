package api

// The other instances of the group: listing them, and forwarding task
// operations to one.

import (
	"context"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/parleyport/kit/relay"
)

// refuseFederationOff answers every call that would reach another instance
// while the module is switched off. The group stays and comes back with it.
// The code lets the interface say it in the reader's language.
func refuseFederationOff(w http.ResponseWriter) {
	writeRefusal(w, http.StatusServiceUnavailable, "federationOff",
		`"Instances" is switched off on the Instances page`, nil)
}

func registerFederation(reg *Registry, a *app.App) {
	forgetInstancesAddedByAddress(a)

	reg.Add(http.MethodGet, "/api/instances", "the other instances of this instance's group reachable now; none while the module is switched off",
		func(w http.ResponseWriter, r *http.Request) {
			if a.ModuleOff("federation") {
				writeJSON(w, []federation.Instance{})
				return
			}
			writeJSON(w, a.Federation.List())
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
			// tasks/../settings. It would pass the list below, and the other
			// instance would serve /api/settings to this one's member key.
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
			// The other instance takes the call as from this one, so a
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
// links to this instance. It runs behind the group's guard, so the member a
// call names is the one whose pinned key signed it.
func announceArrivals(a *app.App, serve relay.ProxyHandler) relay.ProxyHandler {
	return func(ctx context.Context, call relay.ProxyCall) (int, []byte) {
		status, out := serve(ctx, call)
		if call.From != "" && addsLinks(call.Method, call.Path) && status/100 == 2 {
			a.Hub.Broadcast("handover", handover{Peer: call.From})
		}
		return status, out
	}
}

// addressCredentials is the accounts service older releases kept the token
// for calling an instance added by its address under.
const addressCredentials = "federation-peer"

// forgetInstancesAddedByAddress drops the credentials older releases kept for
// instances added by their address: the token for calling each one, and the
// tokens minted for them, which would stay live with nothing left to use
// them. Instances pair by the phrase alone.
func forgetInstancesAddedByAddress(a *app.App) {
	for _, peer := range a.Accounts.AccountIDs(addressCredentials) {
		if err := a.Accounts.SetCredential(addressCredentials, peer, accounts.Credential{}); err != nil {
			log.Printf("instances: could not forget the credential for %s: %v", peer, err)
		}
	}
	for _, t := range a.APITokens.List() {
		if strings.HasPrefix(t.Name, "peer: ") {
			if err := a.APITokens.Revoke(t.ID); err != nil {
				log.Printf("instances: could not revoke the token %q: %v", t.Name, err)
			}
		}
	}
}
