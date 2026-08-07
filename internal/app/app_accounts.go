package app

// Credentials and what they route: which services are configured, what they say
// when asked, and the resolver/backend table rebuilt whenever one changes.

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// rewireBackends rebuilds the resolver routing table and the download backends
// from the credentials currently stored. It runs at startup and again whenever
// an account changes, so adding or removing a debrid key takes effect
// immediately instead of on the next restart. Everything is assembled into
// locals first and swapped in at the end, so a running download never sees a
// half-built table.
func (a *App) rewireBackends() {
	eng, acc := a.Engine, a.Accounts

	// Resolve which hoster backends are configured. Each debrid service brings
	// its own supported-host list; their union tells file hosters (→ debrid/JD)
	// from media pages (→ yt-dlp).
	torboxKey := credential(acc, "torbox", "KL_TORBOX")
	jdBase := os.Getenv("KL_JD")

	var hosterSet map[string]bool
	if torboxKey != "" || jdBase != "" {
		hosterSet = fetchTorboxHosters(torboxKey)
	}

	// One-shot debrid services (AllDebrid, Real-Debrid): a single unlock call
	// yields a direct URL the engine downloads.
	type debridSetup struct {
		svc  debrid.Service
		prio int
	}
	var configured []debridSetup
	if k := credential(acc, "alldebrid", "KL_ALLDEBRID"); k != "" {
		configured = append(configured, debridSetup{debrid.NewAllDebrid(k), 34})
	}
	if k := credential(acc, "realdebrid", "KL_REALDEBRID"); k != "" {
		configured = append(configured, debridSetup{debrid.NewRealDebrid(k), 33})
	}
	newDebrid := map[string]backend{}
	for _, d := range configured {
		hosts := fetchDebridHosts(d.svc)
		newDebrid[d.svc.ID()] = debrid.NewBackend(d.svc, eng, a.onUpdate)
		a.Registry.Register(debrid.Resolver{ServiceID: d.svc.ID(), Prio: d.prio, Hosts: hosts})
		for h := range hosts {
			if hosterSet == nil {
				hosterSet = map[string]bool{}
			}
			hosterSet[h] = true
		}
		log.Printf("%s debrid backend enabled (%d supported hosts)", d.svc.Label(), len(hosts))
	}

	// Optional yt-dlp media backend: when the yt-dlp binary is present, media
	// pages (non-hoster, non-file links) route through it.
	var newYtdlp backend
	ytbin := os.Getenv("KL_YTDLP")
	if ytbin == "" {
		ytbin = "yt-dlp"
	}
	if yb := ytdlp.NewBackend(ytbin, a.dlDir, a.onUpdate); yb.Available() {
		// The limit in force rather than the one in the settings file. yt-dlp meters
		// itself because its bytes never pass through our loopback proxy, and the
		// limiter is what the timetable writes: reading the setting directly would
		// leave yt-dlp running at the daytime speed right through a nightly window.
		yb.RateLimit = a.Throttle.Limit
		yb.Dir = a.taskDir
		newYtdlp = yb
		a.Registry.Register(ytdlp.Resolver{ExcludeHosts: hosterSet})
		log.Printf("yt-dlp backend enabled: %s", ytbin)
	}

	// Optional TorBox debrid backend: when a key is present, supported hoster
	// links are unlocked into a direct CDN URL the engine then downloads.
	var newTorbox backend
	if torboxKey != "" {
		newTorbox = torbox.NewBackend(torbox.NewClient(torboxKey), eng, a.onUpdate)
		a.Registry.Register(torbox.Resolver{Hosts: hosterSet})
		log.Printf("TorBox debrid backend enabled (%d supported hosts)", len(hosterSet))
	}

	// Optional headless-JD backend: the lowest-priority catch-all for hoster
	// links nothing else claims, via JD's crawler and hoster plugins.
	var newJD backend
	if jdBase != "" {
		jb := jd.NewBackend(jdBase, a.onUpdate)
		if err := jb.Reachable(); err != nil {
			log.Printf("KL_JD set but JD unreachable (%v); skipping JD backend", err)
		} else {
			newJD = jb
			a.Registry.Register(jd.Resolver{})
			log.Printf("headless JD backend enabled: %s", jdBase)
		}
	}

	// A credential that is gone must stop claiming links, or those links would
	// route to a service that can no longer unlock them.
	for _, id := range []string{"alldebrid", "realdebrid"} {
		if _, ok := newDebrid[id]; !ok {
			a.Registry.Unregister(id)
		}
	}
	if newTorbox == nil {
		a.Registry.Unregister("torbox")
	}
	if newJD == nil {
		a.Registry.Unregister("jd")
	}
	if newYtdlp == nil {
		a.Registry.Unregister("ytdlp")
	}

	a.bmu.Lock()
	a.debrid, a.ytdlp, a.torbox, a.jd = newDebrid, newYtdlp, newTorbox, newJD
	a.bmu.Unlock()
}

// credential reads a service secret from the encrypted store, with the env var
// taking precedence (handy for containers and tests).
func credential(acc *accounts.Store, service, envVar string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	v, _ := acc.Get(service)
	return v
}

// fetchDebridHosts returns a service's supported-host set, or nil when the
// list can't be fetched (routing then degrades to the other backends).
func fetchDebridHosts(svc debrid.Service) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hosts, err := svc.Hosts(ctx)
	if err != nil {
		log.Printf("%s host list unavailable (%v); its routing is disabled", svc.Label(), err)
		return nil
	}
	return hosts
}

// SetAccount stores (or, with an empty secret, clears) a credential for a
// service such as "torbox", and re-wires the backends so it takes effect right
// away — a saved key that only works after a restart is a key that looks broken.
func (a *App) SetAccount(service, secret string) error {
	if err := a.Accounts.Set(service, secret); err != nil {
		return err
	}
	a.rewireBackends()
	return nil
}

// AccountState is what the settings page shows per service: whether a
// credential is stored, whether it currently works, and what the service said.
type AccountState struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
	FromEnv    bool   `json:"fromEnv"` // supplied by the container, not editable here
	OK         bool   `json:"ok"`
	Detail     string `json:"detail"`
	Hosts      int    `json:"hosts"` // supported hosters the service reports
}

// knownServices is the fixed set of credential slots the UI offers, with the
// environment variable that can supply each one instead.
var knownServices = []struct{ id, label, env string }{
	{"torbox", "TorBox", "KL_TORBOX"},
	{"alldebrid", "AllDebrid", "KL_ALLDEBRID"},
	{"realdebrid", "Real-Debrid", "KL_REALDEBRID"},
}

// AccountStates reports every credential slot without contacting anyone.
func (a *App) AccountStates() []AccountState {
	out := make([]AccountState, 0, len(knownServices))
	for _, svc := range knownServices {
		st := AccountState{ID: svc.id, Label: svc.label}
		if v := os.Getenv(svc.env); v != "" {
			st.Configured, st.FromEnv = true, true
		} else if v, _ := a.Accounts.Get(svc.id); v != "" {
			st.Configured = true
		}
		out = append(out, st)
	}
	return out
}

// TestAccount checks a stored credential against the service and reports what
// came back, so a typo in a key is visible here instead of on the first download.
func (a *App) TestAccount(service string) AccountState {
	st := AccountState{ID: service, Label: service}
	for _, svc := range knownServices {
		if svc.id == service {
			st.Label = svc.label
			st.FromEnv = os.Getenv(svc.env) != ""
		}
	}
	key := ""
	for _, svc := range knownServices {
		if svc.id == service {
			key = credential(a.Accounts, svc.id, svc.env)
		}
	}
	if key == "" {
		st.Detail = "no credential stored"
		return st
	}
	st.Configured = true

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var hosts map[string]bool
	var err error
	switch service {
	case "torbox":
		var list []torbox.Hoster
		if list, err = torbox.NewClient(key).Hosters(ctx); err == nil {
			hosts = map[string]bool{}
			for _, h := range list {
				for _, d := range h.Domains {
					hosts[d] = true
				}
			}
		}
	case "alldebrid":
		hosts, err = debrid.NewAllDebrid(key).Hosts(ctx)
	case "realdebrid":
		hosts, err = debrid.NewRealDebrid(key).Hosts(ctx)
	default:
		st.Detail = "unknown service"
		return st
	}
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	st.OK, st.Hosts = true, len(hosts)
	st.Detail = "credential accepted"
	return st
}

// fetchTorboxHosters returns the set of TorBox-supported hoster domains, or nil
// if the list can't be fetched (routing then degrades gracefully).
func fetchTorboxHosters(key string) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hs, err := torbox.NewClient(key).Hosters(ctx)
	if err != nil {
		log.Printf("TorBox hoster list unavailable (%v); hoster routing degraded", err)
		return nil
	}
	set := map[string]bool{}
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(d, "www.")))
		if d != "" {
			set[d] = true
		}
	}
	for _, h := range hs {
		add(h.Domain)
		for _, d := range h.Domains {
			add(d)
		}
	}
	return set
}
