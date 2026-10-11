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
// trackers and indexers links come from.
var secretArgs = map[string]bool{
	"apikey": true, "api_key": true, "auth": true, "authkey": true, "pass": true,
	"passkey": true, "password": true, "pwd": true, "secret": true, "sid": true,
	"sig": true, "signature": true, "token": true, "torrent_pass": true,
}

// RedactURL is address s without a sign-in and with the value of every secret
// query argument replaced. In a magnet link the trackers keep their host
// alone, as in WithoutCredentials. Anything that is not an address comes back
// as it is.
func RedactURL(s string) string {
	s = userinfo.ReplaceAllString(s, "$1")
	head, query, ok := strings.Cut(s, "?")
	if !ok {
		return s
	}
	query, fragment, hasFragment := strings.Cut(query, "#")
	magnet := strings.HasPrefix(strings.ToLower(head), "magnet:")
	args := strings.Split(query, "&")
	for i, arg := range args {
		key, value, _ := strings.Cut(arg, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		name = strings.ToLower(name)
		switch {
		case secretArgs[name]:
			args[i] = key + "=redacted"
		case magnet && name == "tr":
			if tr, err := url.QueryUnescape(value); err == nil {
				args[i] = key + "=" + url.QueryEscape(HostOf(tr))
			}
		}
	}
	out := head + "?" + strings.Join(args, "&")
	if hasFragment {
		out += "#" + fragment
	}
	return out
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
