package accounts

// Kind is the shape of secret a service needs: an API key, or a username and
// password. It decides which form the accounts page renders and which
// Credential fields the service uses.
type Kind string

const (
	KindAPIKey           Kind = "apiKey"
	KindUsernamePassword Kind = "usernamePassword"
)

// Group is the section of the accounts page a service belongs to.
type Group string

const (
	// GroupDebrid is a service whose API key unlocks supported hoster links.
	// Configuring one changes routing, not just whether a login works.
	GroupDebrid Group = "debrid"
	// GroupHoster is a direct premium login to one file hoster. The accounts
	// page builds that section from internal/hosterauth, not from Catalogue.
	GroupHoster Group = "hoster"
	// GroupCaptchaSolver is a paid captcha-solving API, tried before a person
	// is asked. Its key unlocks no link, and it is configured on the captcha
	// settings page rather than the accounts page.
	GroupCaptchaSolver Group = "captchaSolver"
	// GroupRemoteServer is a login to a server the user owns, such as a
	// seedbox over FTP or a NAS over SFTP (see internal/resolver/remotefs).
	// The account id is the server's hostname: remotefs looks a link's host up
	// by it, so a credential stored under any other name is never used.
	GroupRemoteServer Group = "remoteServer"
)

// Service is one entry in the catalogue of credentials KnightLoader can
// store. Both the settings API and the accounts page read from Catalogue.
type Service struct {
	// ID matches the resolver id used for routing and the key the credential
	// is stored under.
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  Kind   `json:"kind"`
	Group Group  `json:"group"`
	// Env is the environment variable that can supply this credential instead
	// of the store, and wins over a stored value. Empty when there is none.
	Env string `json:"env,omitempty"`
	// WhereURL is the page on the service's own site where a user finds or
	// generates the credential.
	WhereURL string `json:"whereUrl"`
	// Domain is the service's own domain where WhereURL is on a subdomain, so
	// JD's entry for it is recognised as this service.
	Domain string `json:"domain,omitempty"`
	// UserLabel and PassLabel name the two fields of a KindUsernamePassword
	// service whose fields are not a website login, such as an API user and an
	// API key. Empty means username and password.
	UserLabel CredentialField `json:"userLabel,omitempty"`
	PassLabel CredentialField `json:"passLabel,omitempty"`
}

// CredentialField names one field of a two-part credential for the form.
type CredentialField string

const (
	FieldAPIUser    CredentialField = "apiUser"
	FieldAPIKey     CredentialField = "apiKey"
	FieldCustomerID CredentialField = "customerId"
	FieldEmail      CredentialField = "email"
)

// Catalogue is every service KnightLoader can store a credential for, in
// display order. Each WhereURL is taken from the vendor's own API
// documentation or account pages.
var Catalogue = []Service{
	{ID: "torbox", Label: "TorBox", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_TORBOX", WhereURL: "https://torbox.app/settings"},
	{ID: "alldebrid", Label: "AllDebrid", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_ALLDEBRID", WhereURL: "https://alldebrid.com/apikeys"},
	{ID: "realdebrid", Label: "Real-Debrid", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_REALDEBRID", WhereURL: "https://real-debrid.com/apitoken"},
	{ID: "debridlink", Label: "Debrid-Link", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_DEBRIDLINK", WhereURL: "https://debrid-link.com/webapp/apikey"},
	{ID: "premiumize", Label: "Premiumize.me", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_PREMIUMIZE", WhereURL: "https://www.premiumize.me/account"},
	// Linksnappy has no API key; it logs in with the website credentials.
	{ID: "linksnappy", Label: "Linksnappy", Kind: KindUsernamePassword, Group: GroupDebrid, Env: "", WhereURL: "https://linksnappy.com/myaccount"},
	{ID: "offcloud", Label: "Offcloud", Kind: KindAPIKey, Group: GroupDebrid, Env: "KL_OFFCLOUD", WhereURL: "https://offcloud.com/#/account"},
	// The smaller multihosters. They take the store only, since a variable each
	// would crowd the environment for services few people use.
	{ID: "bestdebrid", Label: "BestDebrid", Kind: KindAPIKey, Group: GroupDebrid, WhereURL: "https://bestdebrid.com/profile"},
	{ID: "cocoleech", Label: "CocoLeech", Kind: KindAPIKey, Group: GroupDebrid, WhereURL: "https://members.cocoleech.com/settings", Domain: "cocoleech.com"},
	{ID: "cooldebrid", Label: "CoolDebrid", Kind: KindAPIKey, Group: GroupDebrid, WhereURL: "https://cooldebrid.com/api.html"},
	{ID: "debriditalia", Label: "DebridItalia", Kind: KindUsernamePassword, Group: GroupDebrid, WhereURL: "https://www.debriditalia.com/"},
	{ID: "deepbrid", Label: "Deepbrid", Kind: KindAPIKey, Group: GroupDebrid, WhereURL: "https://www.deepbrid.com/devices"},
	{ID: "fakirdebrid", Label: "FakirDebrid", Kind: KindAPIKey, Group: GroupDebrid, WhereURL: "https://fakirdebrid.net/api/login.php"},
	{ID: "megadebrid", Label: "Mega-Debrid", Kind: KindUsernamePassword, Group: GroupDebrid, WhereURL: "https://www.mega-debrid.eu/"},
	{ID: "multiup", Label: "MultiUp", Kind: KindUsernamePassword, Group: GroupDebrid, WhereURL: "https://multiup.io/en/login"},
	{ID: "neodebrid", Label: "NeoDebrid", Kind: KindUsernamePassword, Group: GroupDebrid, WhereURL: "https://neodebrid.com/login", UserLabel: FieldEmail},
	{ID: "proleech", Label: "ProLeech", Kind: KindUsernamePassword, Group: GroupDebrid, WhereURL: "https://proleech.link/v2/jdownloader", UserLabel: FieldAPIUser, PassLabel: FieldAPIKey},
	{ID: "rpnet", Label: "RPNet", Kind: KindUsernamePassword, Group: GroupDebrid, WhereURL: "https://premium.rpnet.biz/account", UserLabel: FieldCustomerID, PassLabel: FieldAPIKey},
	{ID: "zevera", Label: "Zevera", Kind: KindAPIKey, Group: GroupDebrid, WhereURL: "https://www.zevera.com/account"},
	{ID: "2captcha", Label: "2Captcha", Kind: KindAPIKey, Group: GroupCaptchaSolver, WhereURL: "https://2captcha.com/enterpage"},
	{ID: "anticaptcha", Label: "Anti-Captcha", Kind: KindAPIKey, Group: GroupCaptchaSolver, WhereURL: "https://anti-captcha.com/clients/settings/apisetup"},
	// There is no vendor page for a server the user owns, so WhereURL points
	// at the documentation of the hostname convention. There is no Env either,
	// since one variable could not say which of several servers it means.
	{ID: "remotefs", Label: "Own server (FTP, SFTP, WebDAV)", Kind: KindUsernamePassword, Group: GroupRemoteServer, WhereURL: "https://github.com/junkerderprovinz/knightloader#own-servers-ftp-sftp-webdav"},
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
