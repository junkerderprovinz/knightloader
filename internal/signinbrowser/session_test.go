package signinbrowser

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTheLoginPageOpensOnlyOnceTheGuardStands(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")

	nav := f.waitFor(t, "the login page", method("S-T1", "Page.navigate"))
	if nav.params["url"] != "https://www.patreon.com/login" {
		t.Errorf("the window opened %v, want Patreon's login page", nav.params["url"])
	}
	navAt := f.index(method("S-T1", "Page.navigate"))
	for _, before := range []struct {
		what string
		ok   func(sent) bool
	}{
		{"every request paused for the guard", func(c sent) bool { return c.session == "" && c.method == "Fetch.enable" }},
		{"downloads denied", func(c sent) bool {
			return c.method == "Browser.setDownloadBehavior" && c.params["behavior"] == "deny"
		}},
		{"new pages held until set up", func(c sent) bool {
			return c.method == "Target.setAutoAttach" && c.params["waitForDebuggerOnStart"] == true
		}},
		{"the page let go", method("S-T1", "Runtime.runIfWaitingForDebugger")},
	} {
		if i := f.index(before.ok); i < 0 || i > navAt {
			t.Errorf("%s: at %d, want before the login page at %d", before.what, i, navAt)
		}
	}
	for _, arg := range []string{"--headless", "--user-data-dir=", "--password-store=basic", "--disable-blink-features=AutomationControlled"} {
		if !hasArg(f.args, arg) {
			t.Errorf("Chromium was started without %s: %v", arg, f.args)
		}
	}
	if hasArg(f.args, "--enable-automation") || hasArg(f.args, "--remote-debugging-port") {
		t.Errorf("Chromium was started with an automation switch or a port: %v", f.args)
	}
}

func TestThePageIdentifiesAsChromeWithItsOwnClientHints(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	ua := f.waitFor(t, "the user agent", method("S-T1", "Emulation.setUserAgentOverride"))
	if s, _ := ua.params["userAgent"].(string); strings.Contains(s, "Headless") || !strings.Contains(s, "Chrome/158") {
		t.Errorf("user agent = %q, want Chromium's own without Headless", s)
	}
	meta, _ := ua.params["userAgentMetadata"].(map[string]any)
	if meta["platform"] != "Linux" || meta["bitness"] != "64" || meta["brands"] == nil {
		t.Errorf("client hints = %v, want the browser's own", meta)
	}
}

func TestAPageOffTheListIsBlockedAndTheViewerTold(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	v := newViewer()
	defer h.watch(t, v)()

	f.emit("", "Fetch.requestPaused", map[string]any{"requestId": "R1", "frameId": "T1", "resourceType": "Document",
		"request": map[string]any{"url": "https://example.com/elsewhere"}})
	f.emit("", "Fetch.requestPaused", map[string]any{"requestId": "R2", "frameId": "T1", "resourceType": "Document",
		"request": map[string]any{"url": "https://www.patreon.com/home"}})
	f.emit("", "Fetch.requestPaused", map[string]any{"requestId": "R3", "frameId": "T1", "resourceType": "Image",
		"request": map[string]any{"url": "http://192.168.20.1/logo.png"}})

	// No Content keeps the page where it was, where an error page would
	// put the blocked address in the bar.
	refused := f.waitFor(t, "the blocked page refused", func(c sent) bool {
		return c.params["requestId"] == "R1" && c.method != "Fetch.continueRequest"
	})
	if refused.method != "Fetch.fulfillRequest" || refused.params["responseCode"] != float64(204) {
		t.Errorf("refused with %s %v, want No Content", refused.method, refused.params)
	}
	f.waitFor(t, "the site's own page let through", func(c sent) bool {
		return c.method == "Fetch.continueRequest" && c.params["requestId"] == "R2"
	})
	f.waitFor(t, "the LAN address refused", func(c sent) bool {
		return c.method == "Fetch.failRequest" && c.params["requestId"] == "R3"
	})
	if m := v.next(t, "blocked"); m["host"] != "example.com" {
		t.Errorf("the viewer heard of %v, want example.com", m["host"])
	}
}

func TestAPopupHeldForTheDebuggerIsLetGoWithoutWaitingForIt(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	f.attach("T2", "T1", "")
	f.waitFor(t, "the popup let go", method("S-T2", "Runtime.runIfWaitingForDebugger"))
	// The events behind the popup keep flowing.
	f.emit("S-T1", "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "T1", "url": "file:///C:/"}})
	f.waitFor(t, "the event after the popup handled", func(c sent) bool {
		n := 0
		for _, o := range f.sentCalls() {
			if o.method == "Page.navigate" {
				n++
			}
		}
		return n == 2
	})
}

func TestAPopupOffTheListIsClosed(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	f.attach("T2", "T1", "")
	f.waitFor(t, "the popup let go", method("S-T2", "Runtime.runIfWaitingForDebugger"))
	f.emit("", "Fetch.requestPaused", map[string]any{"requestId": "R9", "frameId": "T2", "resourceType": "Document",
		"request": map[string]any{"url": "https://ads.example.com/"}})
	f.waitFor(t, "the popup closed", func(c sent) bool {
		return c.method == "Target.closeTarget" && c.params["targetId"] == "T2"
	})
}

func TestATopFrameThatLandsOutsideTheWebIsSentBack(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	f.emit("S-T1", "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "T1", "url": "file:///C:/"}})
	logins := func() int {
		n := 0
		for _, c := range f.sentCalls() {
			if c.method == "Page.navigate" && c.params["url"] == "https://www.patreon.com/login" {
				n++
			}
		}
		return n
	}
	f.waitFor(t, "the login page again", func(sent) bool { return logins() == 2 })
}

func TestFramesReachTheViewerAndAreAcknowledged(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	v := newViewer()
	defer h.watch(t, v)()
	f.emit("S-T1", "Page.screencastFrame", frame("jpeg bytes"))
	select {
	case got := <-v.frames:
		if string(got) != "jpeg bytes" {
			t.Errorf("frame = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no frame reached the viewer")
	}
	f.waitFor(t, "the frame acknowledged", func(c sent) bool {
		return c.session == "S-T1" && c.method == "Page.screencastFrameAck" && c.params["sessionId"] == float64(7)
	})
}

func TestTheViewersInputReachesThePageInFront(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	h.m.Input(Input{Type: "mousedown", X: 5, Y: 6, Button: 0, Buttons: 1, Clicks: 1})
	f.waitFor(t, "the click on the page", method("S-T1", "Input.dispatchMouseEvent"))

	// A sign-in popup on the list comes to the front.
	f.attach("T2", "T1", "")
	f.waitFor(t, "the popup let go", method("S-T2", "Runtime.runIfWaitingForDebugger"))
	f.waitFor(t, "the popup's screencast", method("S-T2", "Page.startScreencast"))
	h.m.Input(Input{Type: "text", Text: "ä"})
	f.waitFor(t, "the text in the popup", method("S-T2", "Input.insertText"))

	// Once it closes, the page behind it is in front again.
	f.emit("", "Target.detachedFromTarget", map[string]any{"sessionId": "S-T2", "targetId": "T2"})
	f.waitFor(t, "the page's screencast again", func(sent) bool {
		n := 0
		for _, c := range f.sentCalls() {
			if c.session == "S-T1" && c.method == "Page.startScreencast" {
				n++
			}
		}
		return n == 2
	})
	h.m.Input(Input{Type: "keydown", Key: "Tab", Code: "Tab", KeyCode: 9})
	f.waitFor(t, "the key on the page", method("S-T1", "Input.dispatchKeyEvent"))
}

func TestBackAndReloadMoveThePageInFront(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	h.m.Input(Input{Type: "back"})
	back := f.waitFor(t, "back", method("S-T1", "Page.navigateToHistoryEntry"))
	if back.params["entryId"] != float64(11) {
		t.Errorf("back went to entry %v, want the one before", back.params["entryId"])
	}
	h.m.Input(Input{Type: "reload"})
	f.waitFor(t, "reload", method("S-T1", "Page.reload"))
}

// A sign-in popup opens with no history of its own, and Back leaves it for
// the page behind.
func TestBackFromAFreshPopupClosesIt(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	v := newViewer()
	defer h.watch(t, v)()
	f.attach("T2", "T1", "")
	f.waitFor(t, "the popup's screencast", method("S-T2", "Page.startScreencast"))
	for {
		m := v.next(t, "page")
		if m["host"] != "accounts.google.com" {
			continue
		}
		if m["canGoBack"] != true {
			t.Errorf("a popup offers no way back: %v", m)
		}
		break
	}
	h.m.Input(Input{Type: "back"})
	f.waitFor(t, "the popup closed", func(c sent) bool {
		return c.method == "Target.closeTarget" && c.params["targetId"] == "T2"
	})
}

func TestResizingFitsThePageToThePanel(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	h.m.Input(Input{Type: "resize", Width: 5000, Height: 700, Scale: 2})
	got := f.waitFor(t, "the new size", func(c sent) bool {
		return c.method == "Emulation.setDeviceMetricsOverride" && c.params["height"] == float64(700)
	})
	if got.params["width"] != float64(maxWidth) || got.params["deviceScaleFactor"] != float64(2) {
		t.Errorf("metrics = %v, want the width capped at %d", got.params, maxWidth)
	}
}

func TestASignInIsHandedOnOnceItsCookiesAppearAndASignOutToo(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	v := newViewer()
	defer h.watch(t, v)()

	h.m.tick(h.clock.now())
	select {
	case got := <-h.handed:
		t.Fatalf("a browser holding no sign-in handed on %+v", got)
	default:
	}

	f.setCookies(cookie(".patreon.com", "session_id", "S1"), cookie(".google.com", "SID", "NOT-PATREON"))
	h.m.tick(h.clock.now())
	got := <-h.handed
	if got.site != "patreon" || !strings.Contains(got.jar, "session_id\tS1") || strings.Contains(got.jar, "NOT-PATREON") {
		t.Errorf("handed on %+v, want Patreon's own session", got)
	}
	if m := v.next(t, "login"); m["login"] == nil {
		t.Errorf("the viewer was not told: %v", m)
	}

	h.m.tick(h.clock.now())
	select {
	case again := <-h.handed:
		t.Errorf("the same sign-in was handed on twice: %+v", again)
	default:
	}

	f.setCookies()
	h.m.tick(h.clock.now())
	if out := <-h.handed; out.jar != "" {
		t.Errorf("signing out handed on %q, want an empty jar", out.jar)
	}
}

func TestClosingHandsOnTheLatestCookiesAndStopsTheBrowser(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	f.setCookies(cookie(".patreon.com", "session_id", "S2"))
	h.m.Close(context.Background())
	got := <-h.handed
	if !strings.Contains(got.jar, "session_id\tS2") {
		t.Errorf("closing handed on %q, want the latest session", got.jar)
	}
	f.waitFor(t, "the browser closed", method("", "Browser.close"))
	if h.m.OpenSite() != "" {
		t.Errorf("a window is still open for %q", h.m.OpenSite())
	}
}

func TestAWindowLeftAloneCloses(t *testing.T) {
	h := newHarness(t)
	h.open(t, "patreon")
	v := newViewer()
	defer h.watch(t, v)()

	h.m.tick(h.clock.add(idleAfter - time.Second))
	if h.m.OpenSite() == "" {
		t.Fatal("the window closed before it was idle")
	}
	h.m.Input(Input{Type: "mousemove", X: 1, Y: 1})
	h.m.tick(h.clock.add(idleAfter - time.Second))
	if h.m.OpenSite() == "" {
		t.Fatal("the window closed although the pointer moved")
	}
	h.m.tick(h.clock.add(2 * time.Second))
	if h.m.OpenSite() != "" {
		t.Fatal("an idle window stayed open")
	}
	if m := v.next(t, "closed"); m["reason"] != "idle" {
		t.Errorf("closed for %v, want idle", m["reason"])
	}
}

func TestAWindowNobodyWatchesCloses(t *testing.T) {
	h := newHarness(t)
	h.open(t, "patreon")
	stop := h.watch(t, newViewer())
	stop()
	h.m.tick(h.clock.add(goneAfter / 2))
	if h.m.OpenSite() == "" {
		t.Fatal("the window closed while the page could still come back")
	}
	h.m.tick(h.clock.add(goneAfter))
	if h.m.OpenSite() != "" {
		t.Fatal("a window nobody watches stayed open")
	}
}

func TestOpeningAnotherSiteClosesTheFirstWindow(t *testing.T) {
	h := newHarness(t)
	first := h.open(t, "patreon")
	first.setCookies(cookie(".patreon.com", "session_id", "S3"))
	h.open(t, "youtube")
	if got := <-h.handed; got.site != "patreon" {
		t.Errorf("handed on %+v, want Patreon's session before YouTube opens", got)
	}
	first.waitFor(t, "the first browser closed", method("", "Browser.close"))
	if h.m.OpenSite() != "youtube" {
		t.Errorf("open site = %q, want youtube", h.m.OpenSite())
	}
	second := h.browser(t, 1)
	nav := second.waitFor(t, "YouTube's login", method("S-T1", "Page.navigate"))
	if !strings.HasPrefix(nav.params["url"].(string), "https://accounts.google.com/") {
		t.Errorf("YouTube opened %v", nav.params["url"])
	}
}

func TestAViewerHearsWhenGoogleTurnsTheBrowserAway(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "youtube")
	v := newViewer()
	defer h.watch(t, v)()
	f.emit("S-T1", "Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "T1",
		"url": "https://accounts.google.com/v3/signin/rejected?continue=https://www.youtube.com/"}})
	v.next(t, "refused")
}

func TestTheLiveCookiesAreReadForTheOpenSiteOnly(t *testing.T) {
	h := newHarness(t)
	f := h.open(t, "patreon")
	f.setCookies(cookie(".patreon.com", "session_id", "LIVE"))
	if jar, ok := h.m.Jar(context.Background(), "patreon"); !ok || !strings.Contains(jar, "LIVE") {
		t.Errorf("Jar(patreon) = %q, %v, want the live session", jar, ok)
	}
	if _, ok := h.m.Jar(context.Background(), "youtube"); ok {
		t.Error("a window open for Patreon answered for YouTube")
	}
}
