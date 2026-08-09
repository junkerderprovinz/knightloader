package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/rules"
	"github.com/junkerderprovinz/knightloader/internal/schedule"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// settingsWith is the settings a fresh install has, plus whatever the test cares
// about, so a PUT never accidentally wipes a field it was not about.
func settingsWith(mutate func(*settings.Settings)) settings.Settings {
	s := settings.Defaults()
	mutate(&s)
	return s
}

// putSettings sends a settings object and returns the status and the decoded
// body, which for anything but 200 is the plain-text error.
func putSettings(t *testing.T, url string, s settings.Settings) (int, map[string]any, string) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url+"/api/settings", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		_, _ = msg.ReadFrom(resp.Body)
		return resp.StatusCode, nil, msg.String()
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body, ""
}

// getSettings reads the settings the way the form does.
func getSettings(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// problemCount digs the reported problems for one rule list out of a settings
// response.
func problemCount(t *testing.T, body map[string]any, list string) int {
	t.Helper()
	problems, ok := body["problems"].(map[string]any)
	if !ok {
		t.Fatalf("the settings response carries no problems object: %v", body["problems"])
	}
	got, ok := problems[list].([]any)
	if !ok {
		t.Fatalf("problems.%s is %v, want a list", list, problems[list])
	}
	return len(got)
}

// TestRuleProblemsReachTheFormOnSaveAndOnLoad is the point of keeping a broken
// rule on disk. Storing it and never mentioning it again would be worse than
// dropping it: the user sees their rule in the list, believes it is filtering,
// and finds out otherwise from the download folder.
func TestRuleProblemsReachTheFormOnSaveAndOnLoad(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	broken := settingsWith(func(s *settings.Settings) {
		s.LinkFilter = rules.Set{Rules: []rules.Rule{{
			Name: "half a pattern",
			Conditions: []rules.Condition{
				{Field: rules.FieldFilename, Op: rules.OpMatches, Value: "(unclosed"},
			},
			Action: rules.Action{Reject: true},
		}}}
		s.Packagizer = rules.Set{Rules: []rules.Rule{{
			Name:       "impossible range",
			Conditions: []rules.Condition{{Field: rules.FieldFilesize, Op: rules.OpBetween, Min: 900, Max: 100}},
			Action:     rules.Action{PackageName: "Big"},
		}}}
	})

	code, saved, msg := putSettings(t, srv.URL, broken)
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d: %s", code, msg)
	}
	if n := problemCount(t, saved, "linkFilter"); n != 1 {
		t.Errorf("PUT reported %d filter problems, want the broken pattern named", n)
	}
	if n := problemCount(t, saved, "packagizer"); n != 1 {
		t.Errorf("PUT reported %d packagizer problems, want the impossible range named", n)
	}

	// And again on the next visit to the page, which is the one that matters:
	// the user comes back tomorrow and has to be told the same thing.
	loaded := getSettings(t, srv.URL)
	if n := problemCount(t, loaded, "linkFilter"); n != 1 {
		t.Errorf("GET reported %d filter problems, want the broken rule still named", n)
	}
	if n := problemCount(t, loaded, "packagizer"); n != 1 {
		t.Errorf("GET reported %d packagizer problems", n)
	}
	// The rule itself came back too, or there would be nothing to fix.
	filter, ok := loaded["linkFilter"].(map[string]any)
	if !ok || len(filter["rules"].([]any)) != 1 {
		t.Errorf("the broken rule did not survive to the form: %v", loaded["linkFilter"])
	}
}

// TestSettingsNeverShipASecret pins the one thing GET must not do. Two passwords
// live in the settings now, and handing them to every connected browser makes
// the merge machinery that puts them back on save protect a value the client
// already has.
func TestSettingsNeverShipASecret(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	withSecrets := settingsWith(func(s *settings.Settings) {
		s.Reconnect = reconnect.Config{
			Method:   reconnect.MethodHTTP,
			Username: "admin",
			Password: "router-secret",
			Requests: []reconnect.Request{{URL: "http://192.0.2.1/reboot"}},
			CheckURL: "http://192.0.2.9/ip",
		}
		s.Connections = []proxycfg.Entry{{
			Kind: proxycfg.KindHTTP, Host: "proxy.lan", Port: 8080,
			Username: "u", Password: "proxy-secret", Enabled: true,
		}}
	})
	code, saved, msg := putSettings(t, srv.URL, withSecrets)
	if code != http.StatusOK {
		t.Fatalf("PUT answered %d: %s", code, msg)
	}

	// Both directions, because the save answers with the settings as well and a
	// response that echoes what was posted leaks just as effectively as a read.
	for where, body := range map[string]map[string]any{"PUT": saved, "GET": getSettings(t, srv.URL)} {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"router-secret", "proxy-secret"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("%s /api/settings shipped %q to the client", where, secret)
			}
		}
	}

	// The secrets are still on disk, or the redaction would just be data loss.
	stored := a.Settings.Get()
	if stored.Reconnect.Password != "router-secret" {
		t.Errorf("the router password was not stored: %q", stored.Reconnect.Password)
	}
	if len(stored.Connections) != 1 || stored.Connections[0].Password != "proxy-secret" {
		t.Errorf("the proxy password was not stored: %+v", stored.Connections)
	}
}

// TestRefusedRowsComeBackWithAReason keeps a row from disappearing on save.
// Sanitize's job is to drop what cannot be used; explaining it is this
// endpoint's, and a connection that vanishes silently is blamed on the proxy
// weeks later.
func TestRefusedRowsComeBackWithAReason(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	cases := []struct {
		name string
		s    settings.Settings
	}{
		{
			name: "a proxy with no port",
			s: settingsWith(func(s *settings.Settings) {
				s.Connections = []proxycfg.Entry{{Kind: proxycfg.KindHTTP, Host: "proxy.lan", Enabled: true}}
			}),
		},
		{
			name: "a window with no weekday",
			s: settingsWith(func(s *settings.Settings) {
				s.Schedule = []schedule.Entry{{Start: "22:00", End: "06:00", Action: schedule.ActionPause}}
			}),
		},
		{
			name: "a reconnect script with no check URL",
			s: settingsWith(func(s *settings.Settings) {
				s.Reconnect = reconnect.Config{
					Method:   reconnect.MethodHTTP,
					Requests: []reconnect.Request{{URL: "http://192.0.2.1/reboot"}},
				}
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, msg := putSettings(t, srv.URL, tc.s)
			if code != http.StatusBadRequest {
				t.Fatalf("answered %d, want the row refused with a reason", code)
			}
			if msg == "" {
				t.Error("refused with no reason at all")
			}
		})
	}
}

// TestOptionsOnlyOffersWhatTheAppHonours stops the settings form from listing a
// value nothing acts on. collide.Ask parks a task until a human answers, and
// there is neither a status for that nor a way to answer, so a task set to it
// would sit in the queue forever with nothing saying why.
func TestOptionsOnlyOffersWhatTheAppHonours(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/options")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Decoded loosely and narrowed per key, because not everything this route
	// answers with is a menu: the archive help text needs the name of the trash
	// folder, which is one string. Insisting on []string here would fail the
	// whole test on the shape of a value it has no opinion about.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	body := map[string][]string{}
	for key, msg := range raw {
		var list []string
		if err := json.Unmarshal(msg, &list); err == nil {
			body[key] = list
		}
	}

	for _, list := range []string{
		"mirrorPolicies", "collisionPolicies", "proxyKinds", "scheduleActions",
		// The archive page draws no chooser at all when its list is empty, so an
		// empty one here is a control that silently stops existing rather than a
		// control that misbehaves - which is harder to notice, not easier.
		"archiveCollisions", "archiveDisposals", "archiveFormats",
	} {
		if len(body[list]) == 0 {
			t.Errorf("%s is empty; the form has nothing to offer for it", list)
		}
	}
	// The rule vocabulary is served by GET /api/rules/grammar, built from the
	// engine. A second hand-written copy here is how a dropdown comes to offer an
	// operator Compile refuses, which saves cleanly and then never fires.
	for _, gone := range []string{"ruleFields", "ruleOps", "ruleActions"} {
		// Against the raw response, not the narrowed map: a key that came back
		// as something other than a list would slip through the narrowing, and
		// this assertion is about the key being there at all.
		if _, ok := raw[gone]; ok {
			t.Errorf("%s is back in /api/options; the grammar route is the one source", gone)
		}
	}
	for _, p := range body["collisionPolicies"] {
		if p == string(collide.Ask) {
			t.Error("the collision dropdown offers a policy the app cannot honour")
		}
	}
	// The same rule one layer along: an extraction has even less of a way to
	// ask than a download does, and a job parked on a question nobody can see
	// never finishes and never says why.
	for _, p := range body["archiveCollisions"] {
		if p == string(collide.Ask) {
			t.Error("the archive collision chooser offers a policy an extraction cannot honour")
		}
	}
	for _, act := range body["ruleActions"] {
		if act == "filename" {
			t.Error("the rule form offers a rename the app deliberately does not apply")
		}
	}
}

// TestRuleTestRunsWithoutTouchingTheCollector is what makes a rule list editable
// at all. The alternative is pasting real links to find out what a rule does,
