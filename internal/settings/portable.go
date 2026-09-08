package settings

// A settings document somebody can carry to another box.
//
// This is deliberately NOT internal/backup's job, and the difference is worth
// spelling out because the two look adjacent from the outside. backup.Build
// writes manifest.json + settings.json + knightloader.db and a restore replaces
// all three at the next start-up, so it moves an install. This moves a
// CONFIGURATION: settings.json alone, minus this box's own identity, into a file
// small enough to keep for two years, and the receiving box takes only the parts
// somebody ticked, live, with no restart (see app.PatchSettings and
// afterSettingsChange - every runtime effect a saved settings page has already
// runs on a partial save).
//
// It lives in this package rather than in internal/api for one reason: the next
// person to add a settings field will open this directory, and the question
// "does my field travel, and does it carry a secret" has to be in front of them
// when they do. NeverPortable below is that question written down.

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
// Checked on import before anything else, because the two files this feature
// puts in a downloads folder are a .zip and a .json that both begin with
// "knightloader-" - and a JSON document that happens to decode into PortableDoc
// while being something else entirely (a task export, a rule set, half of a
// diagnostics bundle) would otherwise reach ApplyPatch as a patch full of keys
// nobody meant to send.
const PortableKind = "knightloader-settings"

// What the Secrets field says about the file it is in. It is a claim, not a
// guarantee: Secretless below judges the document by what it actually holds,
// precisely so a hand-edited "included" cannot promise a password that is not
// there. See its own comment.
const (
	SecretsIncluded = "included"
	SecretsOmitted  = "omitted"
)

// PortableDoc is the file. The four fields above Settings are the same four
// backup.Manifest carries and for the same reasons - a file with no version in
// it cannot be refused by a build too old to read it, and a file with no
// createdAt cannot be told apart from the other three exports in the same
// folder.
type PortableDoc struct {
	Kind       string    `json:"kind"`
	Version    string    `json:"version"`
	Deployment string    `json:"deployment"`
	CreatedAt  time.Time `json:"createdAt"`
	// Secrets is SecretsIncluded or SecretsOmitted, for the interface to say
	// which kind of file the reader is looking at BEFORE they import it.
	Secrets string `json:"secrets"`
	// Settings is settings.json's own top-level keys, raw. Raw and not a
	// Settings value on purpose: a document written by an older build carries
	// keys this build no longer has and misses keys it has gained, and decoding
	// into the struct here would silently drop the first group and silently
	// invent defaults for the second. Kept raw, the import can list both to the
	// person doing it - see the unknown list in routes_settings_transfer.go and
	// trap 8 in this feature's spec (migrate() runs only inside Load, against
	// the raw bytes of settings.json, never against a patch body, so a key that
	// was RENAMED between builds cannot be mapped here either).
	Settings map[string]json.RawMessage `json:"settings"`
}

// NeverPortable is this box's own identity: the keys that describe WHICH
// instance this is rather than how it behaves. They are dropped on the way out
// and refused on the way in, so a hand-edited file cannot smuggle one back.
//
// instanceId is the one that does real damage. It is minted once
// (newInstanceID, settings_identity.go) and never regenerated, and the relay
// keys its peer map by it - internal/relay's own group map is
// out[sib.InstanceID] = in, and routes_relay.go announces it. Two boxes
// carrying the same id therefore occupy one slot in one relay group: the second
// to connect displaces the first, and the symptom is a sibling that "keeps
// going offline" with nothing anywhere naming the cause. Note that the FULL
// backup does clone it, because it copies settings.json wholesale; that is a
// bug in the sibling feature, reported rather than reproduced here.
//
// knownDomains is not configuration at all. It is an observation of how this
// instance has actually been reached (routes_remote.go writes it when a request
// arrives on a hostname), so carrying it seeds a new box with a list of
// addresses that have never pointed at it and never will.
//
// Returned as a fresh slice each call rather than exported as a package var,
// so a caller cannot append to it and quietly widen what this promises.
func NeverPortable() []string {
	return []string{"instanceId", "knownDomains"}
}

// Portable marshals s into a document.
//
// includeSecrets false does TWO things, and the second one is the one that gets
// forgotten: Redacted() covers the router password and every proxy password and
// nothing else, deliberately (see its own doc comment in settings_network.go -
// ArchivePasswords is ordinary visible config on the Archives page, where a user
// is editing their own passwords and has to see them). An export that only
// called Redacted() would ship every archive password in clear text under a
// toggle that says it did not. routes_diagnostics.go:69 already clears it
// separately for the same reason, and this is the second caller of that same
// rule.
//
// version and deployment are parameters rather than reads of internal/buildinfo,
// exactly as backup.Stage takes runningVersion: this package is imported BY
// backup and by the settings tests, and a dependency on the build stamp here
// would put a linker variable in the middle of a package that is otherwise pure.
//
// What it cannot promise, and what the interface has to say out loud: a secret
// can still be sitting in a field nothing here recognises as one.
// feed.Subscription.URL is the whole address including an API key in its query
// string, and no rule can tell a private tracker's key from a path segment;
// reconnect.Script and reconnect.Requests[].Body are, by that package's own
// admission (config.go's String method), "the one place a user is likely to
// hard-code a router password". So this strips the three fields the app KNOWS
// are secrets, and settings.transfer.withSecretsHint says the rest.
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
	// Dropped here rather than only refused on import, so the file itself never
	// holds them: an export is a thing people mail each other, and an id that is
	// not in the file cannot be pasted back in by somebody being helpful.
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

// PortableVersionError is the one refusal that has a typed half, so the
// interface can say it in the reader's own language rather than showing an
// English sentence. Same shape and same reasoning as reconnect.ConfigProblem,
// which is why the API's error envelope already knows how to carry it.
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
// The version guard goes ONE way, verbatim in the shape backup.Stage already
// uses (backup.go's own semver comparison), and that is not an oversight: an
// OLDER document is the normal case and the whole point of an export you kept
// for two years. Only a newer one is refused, because a key this build has never
// heard of is dropped by encoding/json without a word (see PortableDoc.Settings
// above), and a document from the future is where those keys come from in bulk.
//
// The comparison is skipped when either side is not valid semver, which is every
// untagged build ("dev"). Refusing to import into a development build would make
// the feature untestable by the person writing it, and comparing "dev" against
// "v1.4.0" as strings would answer nonsense.
//
// runningVersion is a parameter for the same reason Portable's version is: this
// package must not import buildinfo.
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

// The codes Secretless answers in. They are codes and not sentences because the
// interface is translated into forty-two languages and the server has no idea
// which one the reader is looking at - the same argument
// writeValidationError makes in routes_settings.go.
//
// The head of each code, up to the first dot, is deliberately the TOP-LEVEL
// settings key the secret lives under ("reconnect", "connections",
// "archivePasswords"). That is what lets the import intersect this list with
// the keys it actually applied without a second table mapping one to the other.
const (
	SecretlessReconnect        = "reconnect.password"
	SecretlessConnections      = "connections.password"
	SecretlessArchivePasswords = "archivePasswords"
)

// Secretless names the keys in d that arrive without their password.
//
// Derived from what the document HOLDS, never from d.Secrets. A file is a file:
// its secrets field is a claim somebody can edit in a text editor, and the two
// failures this list exists to prevent are both silent enough already.
//
// Those two failures, because they are why this is a machine-readable list and
// not a hint somebody might have read:
//
//   - a proxy row saves, enables and dials with a username and NO password.
//     proxycfg.Merge returns next untouched when prev is empty, which is exactly
//     a fresh box; Validate never looks at the password; clean() only clears it
//     when the username is empty; usable() is Enabled && Kind != KindNone. So
//     the traffic the user was hiding goes out over their own connection, which
//     is the failure sanitizeNetwork's own comment names.
//   - the router password is silently cleared and the 3am error names the wrong
//     thing. WithSecretsFrom maps the "********" placeholder back to
//     prev.Password, which is "" on a fresh box; Config.Validate never checks
//     the password, so the save is accepted; the nightly reconnect then posts an
//     empty password and comes back as ErrUnchanged, "the address did not
//     change", pointing the operator at their router rather than at an empty
//     field.
//
// archivePasswords is judged the same way and reads slightly differently: an
// empty list cannot be told apart from a box that never had one. That is
// accepted rather than worked around, because the CONSEQUENCE is identical in
// both cases - after taking this key over, this box has no archive passwords -
// and the sentence the interface builds from it is true either way. The
// alternative considered and rejected was dropping the key from the document
// entirely when secrets are omitted: that hides the row from the preview, so the
// person moving boxes never sees that their archive passwords are not coming,
// and it also makes the key impossible to take over deliberately.
func (d PortableDoc) Secretless() []string {
	out := []string{}

	if raw, ok := d.Settings["reconnect"]; ok {
		var c reconnect.Config
		if err := json.Unmarshal(raw, &c); err == nil {
			// The placeholder means "there was one and it did not travel". An
			// empty password beside a username means the same thing arrived by a
			// different route (a hand-built file, an older export). An empty
			// password with no username is not reported: MethodUPnP needs
			// neither, and calling a correctly configured UPnP reconnect
			// incomplete would train people to ignore this list.
			if c.Password == reconnect.RedactedPassword || (c.Password == "" && strings.TrimSpace(c.Username) != "") {
				out = append(out, SecretlessReconnect)
			}
		}
	}

	if raw, ok := d.Settings["connections"]; ok {
		var rows []proxycfg.Entry
		if err := json.Unmarshal(raw, &rows); err == nil {
			for _, e := range rows {
				// HasPassword is proxycfg's own redaction marker: Redacted()
				// sets it true and blanks the password, so the pair is an exact
				// reading of "this row had one and it is not here". The
				// username fallback covers a hand-built file that never went
				// through Redacted().
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
