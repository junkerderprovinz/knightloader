package accounts

// Kind is what shape of secret a service needs: one field for an API key, or
// two for a username and a password. It decides which form the accounts page
// renders for a given service, and which fields of a Credential (accounts.go)
// that service actually uses - Credential itself carries all three fields
// unconditionally and enforces nothing, so Kind is the only place that
// distinction is recorded at all.
type Kind string

const (
	// KindAPIKey is a single token issued by the service - Credential.APIKey.
	KindAPIKey Kind = "apiKey"
	// KindUsernamePassword is a login the same as any other account on that
	// site - Credential.Username and Credential.Password.
	KindUsernamePassword Kind = "usernamePassword"
)

// Group is which section of the accounts page a service belongs to.
type Group string

const (
	// GroupDebrid is a one-unlock-call service: a single API key turns a
	// supported hoster's link into a direct URL the download engine fetches.
	// rewireBackends (app_accounts.go) treats every GroupDebrid credential as
	// a routing decision - configuring one changes which links resolve
	// through it, not just whether a login works.
	GroupDebrid Group = "debrid"
	// GroupHoster is a direct login to one file hoster: the hoster's own
	// premium username and password, the way JDownloader's account manager
	// holds them, with no routing decision attached to having one.
	GroupHoster Group = "hoster"
	// GroupCaptchaSolver is an automatic captcha-solving service: a paid API
	// that turns a captcha image into an answer, tried in a configured order
	// (settings.Settings.CaptchaSolverOrder) before KnightLoader ever shows
	// a human the prompt modal - see internal/captcha's solver_2captcha.go
	// and solver_anticaptcha.go.
	//
	// Neither existing group fits. Not GroupDebrid: rewireBackends
	// (app_accounts.go) treats every GroupDebrid credential as a fact about
	// ROUTING - which hoster links resolve through it - and a solver key
	// unlocks no link and carries no routing weight at all. Not GroupHoster
	// either, and not only for the semantic reason (a solver key logs into
	// no file-hosting site): GroupHoster's page section is not even built
	// from the catalogue - Accounts.tsx renders it via HosterLoginSection,
	// which reads internal/hosterauth's own host-keyed list and never
	// consults Catalogue at all, so a GroupHoster entry here would render
	// nowhere. A service in neither of the two groups Accounts.tsx does
	// filter for (Group == "debrid") simply never appears on that page -
	// which is correct for this group: a solver credential is configured on
	// its own settings sub-page (web/src/pages/settings/Captcha.tsx)
	// instead, beside the enabled/order settings it has nothing to do with
	// otherwise sharing a page with.
	GroupCaptchaSolver Group = "captchaSolver"
)

// Service is one entry in the catalogue of credentials KnightLoader can
// store: what it is called, what shape of secret it needs, which section of
// the accounts page it belongs to, and where to find or generate that secret
// on the service's own site.
//
// This is the single source both the settings API and the accounts page read
// from. It replaces the hardcoded knownServices slice that used to live in
// app_accounts.go and the hand-synced SERVICES array that used to live in
// web/src/pages/Accounts.tsx - two lists, hand-kept in step, with the
// frontend's "where to get your key" strings never routed through
// translation at all. Adding a service now is one entry here.
type Service struct {
	// ID matches the resolver id used for routing and the key under which
	// the credential is stored: accounts.Store.Get(id), .Set(id, ...).
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  Kind   `json:"kind"`
	Group Group  `json:"group"`
	// Env is the environment variable that can supply this credential
	// instead of the encrypted store - set by a container, and always
	// preferred over a stored value (see credential() in app_accounts.go).
	// Empty for a service with no such override.
	Env string `json:"env,omitempty"`
	// WhereURL is the page on the service's own site where a user finds or
	// generates this credential, meant to be rendered as a link - not the
	// hand-typed, never-translated breadcrumb sentence
	// ("torbox.app → Settings → API") this replaces.
	WhereURL string `json:"whereUrl"`
}

// Catalogue is every service KnightLoader knows how to store a credential
// for, in display order. Each WhereURL was checked against the service's own
// documentation or account pages, the same sources internal/resolver/torbox
// and internal/resolver/debrid were verified against: api-docs.torbox.app,
// docs.alldebrid.com, and Real-Debrid's own account/API pages. The two
// solver entries below were verified the same way against 2Captcha's and
// Anti-Captcha's own docs (see internal/captcha/solver_2captcha.go and
// solver_anticaptcha.go's package comments for the exact pages and dates):
// 2captcha.com/api-docs/quick-start names https://2captcha.com/enterpage as
// the dashboard that shows the API key, and
// https://anti-captcha.com/clients/settings/apisetup is Anti-Captcha's own
// "API Setup" account-key page.
var Catalogue = []Service{
	{ID: "torbox", Label: "TorBox", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_TORBOX", WhereURL: "https://torbox.app/settings"},
	{ID: "alldebrid", Label: "AllDebrid", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_ALLDEBRID", WhereURL: "https://alldebrid.com/apikeys"},
	{ID: "realdebrid", Label: "Real-Debrid", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_REALDEBRID", WhereURL: "https://real-debrid.com/apitoken"},
	// No Env override for either solver: unlike the three debrid services
	// above, no container build ships a well-known KL_ environment variable
	// for a captcha-solver key. Env is optional (see Service.Env's own doc
	// comment) - a bare zero value, not an omission that needs a workaround.
	{ID: "2captcha", Label: "2Captcha", Kind: KindAPIKey, Group: GroupCaptchaSolver, WhereURL: "https://2captcha.com/enterpage"},
	{ID: "anticaptcha", Label: "Anti-Captcha", Kind: KindAPIKey, Group: GroupCaptchaSolver, WhereURL: "https://anti-captcha.com/clients/settings/apisetup"},
}

// Lookup returns the catalogue entry for a service id, or false if
// KnightLoader has no such service.
func Lookup(id string) (Service, bool) {
	for _, svc := range Catalogue {
		if svc.ID == id {
			return svc, true
		}
	}
	return Service{}, false
}
