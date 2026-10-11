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

// WithoutCredentials is Redacted with its type.
func (t Task) WithoutCredentials() Task {
	t.Password = ""
	t.DownloadPassword = ""
	t.URL = RedactURL(t.URL)
	t.Source = RedactURL(t.Source)
	if t.Trackers != nil {
		trackers := make([]string, len(t.Trackers))
		for i, tr := range t.Trackers {
			trackers[i] = trackerHost(tr)
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
				args[i] = key + "=" + url.QueryEscape(trackerHost(tr))
			}
		}
	}
	out := head + "?" + strings.Join(args, "&")
	if hasFragment {
		out += "#" + fragment
	}
	return out
}

// trackerHost is a tracker's address cut to its scheme and host.
func trackerHost(tr string) string {
	u, err := url.Parse(strings.TrimSpace(tr))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
