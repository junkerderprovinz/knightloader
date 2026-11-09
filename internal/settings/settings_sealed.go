package settings

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	kitnotify "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// FileName is the file in the data folder settings live in.
const FileName = "settings.json"

const sealPurpose = "settings"

// LoadWithKey is Load with an app key. Each secret is then sealed on its own
// inside settings.json, so the rest of the file stays readable and editable.
// A file written before the key is sealed on the spot. Without a key a sealed
// value is refused, since saving over it would lose it.
func LoadWithKey(dir string, key *appkey.Key) (*Store, error) {
	s := &Store{path: filepath.Join(dir, FileName), cur: Defaults(), appKey: key}
	b, err := os.ReadFile(s.path)
	if err == nil {
		// Unmarshal over defaults so new fields keep their default value.
		if err := json.Unmarshal(b, &s.cur); err != nil {
			s.keepUnreadable(b, err)
		}
		s.cur = migrate(b, s.cur)
	}
	plain, err := s.cur.mapSecrets(func(v string) (string, error) {
		if !appkey.IsSealed(v) {
			return v, nil
		}
		return key.OpenString(sealPurpose, v)
	})
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read: %w", s.path, err)
	}
	unsealed := false
	if key != nil {
		_, _ = s.cur.mapSecrets(func(v string) (string, error) {
			unsealed = unsealed || (v != "" && !appkey.IsSealed(v))
			return v, nil
		})
	}
	plain, migrated := migrateEventTargets(b, plain)
	s.cur = loadInstanceID(dir, plain)
	s.cur = sanitize(s.cur)
	if unsealed || migrated {
		b, err := s.encode(s.cur)
		if err == nil {
			err = writeFileAtomic(s.path, b)
		}
		if err != nil {
			return nil, fmt.Errorf("sealing the secrets in %s: %w", s.path, err)
		}
	}
	return s, nil
}

// encode is settings.json's content for n, with every secret sealed when
// there is an app key.
func (s *Store) encode(n Settings) ([]byte, error) {
	if s.appKey != nil {
		var err error
		n, err = n.mapSecrets(func(v string) (string, error) {
			if v == "" {
				return v, nil
			}
			return s.appKey.SealString(sealPurpose, v)
		})
		if err != nil {
			return nil, err
		}
	}
	return json.MarshalIndent(n, "", "  ")
}

// mapSecrets returns a copy of s with f applied to every value that is a
// password or a token, or a command line, address, header or body that may
// hold one: the values Redacted hides from clients, the archive passwords, the
// event targets' addresses and bodies (a Discord or Telegram token sits in the
// path), the feeds' addresses (an indexer's API key) and the reconnect
// requests and script. Scrubbed reads the same list, so a secret added here
// is sealed at rest and kept out of the diagnostics bundle at once.
func (s Settings) mapSecrets(f func(string) (string, error)) (Settings, error) {
	var firstErr error
	apply := func(v *string) {
		out, err := f(*v)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		*v = out
	}

	apply(&s.Reconnect.Password)
	apply(&s.Reconnect.Script)
	s.Reconnect.Requests = slices.Clone(s.Reconnect.Requests)
	for i := range s.Reconnect.Requests {
		q := &s.Reconnect.Requests[i]
		apply(&q.URL)
		q.Headers = maps.Clone(q.Headers)
		for k, v := range q.Headers {
			apply(&v)
			q.Headers[k] = v
		}
		apply(&q.Body)
	}
	apply(&s.Torrent.QBittorrent.Password)

	s.Feeds = slices.Clone(s.Feeds)
	for i := range s.Feeds {
		apply(&s.Feeds[i].URL)
	}

	s.ArchivePasswords = slices.Clone(s.ArchivePasswords)
	for i := range s.ArchivePasswords {
		apply(&s.ArchivePasswords[i])
	}

	s.Connections = slices.Clone(s.Connections)
	for i := range s.Connections {
		apply(&s.Connections[i].Password)
	}

	s.EventTargets = slices.Clone(s.EventTargets)
	for i := range s.EventTargets {
		s.EventTargets[i].Headers = mapValues(s.EventTargets[i].Headers, apply)
		apply(&s.EventTargets[i].URL)
		apply(&s.EventTargets[i].Body)
	}

	s.NotifyChannels = slices.Clone(s.NotifyChannels)
	for i := range s.NotifyChannels {
		channelSecrets(&s.NotifyChannels[i], apply)
	}

	apply(&s.IdleAction.Command.Program)
	s.IdleAction.Command.Args = slices.Clone(s.IdleAction.Command.Args)
	for i := range s.IdleAction.Command.Args {
		apply(&s.IdleAction.Command.Args[i])
	}

	s.EventPrograms = slices.Clone(s.EventPrograms)
	for i := range s.EventPrograms {
		c := &s.EventPrograms[i].Command
		apply(&c.Program)
		c.Args = slices.Clone(c.Args)
		for j := range c.Args {
			apply(&c.Args[j])
		}
	}
	return s, firstErr
}

// channelSecrets applies f to every value of c that is or may hold a secret.
// Each settings block is copied first, so the caller's channel keeps its own.
func channelSecrets(c *kitnotify.Channel, f func(*string)) {
	if w := c.Webhook; w != nil {
		cp := *w
		f(&cp.URL)
		cp.Headers = mapValues(cp.Headers, f)
		f(&cp.Body)
		c.Webhook = &cp
	}
	if d := c.Discord; d != nil {
		cp := *d
		f(&cp.URL)
		c.Discord = &cp
	}
	if sl := c.Slack; sl != nil {
		cp := *sl
		f(&cp.URL)
		c.Slack = &cp
	}
	if g := c.Gotify; g != nil {
		cp := *g
		f(&cp.URL)
		f(&cp.Token)
		c.Gotify = &cp
	}
	if n := c.Ntfy; n != nil {
		cp := *n
		f(&cp.URL)
		f(&cp.Token)
		c.Ntfy = &cp
	}
	if m := c.Matrix; m != nil {
		cp := *m
		f(&cp.Token)
		c.Matrix = &cp
	}
	if sm := c.SMTP; sm != nil {
		cp := *sm
		f(&cp.Password)
		c.SMTP = &cp
	}
	if a := c.Apprise; a != nil {
		cp := *a
		f(&cp.URL)
		cp.URLs = slices.Clone(cp.URLs)
		for i := range cp.URLs {
			f(&cp.URLs[i])
		}
		c.Apprise = &cp
	}
	if h := c.Healthchecks; h != nil {
		cp := *h
		f(&cp.URL)
		cp.ByKey = mapValues(cp.ByKey, f)
		c.Healthchecks = &cp
	}
}

// mapValues is a copy of m with f applied to every value.
func mapValues(m map[string]string, f func(*string)) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		f(&v)
		out[k] = v
	}
	return out
}

// Scrubbed is s for a document that leaves the owner's hands, such as the
// diagnostics bundle: every value mapSecrets reaches is replaced by
// reconnect.RedactedPassword, and an address keeps only its scheme and host,
// which says what is configured without the token in its path or query.
func (s Settings) Scrubbed() Settings {
	out, _ := s.mapSecrets(func(v string) (string, error) {
		if v == "" {
			return v, nil
		}
		if host := core.HostOf(v); host != "" {
			return host, nil
		}
		return reconnect.RedactedPassword, nil
	})
	return out
}
