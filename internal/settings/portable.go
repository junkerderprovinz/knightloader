package settings

// A settings document somebody can carry to another box.
//
// This is not internal/backup's job, although the two look adjacent from the
// outside. backup.Build writes manifest.json, settings.json and
// knightloader.db, and a restore replaces all three at the next start-up, so it
// moves an install. This moves a configuration: settings.json alone, minus this
// box's own identity, and the receiving box takes only the parts somebody
// ticked, live, with no restart (see app.PatchSettings and
// afterSettingsChange).
//
// It lives in this package so that the next person to add a settings field
// meets the question "does my field travel, and does it carry a secret".
// NeverPortable below is that question written down.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"golang.org/x/mod/semver"
)

// PortableKind is what the file says it is, in its own first field.
//
// Checked on import before anything else: the two files this feature puts in a
// downloads folder are a .zip and a .json that both begin with "knightloader-",
// and a JSON document that happens to decode into PortableDoc while being
// something else (a task export, a rule set, half of a diagnostics bundle)
// would otherwise reach ApplyPatch as a patch full of keys nobody meant to
// send.
const PortableKind = "knightloader-settings"

// What the Secrets field says about the file it is in. It is a claim rather
// than a guarantee: Secretless below judges the document by what it holds, so
// that a hand-edited "included" cannot promise a password that is not there.
const (
	SecretsIncluded = "included"
	SecretsOmitted  = "omitted"
)

// PortableDoc is the file. The four fields above Settings are the four
// backup.Manifest carries: a file with no version in it cannot be refused by a
// build too old to read it, and one with no createdAt cannot be told apart from
// the other exports in the same folder.
type PortableDoc struct {
	Kind       string    `json:"kind"`
	Version    string    `json:"version"`
	Deployment string    `json:"deployment"`
	CreatedAt  time.Time `json:"createdAt"`
	// Secrets is SecretsIncluded or SecretsOmitted, so the interface can say
	// which kind of file the reader is looking at before they import it.
	Secrets string `json:"secrets"`
	// Settings is settings.json's own top-level keys, raw rather than a
	// Settings value: a document written by an older build carries keys this
	// build no longer has and misses keys it has gained, and decoding into the
	// struct here would drop the first group and invent defaults for the
	// second. Kept raw, the import can list both, see
	// routes_settings_transfer.go. migrate() runs only inside Load against the
	// raw bytes of settings.json, so a key renamed between builds cannot be
	// mapped here either.
	Settings map[string]json.RawMessage `json:"settings"`
}

// NeverPortable is this box's own identity: the keys that describe which
// instance this is rather than how it behaves. They are dropped on the way out
// and refused on the way in, so a hand-edited file cannot smuggle one back.
//
// instanceId is the one that does damage. It is minted once (newInstanceID,
// settings_identity.go) and never regenerated, and the relay keys its peer map
// by it (out[sib.InstanceID] = in in internal/relay, announced by
// routes_relay.go). Two boxes carrying the same id occupy one slot in one relay
// group: the second to connect displaces the first, and the symptom is a
// sibling that keeps going offline with nothing naming the cause.
//
// knownDomains is not configuration at all but an observation of how this
// instance has been reached (routes_remote.go writes it when a request arrives
// on a hostname), so carrying it seeds a new box with addresses that have never
// pointed at it.
//
// Returned as a fresh slice each call rather than exported as a package var, so
// a caller cannot append to it and widen what this promises.
func NeverPortable() []string {
	return []string{"instanceId", "knownDomains"}
}

// Portable marshals s into a document.
//
// includeSecrets false does two things. Redacted() covers the router password
// and every proxy password and nothing else, because ArchivePasswords is
// ordinary visible config on the Archives page (see settings_network.go), so an
// export that only called Redacted() would ship every archive password in clear
// text under a toggle saying it did not. routes_diagnostics.go clears it
// separately for the same reason.
//
// version and deployment are parameters rather than reads of
// internal/buildinfo, as backup.Stage takes runningVersion: this package is
// imported by backup and by the settings tests, and a dependency on the build
// stamp would put a linker variable in the middle of an otherwise pure package.
//
// It cannot promise that no secret remains. feed.Subscription.URL is the whole
// address including an API key in its query string, and no rule can tell a
// private tracker's key from a path segment; reconnect.Script and
// reconnect.Requests[].Body are, by that package's own admission, where a user
// is likely to hard-code a router password. This strips the three fields the
// app knows are secrets, and settings.transfer.withSecretsHint says the rest.
func Portable(s Settings, includeSecrets bool, version, deployment string, now time.Time) (PortableDoc, error) {
	secrets := SecretsIncluded
	if !includeSecrets {
		s = s.Redacted()
		s.ArchivePasswords = nil
		secrets = SecretsOmitted
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return PortableDoc{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return PortableDoc{}, err
	}
	// Dropped here rather than only refused on import, so the file never holds
	// them: an export is a thing people mail each other, and an id that is not
	// in the file cannot be pasted back in by somebody being helpful.
	for _, k := range NeverPortable() {
		delete(fields, k)
	}
	return PortableDoc{
		Kind:       PortableKind,
		Version:    version,
		Deployment: deployment,
		CreatedAt:  now.UTC(),
		Secrets:    secrets,
		Settings:   fields,
	}, nil
}

// PortableVersionError is the one refusal with a typed half, so the interface
// can say it in the reader's own language rather than showing an English
// sentence. Same shape as reconnect.ConfigProblem, which is why the API's error
// envelope already knows how to carry it.
type PortableVersionError struct {
	// DocVersion is what wrote the file; RunningVersion is what is being asked
	// to read it.
	DocVersion     string
	RunningVersion string
}

func (e *PortableVersionError) Error() string {
	return fmt.Sprintf(
		"this settings export was written by %s, which is newer than the %s this server is running; "+
			"upgrade the server first, then import", e.DocVersion, e.RunningVersion)
}

// Check refuses a document that is not one of ours, and one written by a newer
// build.
//
// The version guard goes one way, in the shape backup.Stage uses. An older
// document is the normal case, an export somebody kept for two years. Only a
// newer one is refused, because a key this build has never heard of is dropped
// by encoding/json without a word (see PortableDoc.Settings above), and a
// document from the future is where those keys come from in bulk.
//
// The comparison is skipped when either side is not valid semver, which is
// every untagged build ("dev"): comparing "dev" against "v1.4.0" as strings
// answers nonsense, and refusing to import into a development build would make
// the feature untestable.
//
// runningVersion is a parameter for the reason Portable's version is: this
// package does not import buildinfo.
func (d PortableDoc) Check(runningVersion string) error {
	if d.Kind != PortableKind {
		if strings.TrimSpace(d.Kind) == "" {
			return errors.New("this file does not say what it is, so it is not a KnightLoader settings export")
		}
		return fmt.Errorf("this file says it is %q, not a KnightLoader settings export (%q)", d.Kind, PortableKind)
	}
	if len(d.Settings) == 0 {
		return errors.New("this settings export carries no settings at all")
	}
	if semver.IsValid(d.Version) && semver.IsValid(runningVersion) &&
		semver.Compare(d.Version, runningVersion) > 0 {
		return &PortableVersionError{DocVersion: d.Version, RunningVersion: runningVersion}
	}
	return nil
}

// The codes Secretless answers in. Codes rather than sentences because the
// interface is translated into forty-two languages and the server has no idea
// which one the reader is looking at, the argument writeValidationError makes
// in routes_settings.go.
//
// The head of each code, up to the first dot, is the top-level settings key the
// secret lives under. That lets the import intersect this list with the keys it
// applied, without a second table mapping one to the other.
const (
	SecretlessReconnect        = "reconnect.password"
	SecretlessConnections      = "connections.password"
	SecretlessArchivePasswords = "archivePasswords"
)

// Secretless names the keys in d that arrive without their password.
//
// Derived from what the document holds, never from d.Secrets, which is a claim
// somebody can edit in a text editor. It is a machine-readable list rather than
// a hint because both failures it prevents are silent:
//
//   - a proxy row saves, enables and dials with a username and no password.
//     proxycfg.Merge returns next untouched when prev is empty, which is a
//     fresh box; Validate never looks at the password; clean() clears it only
//     when the username is empty; usable() is Enabled && Kind != KindNone. The
//     traffic the user was hiding goes out over their own connection.
//   - the router password is cleared and the error at three in the morning
//     names the wrong thing. WithSecretsFrom maps the "********" placeholder
//     back to prev.Password, which is "" on a fresh box; Config.Validate never
//     checks the password, so the save is accepted; the reconnect then posts an
//     empty password and comes back as ErrUnchanged, pointing the operator at
//     their router rather than at an empty field.
//
// archivePasswords is judged the same way, with one difference: an empty list
// cannot be told apart from a box that never had one. That is accepted, because
// the consequence is the same either way, this box has no archive passwords,
// and the sentence the interface builds from it stays true. Dropping the key
// from the document when secrets are omitted would hide the row from the
// preview and make the key impossible to take over on purpose.
func (d PortableDoc) Secretless() []string {
	out := []string{}

	if raw, ok := d.Settings["reconnect"]; ok {
		var c reconnect.Config
		if err := json.Unmarshal(raw, &c); err == nil {
			// The placeholder means there was one and it did not travel. An
			// empty password beside a username says the same from a hand-built
			// file or an older export. An empty password with no username is
			// not reported: MethodUPnP needs neither, and calling a correctly
			// configured UPnP reconnect incomplete would train people to ignore
			// this list.
			if c.Password == reconnect.RedactedPassword || (c.Password == "" && strings.TrimSpace(c.Username) != "") {
				out = append(out, SecretlessReconnect)
			}
		}
	}

	if raw, ok := d.Settings["connections"]; ok {
		var rows []proxycfg.Entry
		if err := json.Unmarshal(raw, &rows); err == nil {
			for _, e := range rows {
				// HasPassword is proxycfg's redaction marker: Redacted() sets it
				// true and blanks the password, so the pair reads as "this row
				// had one and it is not here". The username fallback covers a
				// hand-built file that never went through Redacted().
				if (e.HasPassword && e.Password == "") ||
					(e.Password == "" && strings.TrimSpace(e.Username) != "") {
					out = append(out, SecretlessConnections)
					break
				}
			}
		}
	}

	if raw, ok := d.Settings["archivePasswords"]; ok {
		var list []string
		if err := json.Unmarshal(raw, &list); err == nil && len(list) == 0 {
			out = append(out, SecretlessArchivePasswords)
		}
	}

	return out
}
