package core

import (
	"net/url"
	"regexp"
	"strings"
)

// Redacted is t without the credentials it carries, as a caller that may not
// see them gets it: a member of the phrase group or an API token without the
// admin right. The passwords go, and the addresses keep what names the file
// but lose a sign-in and the values of secret query arguments. A tracker keeps
// only its host, since a private tracker puts the member's passkey in the path.
func (t Task) Redacted() any { return t.WithoutCredentials() }

// WithoutCredentials is Redacted with its type. Every field that can hold an
// address goes through RedactText, the name among them, since a link that
// names no file keeps its address as the name.
func (t Task) WithoutCredentials() Task {
	t.Password = ""
	t.DownloadPassword = ""
	t.URL = RedactURL(t.URL)
	t.Source = RedactURL(t.Source)
	for _, s := range []*string{&t.Name, &t.Package, &t.Filename, &t.Comment, &t.Error, &t.SkipReason, &t.Note} {
		*s = RedactText(*s)
	}
	t.ErrorParams = redactValues(t.ErrorParams)
	t.RejectParams = redactValues(t.RejectParams)
	t.SkipParams = redactValues(t.SkipParams)
	if t.Trackers != nil {
		trackers := make([]string, len(t.Trackers))
		for i, tr := range t.Trackers {
			trackers[i] = HostOf(tr)
		}
		t.Trackers = trackers
	}
	return t
}

// userinfo matches the sign-in of an address, up to the last @ before the
// path, so a password holding an @ or a quote goes whole.
var userinfo = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*://)[^/?#]*@`)

// secretArgs are query arguments whose value is a credential on the hosters,
// trackers, indexers and storage services links come from: "r" is a Newznab
// feed's API key, "tp" a tracker's passkey, and the X-Amz ones sign an S3
// address.
var secretArgs = map[string]bool{
	"access_token": true, "apikey": true, "api_key": true, "auth": true, "auth_token": true,
	"authkey": true, "key": true, "pass": true, "passkey": true, "password": true,
	"pwd": true, "r": true, "secret": true, "sid": true, "sig": true, "signature": true,
	"token": true, "torrent_pass": true, "tp": true,
	"x-amz-credential": true, "x-amz-security-token": true, "x-amz-signature": true,
}

// pathSecrets are services that put a credential in the path of an address:
// Telegram's bot token, a Discord webhook's token and a TorrentLeech feed's
// or download link's key. The path segment after the match is replaced.
var pathSecrets = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*://(?:api\.telegram\.org/bot|(?:[a-z0-9-]+\.)?discord(?:app)?\.com/api(?:/v\d+)?/webhooks/[^/?#]+/|rss24h\.torrentleech\.org/|rss\.torrentleech\.org/|(?:www\.)?torrentleech\.org/rss/download/[^/?#]+/))[^/?#]+`)

// slackHook matches a Slack webhook, whose whole path after the kind is the
// credential.
var slackHook = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*://hooks\.slack\.com/(?:services|workflows|triggers)/)[^?#]+`)

// magnetAddresses are the arguments of a magnet link that name an address a
// client fetches from: trackers, the exact and the acceptable source, and web
// seeds. A private tracker or a seedbox puts a passkey in their path.
var magnetAddresses = map[string]bool{"tr": true, "xs": true, "as": true, "ws": true}

// RedactURL is address s without a sign-in and with the value of every secret
// query argument, and a token a known service puts in the path, replaced. In a
// magnet link the addresses keep their host alone, as trackers do in
// WithoutCredentials. An argument that is itself an address, as a redirect or
// a proxy takes one, is redacted the same way. Arguments may be separated by
// "&" or ";". Anything that is not an address comes back as it is.
func RedactURL(s string) string {
	s = userinfo.ReplaceAllString(s, "$1")
	s = pathSecrets.ReplaceAllString(s, "${1}redacted")
	s = slackHook.ReplaceAllString(s, "${1}redacted")
	head, query, ok := strings.Cut(s, "?")
	if !ok {
		return s
	}
	query, fragment, hasFragment := strings.Cut(query, "#")
	magnet := strings.HasPrefix(strings.ToLower(head), "magnet:")
	var out strings.Builder
	out.WriteString(head + "?")
	for query != "" {
		end := strings.IndexAny(query, "&;")
		arg, sep := query, ""
		if end >= 0 {
			arg, sep, query = query[:end], query[end:end+1], query[end+1:]
		} else {
			query = ""
		}
		key, value, _ := strings.Cut(arg, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		name = strings.ToLower(name)
		kind, _, _ := strings.Cut(name, ".")
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			decoded = value
		}
		switch {
		case secretArgs[name]:
			arg = key + "=redacted"
		case magnet && magnetAddresses[kind]:
			arg = key + "=" + url.QueryEscape(HostOf(decoded))
		default:
			if inner, rounds := nestedAddress(decoded); rounds > 0 {
				if redacted := RedactURL(inner); redacted != inner {
					for range rounds {
						redacted = url.QueryEscape(redacted)
					}
					arg = key + "=" + redacted
				}
			}
		}
		out.WriteString(arg + sep)
	}
	if hasFragment {
		out.WriteString("#" + fragment)
	}
	return out.String()
}

// nestedAddress unescapes an argument once escaped, as far as it was escaped
// again and at most three times more, and reports how many escapes it took to
// reach an address, or 0 when it holds none.
func nestedAddress(once string) (string, int) {
	s := once
	for rounds := 1; rounds <= 4; rounds++ {
		if strings.Contains(s, "://") || strings.HasPrefix(strings.ToLower(s), "magnet:") {
			return s, rounds
		}
		next, err := url.QueryUnescape(s)
		if err != nil || next == s {
			return "", 0
		}
		s = next
	}
	return "", 0
}

// addressInText finds an address in free text. It runs to the next blank,
// because a password in a sign-in may hold a quote or a bracket; RedactText
// gives back the punctuation a sentence puts after an address.
var addressInText = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://|magnet:\?)\S+`)

// RedactText is s with RedactURL applied to every address in it, for a name,
// an error or a log line that may quote a link.
func RedactText(s string) string {
	if !strings.Contains(s, ":") {
		return s
	}
	return addressInText.ReplaceAllStringFunc(s, func(addr string) string {
		bare := strings.TrimRight(addr, `"'()<>[]{},.;:!?`)
		return RedactURL(bare) + addr[len(bare):]
	})
}

func redactValues(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = RedactText(v)
	}
	return out
}

// HostOf is address s cut to its scheme and host, or empty when s is not an
// address with a host. It is all a tracker keeps, and all of an address that
// may carry a token in its path.
func HostOf(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
