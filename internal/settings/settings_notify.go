package settings

// The channels (Settings.NotifyChannels): the services this instance tells
// when something happens. Their format and checks are parleyport kit's, so
// every app of a fortress stores them alike.

import (
	"encoding/json"
	"log"

	"github.com/junkerderprovinz/parleyport/kit/notify/legacy"
)

// LegacyEventTarget is an event target as builds before the channels stored
// it: one HTTP request with its method, headers, body template and triggers.
type LegacyEventTarget struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Enabled        bool              `json:"enabled"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	Triggers       []string          `json:"triggers,omitempty"`
	Attempts       int               `json:"attempts"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
}

// migrateEventTargets turns the old event targets into channels, once: raw is
// the file as read, and a notifyChannels key in it means the move happened.
// It runs on the unsealed settings, since a sealed address is no address.
// It reports whether anything was moved, so the caller writes the file.
func migrateEventTargets(raw []byte, n Settings) (Settings, bool) {
	if len(n.EventTargets) == 0 {
		return n, false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return n, false
	}
	if _, done := doc["notifyChannels"]; done {
		return n, false
	}
	old, err := json.Marshal(n.EventTargets)
	if err != nil {
		return n, false
	}
	channels, err := legacy.FromTargets(old)
	if err != nil {
		log.Printf("settings: the event targets could not be moved to channels: %v", err)
		return n, false
	}
	n.NotifyChannels = channels
	return n, true
}
