package api

// The group an instance left at a password reset. Its phones, browsers and
// other instances still hold the old words and would only see this instance
// go quiet. For retiredFor after the reset the instance stays on the old
// group's relay key and answers every call that opens under the old frame key
// with a sealed 410 "reset", which the app and the extension turn into "pair
// again with the new words". It keeps the two derived keys and never the old
// phrase, and the answer runs no route.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/relay"
)

// retiredAccountService is the credential-store service the retired group's
// keys are sealed under.
const retiredAccountService = "relay-retired"

// retiredFor is how long an instance answers the group it left.
const retiredFor = 30 * 24 * time.Hour

// retiredBody is the answer's body. Like "removed", the app and the extension
// read it as it stands.
const retiredBody = "reset"

// retiredGroup is what is kept of the group left at a reset.
type retiredGroup struct {
	URL      string    `json:"url"`
	Key      string    `json:"key"`
	FrameKey string    `json:"frameKey"`
	Until    time.Time `json:"until"`
}

// retireGroup keeps the keys of the group this instance dials now, before a
// reset leaves it.
func retireGroup(a *app.App, now time.Time) error {
	url, key, frameKey := relayTarget(a)
	if url == "" || key == "" {
		return nil
	}
	b, err := json.Marshal(retiredGroup{URL: url, Key: key, FrameKey: hex.EncodeToString(frameKey), Until: now.Add(retiredFor)})
	if err != nil {
		return err
	}
	return a.Accounts.Set(retiredAccountService, string(b))
}

// retiredOf reads the kept group, and drops it once its time is up.
func retiredOf(a *app.App, now time.Time) (retiredGroup, bool) {
	stored, err := a.Accounts.Get(retiredAccountService)
	if err != nil || stored == "" {
		return retiredGroup{}, false
	}
	var g retiredGroup
	if json.Unmarshal([]byte(stored), &g) != nil || !now.Before(g.Until) {
		if err := a.Accounts.Set(retiredAccountService, ""); err != nil {
			log.Printf("relay: could not forget the group left at the reset: %v", err)
		}
		return retiredGroup{}, false
	}
	return g, true
}

// applyRetired connects to the kept group's relay, or drops that connection
// when there is none or its time is up.
func applyRetired(a *app.App) {
	g, ok := retiredOf(a, time.Now())
	frameKey, err := hex.DecodeString(g.FrameKey)
	if !ok || err != nil {
		a.Federation.SetRetired(nil, time.Time{})
		return
	}
	c, err := relay.NewClient(relay.ClientOptions{
		URL:      g.URL,
		Key:      g.Key,
		FrameKey: frameKey,
		Self: relay.Announce{
			InstanceID: a.Settings.Get().InstanceID,
			Name:       instanceDisplayName(a),
			Deployment: buildinfo.Deployment,
		},
		Serve: retiredAnswer(g.Until),
	})
	if err != nil {
		log.Printf("relay: could not stay on the group left at the reset: %v", err)
		a.Federation.SetRetired(nil, time.Time{})
		return
	}
	c.Start()
	a.Federation.SetRetired(c, g.Until)
}

// retiredAnswer answers every call until until with 410 "reset", and none
// after.
func retiredAnswer(until time.Time) relay.ProxyHandler {
	return func(context.Context, relay.ProxyCall) (int, []byte) {
		if !time.Now().Before(until) {
			return relay.StatusDrop, nil
		}
		return http.StatusGone, []byte(retiredBody)
	}
}
