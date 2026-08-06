// Package api exposes the app over HTTP: a small REST surface plus a WebSocket
// stream under /api, and the embedded SPA everywhere else.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/web"
)

// Handler builds the full HTTP handler.
func Handler(a *app.App) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok", "version": buildinfo.Version})
	})
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Tasks())
	})
	mux.HandleFunc("POST /api/links", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Links   string `json:"links"` // newline-separated, like JD's paste box
			Package string `json:"package"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		urls := strings.FieldsFunc(body.Links, func(r rune) bool { return r == '\n' || r == '\r' })
		created := a.AddLinks(urls, body.Package)
		if created == nil {
			created = []*core.Task{} // an empty result is [] for clients, never null
		}
		writeJSON(w, created)
	})
	mux.HandleFunc("POST /api/tasks/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // empty/absent = start all collected
		a.StartTasks(body.Ids)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/package", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids     []string `json:"ids"`
			Package string   `json:"package"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Ids) == 0 {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		a.SetPackage(body.Ids, body.Package)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/restart", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // empty/absent = restart all errored
		a.RestartTasks(body.Ids)
		w.WriteHeader(http.StatusNoContent)
	})
	// The queue master switch. Separate from pausing a task: this decides
	// whether anything new starts at all.
	mux.HandleFunc("GET /api/queue", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Queue())
	})
	mux.HandleFunc("POST /api/queue", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Halted   *bool   `json:"halted,omitempty"`
			StopMark *string `json:"stopMark,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if body.Halted != nil {
			a.SetHalted(*body.Halted)
		}
		if body.StopMark != nil {
			a.SetStopMark(*body.StopMark)
		}
		writeJSON(w, a.Queue())
	})
	mux.HandleFunc("POST /api/tasks/recheck", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // empty/absent = recheck all collected
		go a.RecheckTasks(body.Ids)               // probing hosts can take a while
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /api/tasks/priority", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids      []string `json:"ids"`
			Priority int      `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Ids) == 0 {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		a.SetPriority(body.Ids, body.Priority)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/move", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids   []string `json:"ids"`
			Where string   `json:"where"` // "top" or "bottom"
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Ids) == 0 {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		a.MoveTasks(body.Ids, body.Where)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/options", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ids []string `json:"ids"`
			app.TaskOptions
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Ids) == 0 {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := a.SetTaskOptions(body.Ids, body.TaskOptions); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		a.Pause(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/tasks/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		a.Resume(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	// Removing a task takes it off the list. ?files=1 additionally deletes what
	// was downloaded — an explicit, opt-in act.
	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.Remove(r.PathValue("id"), r.URL.Query().Get("files") == "1")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, settingsBody(a, a.Settings.Get()))
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var s settings.Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		// Refuse a folder we cannot write to instead of accepting it and
		// downloading somewhere else.
		if err := settings.Validate(s.DownloadDir); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Rows that carry their own validator are refused here with the reason,
		// rather than being dropped by sanitize on the way to disk. A connection or
		// a schedule window that vanishes on save is the same class of bug as a
		// download folder that silently reverts, except the user blames the proxy
		// for it weeks later.
		if err := validateRows(s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		applied, err := a.ApplySettings(s)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, settingsBody(a, applied))
	})
	// One place every dropdown in the settings form comes from. Hard-coding these
	// in the interface is how a package gains a value nobody can select, and how a
	// value the app cannot honour stays selectable after it has been withdrawn.
	mux.HandleFunc("GET /api/options", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, options())
	})
	// A dry run of a rule list against sample links. Without it the only way to
	// find out what a rule does is to paste real links and watch, which for a
	// filter means finding out by losing something.
	mux.HandleFunc("POST /api/rules/test", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Set   rules.Set `json:"set"`
			Which string    `json:"which"` // "packagizer" or "filter"
			Links []struct {
				Filename string `json:"filename"`
				URL      string `json:"url"`
				Source   string `json:"source"`
				Filesize int64  `json:"filesize"`
				Package  string `json:"package"`
			} `json:"links"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		m, problems := rules.Compile(body.Set)
		type result struct {
			URL     string        `json:"url"`
			Effect  rules.Effect  `json:"effect"`
			Verdict rules.Verdict `json:"verdict"`
		}
		results := make([]result, 0, len(body.Links))
		now := time.Now()
		for _, l := range body.Links {
			c := rules.Candidate{
				Filename: l.Filename,
				URL:      l.URL,
				Source:   l.Source,
				Filesize: l.Filesize,
				Package:  l.Package,
				Added:    now,
			}
			// Both halves are reported whatever "which" says. They are one engine,
			// the answer to the other half costs nothing, and a set that rejects a
			// link while also naming a package for it is worth seeing whole.
			results = append(results, result{URL: l.URL, Effect: m.Apply(c), Verdict: m.Check(c)})
		}
		writeJSON(w, map[string]any{"problems": problemsOrEmpty(problems), "results": results})
	})
	// The trace for links that never became tasks. A link folded away with
	// nothing to show for it looks exactly like a bug in the paste box.
	mux.HandleFunc("GET /api/collector/skipped", func(w http.ResponseWriter, r *http.Request) {
		skipped := a.SkippedLinks()
		if skipped == nil {
			skipped = []app.SkippedLink{}
		}
		writeJSON(w, skipped)
	})
	mux.HandleFunc("DELETE /api/collector/skipped", func(w http.ResponseWriter, r *http.Request) {
		a.ClearSkipped()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/schedule", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.ScheduleState())
	})
	mux.HandleFunc("GET /api/reconnect", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.ReconnectState())
	})
	mux.HandleFunc("POST /api/reconnect", func(w http.ResponseWriter, r *http.Request) {
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
	// The account list is state, never secrets: which slots are filled, where the
	// value comes from, and what a test said.
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.AccountStates())
	})
	mux.HandleFunc("POST /api/accounts/{service}/test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.TestAccount(r.PathValue("service")))
	})
	mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Service string `json:"service"`
			Secret  string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Service == "" {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := a.SetAccount(body.Service, body.Secret); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/instances", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.Federation.List())
	})
	mux.HandleFunc("POST /api/instances", func(w http.ResponseWriter, r *http.Request) {
		var in federation.Instance
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := a.Federation.Add(in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		online := a.Federation.Ping(r.Context(), in.Name) == nil
		writeJSON(w, map[string]any{"name": in.Name, "url": in.URL, "online": online})
	})
	mux.HandleFunc("DELETE /api/instances/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Federation.Remove(r.PathValue("name")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Proxy task operations to a peer instance: only the task/link routes are
	// forwarded, so a peer's settings/accounts stay local to that peer.
	mux.HandleFunc("/api/instances/{name}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("rest")
		if rest != "links" && rest != "tasks" && !strings.HasPrefix(rest, "tasks/") {
			http.Error(w, "route not proxied", http.StatusForbidden)
			return
		}
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		}
		resp, code, err := a.Federation.Proxy(r.Context(), r.PathValue("name"), r.Method, "/api/"+rest, body)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(resp)
	})
	mux.HandleFunc("GET /api/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(a, w, r)
	})

	// Password lock. These routes stay reachable while locked out — they are how
	// you get back in.
	mux.HandleFunc("GET /api/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{
			"enabled":       a.Auth.Enabled(),
			"authenticated": authenticated(a, r),
		})
	})
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !a.Auth.Check(body.Password) {
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
		setSession(w, r, a.Auth.Issue())
		writeJSON(w, map[string]bool{"enabled": true, "authenticated": true})
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		clearSession(w, r)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		// Setting the first password is open (the instance is unprotected
		// anyway); changing or removing one requires the current password and an
		// existing session, which the guard below enforces.
		var body struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := a.Auth.SetPassword(body.Current, body.New); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.New != "" {
			setSession(w, r, a.Auth.Issue()) // don't lock out the person who just set it
		} else {
			clearSession(w, r)
		}
		writeJSON(w, map[string]bool{"enabled": a.Auth.Enabled(), "authenticated": true})
	})

	mux.Handle("/", spaHandler())
	return sameOrigin(guard(a, mux))
}

// settingsResponse is the settings plus what the rule engine could not compile.
// The two travel together on purpose: a rule dropped for a broken regular
// expression that the form never mentions is a rule the user goes on believing
// in, and for a filter that means links they think are blocked and are not.
type settingsResponse struct {
	settings.Settings
	Problems app.RuleProblems `json:"problems"`
}

// settingsBody is the only shape the settings ever leave in. Redacted is not
// optional here: the moment a client is shown the router password or a proxy
// password, the merge machinery that puts them back on save is protecting a
// value the client already holds.
func settingsBody(a *app.App, s settings.Settings) settingsResponse {
	return settingsResponse{Settings: s.Redacted(), Problems: a.RuleProblems()}
}

// validateRows refuses the rows that carry their own validator, naming the one
// that failed. Only sanitize would otherwise see them, and sanitize's job is to
// drop what it cannot use rather than to explain it.
func validateRows(s settings.Settings) error {
	for i, e := range s.Connections {
		if err := proxycfg.Validate(e); err != nil {
			return fmt.Errorf("connection %d: %w", i+1, err)
		}
	}
	// An unconfigured reconnect is the normal state of a fresh install, not a
	// reason to refuse the whole settings page. Only a half-filled one is.
	if s.Reconnect.Method != reconnect.MethodNone && s.Reconnect.Method != "" {
		if err := s.Reconnect.Validate(); err != nil {
			return err
		}
	}
	for i, e := range s.Schedule {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("schedule row %d: %w", i+1, err)
		}
	}
	return nil
}

// options is every fixed choice the settings form offers, taken from the
// packages that implement them so the two cannot drift.
func options() map[string]any {
	return map[string]any{
		"mirrorPolicies": dedupe.Policies(),
		// collide.Ask is deliberately withheld. It means "park the task until a
		// human answers", and there is no status for that and no way to answer, so
		// a task set to it would sit in the queue forever with nothing saying why.
		"collisionPolicies": policiesExcept(collide.Policies(), collide.Ask),
		"proxyKinds": []proxycfg.Kind{
			proxycfg.KindNone, proxycfg.KindDirect,
			proxycfg.KindHTTP, proxycfg.KindHTTPS,
			proxycfg.KindSOCKS4, proxycfg.KindSOCKS4A, proxycfg.KindSOCKS5,
		},
		"ruleFields": []rules.Field{
			rules.FieldFilename, rules.FieldURL, rules.FieldHoster, rules.FieldSource,
			rules.FieldFiletype, rules.FieldFilesize, rules.FieldPackage,
		},
		"ruleOps": []rules.Op{
			rules.OpContains, rules.OpEquals, rules.OpContainsNot,
			rules.OpEqualsNot, rules.OpMatches, rules.OpBetween,
		},
		// The Packagizer actions the app actually honours. "filename" is absent:
		// no backend accepts a destination file name, so a rename would only
		// desynchronise the list from the disk. Offering it would be a setting that
		// silently does nothing.
		"ruleActions":     []string{"packageName", "downloadDir", "comment", "priority", "autoExtract", "chunks"},
		"scheduleActions": []schedule.Action{schedule.ActionPause, schedule.ActionResume, schedule.ActionLimit},
	}
}

// policiesExcept drops a policy the app cannot honour from the menu, so it can
// never be chosen in the first place.
func policiesExcept(in []collide.Policy, drop collide.Policy) []collide.Policy {
	out := make([]collide.Policy, 0, len(in))
	for _, p := range in {
		if p != drop {
			out = append(out, p)
		}
	}
	return out
}

// problemsOrEmpty keeps a JSON null out of the response: a client checking the
// length of the list should not have to check for null first.
func problemsOrEmpty(in []rules.Problem) []rules.Problem {
	if in == nil {
		return []rules.Problem{}
	}
	return in
}

// authenticated reports whether the request carries a valid session, or whether
// no password is set at all.
func authenticated(a *app.App, r *http.Request) bool {
	if !a.Auth.Enabled() {
		return true
	}
	c, err := r.Cookie(auth.CookieName)
	return err == nil && a.Auth.Valid(c.Value)
}

func setSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL / time.Second),
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// openRoutes are reachable without a session: the login flow itself, the health
// probe a container orchestrator needs, and the UI assets that render the login
// screen.
func openRoutes(path string) bool {
	switch path {
	case "/api/health", "/api/auth", "/api/auth/login", "/api/auth/logout":
		return true
	}
	return !strings.HasPrefix(path, "/api/")
}

// guard refuses API calls without a session once a password is set.
func guard(a *app.App, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openRoutes(r.URL.Path) || authenticated(a, r) {
			next.ServeHTTP(w, r)
			return
		}
		// Setting the FIRST password needs no session; there is nothing to
		// protect yet and no way to get one.
		if r.URL.Path == "/api/auth/password" && !a.Auth.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// sameOrigin keeps other websites from driving this instance through the
// visitor's browser. The UI is served from the same origin as the API, so no
// cross-origin access is ever needed.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
			http.Error(w, "cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originMatchesHost compares an Origin header against the host being addressed.
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func serveWS(a *app.App, w http.ResponseWriter, r *http.Request) {
	// No InsecureSkipVerify: the library then requires Origin to match Host,
	// which is what stops another site from opening this socket.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	a.Hub.Add(c)
	defer func() {
		a.Hub.Remove(c)
		c.CloseNow()
	}()
	// Queued through the hub, not written straight to the socket: the writer
	// goroutine started with Add above, so a task event that arrives in between
	// could otherwise reach the client first and be overwritten by the older
	// snapshot.
	a.Hub.SendTo(c, "snapshot", a.Tasks())
	for {
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}

// spaHandler serves the embedded build, falling back to index.html for
// client-side routes.
//
// The bundle file names deliberately carry no content hash, so that the dist
// committed to the repository does not churn on every build. That leaves the
// cache with nothing to go on: embedded files have no modification time, so
// net/http sends no Last-Modified either, and a browser is then free to keep a
// stale app.js for as long as it likes — which after a redeploy means an old
// UI talking to a new API, or a blank page.
//
// The fix is an ETag over the file's own bytes plus no-cache, which does not
// mean "do not cache" but "revalidate before use". A reload then costs one
// conditional request that almost always answers 304.
func spaHandler() http.Handler {
	sub, _ := fs.Sub(web.Dist, "dist")
	etags := buildETags(sub)
	fileServer := http.FileServer(http.FS(sub))

	serve := func(w http.ResponseWriter, r *http.Request, name string) {
		if tag, ok := etags[name]; ok {
			w.Header().Set("ETag", tag)
		}
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			serve(w, r, "index.html")
			return
		}
		if _, err := fs.Stat(sub, p); err == nil {
			serve(w, r, p)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		serve(w, r2, "index.html")
	})
}

// buildETags hashes every embedded file once at startup. The build is immutable
// for the life of the process, so this is computed once rather than per request.
func buildETags(sub fs.FS) map[string]string {
	tags := map[string]string{}
	_ = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(sub, path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		tags[path] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	return tags
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
