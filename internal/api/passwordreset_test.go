package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/logring"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

// None of these tests is parallel: each one stands in for the process's own
// output, which is one variable for the package.

const (
	resetOldPassword = "a-good-password"
	resetNewPassword = "another-good-password"
)

var shownResetCode = regexp.MustCompile(`Password reset code: ([2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4})`)

// output is the process's error output for a test, safe to read while a
// handler writes.
type output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// writeTo makes w what a reset code is written to for the rest of the test.
// A nil w is a process without a readable output.
func writeTo(t *testing.T, w io.Writer) {
	t.Helper()
	prev := resetConsole
	resetConsole = func() io.Writer { return w }
	t.Cleanup(func() { resetConsole = prev })
}

// resetServer is an instance locked with resetOldPassword whose output is
// the returned buffer.
func resetServer(t *testing.T) (*httptest.Server, *app.App, *output) {
	t.Helper()
	out := &output{}
	writeTo(t, out)
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	if err := a.Auth.SetPassword("", resetOldPassword); err != nil {
		t.Fatal(err)
	}
	return srv, a, out
}

func postTo(t *testing.T, c *http.Client, url string, v any) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(v)
	resp, err := c.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

// askForCode presses "Forgot password?" and reads the code off the output.
func askForCode(t *testing.T, base string, out *output) string {
	t.Helper()
	if status, body := postTo(t, http.DefaultClient, base+"/api/auth/reset", struct{}{}); status != http.StatusOK {
		t.Fatalf("asking for a code answered %d: %s", status, body)
	}
	m := shownResetCode.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no code in the output: %q", out.String())
	}
	return m[1]
}

func finishReset(t *testing.T, c *http.Client, base, code, password string) (int, []byte) {
	t.Helper()
	return postTo(t, c, base+"/api/auth/reset/finish", map[string]string{"code": code, "password": password})
}

func tasksStatus(t *testing.T, base string, header http.Header) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/tasks", nil)
	req.Header = header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func sessionHeader(token string) http.Header {
	return http.Header{"Cookie": {auth.CookieName + "=" + token}}
}

func TestALockedOutOwnerResetsThePassword(t *testing.T) {
	srv, _, out := resetServer(t)
	browser := &http.Client{Jar: newJar(t)}

	if status := tasksStatus(t, srv.URL, nil); status != http.StatusUnauthorized {
		t.Fatalf("the instance answered %d before the reset, want it locked", status)
	}
	code := askForCode(t, srv.URL, out)
	if status, body := finishReset(t, browser, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}

	resp, err := browser.Get(srv.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the browser that reset the password answered %d, want it signed in", resp.StatusCode)
	}
	if status := login(t, &http.Client{Jar: newJar(t)}, srv.URL, resetNewPassword); status != http.StatusOK {
		t.Errorf("the new password answered %d", status)
	}
	if status := login(t, &http.Client{Jar: newJar(t)}, srv.URL, resetOldPassword); status != http.StatusUnauthorized {
		t.Errorf("the old password answered %d, want 401", status)
	}
}

// TestTheResetCodeIsInNoAnswer: the code proves access to the machine only
// while the HTTP port never hands it out.
func TestTheResetCodeIsInNoAnswer(t *testing.T) {
	srv, _, out := resetServer(t)

	status, asked := postTo(t, http.DefaultClient, srv.URL+"/api/auth/reset", struct{}{})
	if status != http.StatusOK {
		t.Fatalf("asking for a code answered %d: %s", status, asked)
	}
	m := shownResetCode.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no code in the output: %q", out.String())
	}
	_, refused := finishReset(t, http.DefaultClient, srv.URL, "2222-2222", resetNewPassword)
	_, done := finishReset(t, http.DefaultClient, srv.URL, m[1], resetNewPassword)

	bare := strings.ReplaceAll(m[1], "-", "")
	for name, body := range map[string][]byte{"asking": asked, "a wrong code": refused, "the right code": done} {
		if bytes.Contains(body, []byte(m[1])) || bytes.Contains(body, []byte(bare)) {
			t.Errorf("the answer to %s carries the code: %s", name, body)
		}
	}
	var place resetPlace
	if err := json.Unmarshal(asked, &place); err != nil {
		t.Fatal(err)
	}
	if place.Place != "container" && place.Place != "process" {
		t.Errorf("place = %q, want the process's own output named", place.Place)
	}
	if place.Dir != "" || place.File != "" {
		t.Errorf("the answer names a file or a folder for a code that went to the output: %+v", place)
	}
}

// TestAskingAnswersTheSameWhetherACodeWasWrittenOrNot: a caller must not
// learn from the answer whether somebody is in the middle of a reset. The
// third request comes after the code was guessed away and inside the minute
// in which no new one is minted.
func TestAskingAnswersTheSameWhetherACodeWasWrittenOrNot(t *testing.T) {
	srv, _, out := resetServer(t)
	ask := func() (int, string) {
		status, body := postTo(t, http.DefaultClient, srv.URL+"/api/auth/reset", struct{}{})
		return status, string(body)
	}

	firstStatus, first := ask()
	liveStatus, live := ask()
	for range auth.ResetCodeTries {
		finishReset(t, http.DefaultClient, srv.URL, "2222-2222", resetNewPassword)
	}
	heldStatus, held := ask()

	if firstStatus != http.StatusOK || liveStatus != firstStatus || heldStatus != firstStatus {
		t.Errorf("statuses %d, %d and %d, want 200 three times", firstStatus, liveStatus, heldStatus)
	}
	if live != first || held != first {
		t.Errorf("the answers differ:\n%s\n%s\n%s", first, live, held)
	}
	if n := len(shownResetCode.FindAllString(out.String(), -1)); n != 1 {
		t.Errorf("%d codes were written for three requests within a minute, want 1", n)
	}
}

// TestARefusedResetCodeAnswersTheSameWhateverTheReason: no code asked for, a
// wrong guess at a live one and a code that was used up are one answer.
func TestARefusedResetCodeAnswersTheSameWhateverTheReason(t *testing.T) {
	srv, a, out := resetServer(t)
	refuse := func(code string) string {
		t.Helper()
		status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword)
		if status != http.StatusUnauthorized {
			t.Fatalf("a refused code answered %d: %s", status, body)
		}
		return string(body)
	}

	nothingAsked := refuse("2222-2222")
	code := askForCode(t, srv.URL, out)
	wrongGuess := refuse("2222-2222")
	for range auth.ResetCodeTries - 1 {
		refuse("2222-2222")
	}
	usedUp := refuse(code)

	if wrongGuess != nothingAsked || usedUp != nothingAsked {
		t.Errorf("the refusals differ:\n%s\n%s\n%s", nothingAsked, wrongGuess, usedUp)
	}
	if !a.Auth.Check(resetOldPassword) {
		t.Error("a refused code changed the password")
	}
}

func TestARefusedResetCodeSignsNobodyIn(t *testing.T) {
	srv, a, out := resetServer(t)
	session := a.Auth.Issue()
	askForCode(t, srv.URL, out)

	body, _ := json.Marshal(map[string]string{"code": "2222-2222", "password": resetNewPassword})
	resp, err := http.Post(srv.URL+"/api/auth/reset/finish", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(resp.Cookies()) != 0 {
		t.Error("a wrong code was answered with a cookie")
	}
	if status := tasksStatus(t, srv.URL, sessionHeader(session)); status != http.StatusOK {
		t.Errorf("a wrong code ended a session: it answered %d", status)
	}
}

// TestAResetLeavesTheNewPasswordAsTheOnlyWayIn: whoever resets from the
// machine has to end up alone in the instance. A session, an API token and a
// passkey from before could each belong to somebody else, and the second
// factor may be on the phone that went with the password.
func TestAResetLeavesTheNewPasswordAsTheOnlyWayIn(t *testing.T) {
	out := &output{}
	writeTo(t, out)
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	armTwoFactor(t, a, resetOldPassword)
	session := a.Auth.Issue()
	_, token, err := a.APITokens.Create("the phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.AddPasskey(store.Passkey{
		Name: "my phone", RPID: "kl.example.com",
		CredentialID: []byte{1, 2, 3}, PublicKey: []byte{4, 5, 6},
	}); err != nil {
		t.Fatal(err)
	}
	bearer := http.Header{"Authorization": {"Bearer " + token}}
	if tasksStatus(t, srv.URL, sessionHeader(session)) != http.StatusOK || tasksStatus(t, srv.URL, bearer) != http.StatusOK {
		t.Fatal("the session or the token did not work before the reset")
	}

	code := askForCode(t, srv.URL, out)
	if status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}

	if status := tasksStatus(t, srv.URL, sessionHeader(session)); status != http.StatusUnauthorized {
		t.Errorf("a session from before the reset answered %d, want 401", status)
	}
	if status := tasksStatus(t, srv.URL, bearer); status != http.StatusUnauthorized {
		t.Errorf("an API token from before the reset answered %d, want 401", status)
	}
	if left, _ := a.Store.ListPasskeys(); len(left) != 0 {
		t.Errorf("%d passkeys outlived the reset", len(left))
	}
	if a.Auth.TwoFactorEnabled() {
		t.Error("the second factor outlived the reset")
	}
	status, answer := loginWith(t, &http.Client{Jar: newJar(t)}, srv.URL, resetNewPassword, "")
	if status != http.StatusOK || !answer.Authenticated {
		t.Errorf("the new password alone answered %d, %+v, want it to sign in without a code", status, answer)
	}
}

// TestAResetClosesTheLiveStreams: a stream is let in when it opens, so one
// opened under a session that has just ended would go on delivering.
func TestAResetClosesTheLiveStreams(t *testing.T) {
	srv, a, out := resetServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.URL+"/api/ws", &websocket.DialOptions{HTTPHeader: sessionHeader(a.Auth.Issue())})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	readOneOfType(t, c, "snapshot")

	code := askForCode(t, srv.URL, out)
	if status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}

	for {
		_, frame, err := c.Read(ctx)
		if err != nil {
			break
		}
		if bytes.Contains(frame, []byte(code)) || bytes.Contains(frame, []byte(strings.ReplaceAll(code, "-", ""))) {
			t.Fatalf("the stream carried the code: %s", frame)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("a stream opened before the reset was still open ten seconds after it")
	}
}

// TestWrongResetCodesMeetAThrottleOfTheirOwn: guesses are counted, and
// counted apart from the password's, so hammering one way in does not shut
// the other.
func TestWrongResetCodesMeetAThrottleOfTheirOwn(t *testing.T) {
	srv, _, _ := resetServer(t)

	var last int
	for range loginFailBurst + 1 {
		last, _ = finishReset(t, http.DefaultClient, srv.URL, "2222-2222", resetNewPassword)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong codes the route answered %d, want 429", loginFailBurst+1, last)
	}
	if status := login(t, &http.Client{Jar: newJar(t)}, srv.URL, resetOldPassword); status != http.StatusOK {
		t.Errorf("the password answered %d after the reset ran out of attempts, want 200", status)
	}
}

func TestAThrottledPasswordLeavesTheResetOpen(t *testing.T) {
	srv, _, out := resetServer(t)
	var last int
	for range loginFailBurst + 1 {
		last = login(t, http.DefaultClient, srv.URL, "not-the-password")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong passwords the login answered %d, want 429", loginFailBurst+1, last)
	}

	code := askForCode(t, srv.URL, out)
	if status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Errorf("the right code answered %d while the password was throttled: %s", status, body)
	}
}

func TestAPasswordThatIsTooShortCostsNeitherTheCodeNorAnAttempt(t *testing.T) {
	srv, _, out := resetServer(t)
	code := askForCode(t, srv.URL, out)

	for range loginFailBurst + 1 {
		if status, body := finishReset(t, http.DefaultClient, srv.URL, code, "short"); status != http.StatusBadRequest {
			t.Fatalf("a short password answered %d: %s", status, body)
		}
	}
	if status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Errorf("the code answered %d after passwords that were too short: %s", status, body)
	}
}

func TestAnOpenInstanceHasNothingToReset(t *testing.T) {
	out := &output{}
	writeTo(t, out)
	srv, a := testServer(t)
	t.Cleanup(srv.Close)

	status, body := postTo(t, http.DefaultClient, srv.URL+"/api/auth/reset", struct{}{})
	if status != http.StatusBadRequest || !bytes.Contains(body, []byte(`"noPassword"`)) {
		t.Errorf("asking on an open instance answered %d: %s", status, body)
	}
	if out.String() != "" {
		t.Errorf("something was written for an instance without a password: %q", out.String())
	}
	status, body = finishReset(t, http.DefaultClient, srv.URL, "2222-2222", resetNewPassword)
	if status != http.StatusBadRequest {
		t.Errorf("finishing on an open instance answered %d: %s", status, body)
	}
	if a.Auth.Enabled() {
		t.Error("a reset on an open instance set a password")
	}
}

// TestTheResetCodeStaysOutOfTheLogPageTheLogFileAndTheBundle: all three are
// read by people who did not have to get onto the machine, and a bundle is
// attached to bug reports. The line that says a code was asked for is there,
// which shows the three were being written while the code went past them.
func TestTheResetCodeStaysOutOfTheLogPageTheLogFileAndTheBundle(t *testing.T) {
	srv, _, out := resetServer(t)
	dir := t.TempDir()
	if err := logring.OpenFile(logring.FileOptions{Dir: dir, MaxBytes: 1 << 20, Keep: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logring.CloseFile() })

	code := askForCode(t, srv.URL, out)
	browser := &http.Client{Jar: newJar(t)}
	if status, body := finishReset(t, browser, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Fatalf("the right code answered %d: %s", status, body)
	}

	read := func(path string) []byte {
		t.Helper()
		resp, err := browser.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := readAll(resp)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s answered %d, %v", path, resp.StatusCode, err)
		}
		return raw
	}
	file, err := os.ReadFile(filepath.Join(dir, logring.Name))
	if err != nil {
		t.Fatal(err)
	}
	const asked = "a password reset code was asked for"
	bare := strings.ReplaceAll(code, "-", "")
	for name, text := range map[string][]byte{
		"the log page": read("/api/diagnostics/log"),
		"the bundle":   read("/api/diagnostics"),
		"the log file": file,
	} {
		if !bytes.Contains(text, []byte(asked)) {
			t.Errorf("%s does not say that a code was asked for, so it proves nothing here", name)
		}
		if bytes.Contains(text, []byte(code)) || bytes.Contains(text, []byte(bare)) {
			t.Errorf("%s carries the code", name)
		}
	}
}

// TestWithNoReadableOutputTheCodeGoesToAFile: a service whose output goes
// nowhere still has its data folder, which holds the session key already.
func TestWithNoReadableOutputTheCodeGoesToAFile(t *testing.T) {
	writeTo(t, nil)
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	if err := a.Auth.SetPassword("", resetOldPassword); err != nil {
		t.Fatal(err)
	}

	status, body := postTo(t, http.DefaultClient, srv.URL+"/api/auth/reset", struct{}{})
	if status != http.StatusOK {
		t.Fatalf("asking for a code answered %d: %s", status, body)
	}
	var place resetPlace
	if err := json.Unmarshal(body, &place); err != nil {
		t.Fatal(err)
	}
	if place.Place != "file" || place.File != resetCodeFile {
		t.Errorf("the answer is %+v, want the file named", place)
	}
	if place.Dir != "" {
		t.Errorf("a server told a caller without a session where its data is: %q", place.Dir)
	}
	path := filepath.Join(a.DataDir, resetCodeFile)
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := shownResetCode.FindSubmatch(written)
	if m == nil {
		t.Fatalf("no code in the file: %q", written)
	}
	if bytes.Contains(body, m[1]) {
		t.Errorf("the answer carries the code: %s", body)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("the file's mode is %04o, want it readable by its owner alone", perm)
		}
	}

	if status, body := finishReset(t, http.DefaultClient, srv.URL, string(m[1]), resetNewPassword); status != http.StatusOK {
		t.Fatalf("the code from the file answered %d: %s", status, body)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is still there after the reset: %v", err)
	}
}

// TestTheDesktopBuildWritesAFileAndNamesItsFolder: a desktop program has no
// terminal, and what it tells the page reaches only the person at its window.
func TestTheDesktopBuildWritesAFileAndNamesItsFolder(t *testing.T) {
	prev := buildinfo.Deployment
	buildinfo.Deployment = "desktop"
	t.Cleanup(func() { buildinfo.Deployment = prev })
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	if err := a.Auth.SetPassword("", resetOldPassword); err != nil {
		t.Fatal(err)
	}

	status, body := postTo(t, http.DefaultClient, srv.URL+"/api/auth/reset", struct{}{})
	if status != http.StatusOK {
		t.Fatalf("asking for a code answered %d: %s", status, body)
	}
	var place resetPlace
	if err := json.Unmarshal(body, &place); err != nil {
		t.Fatal(err)
	}
	if place.Place != "file" || place.File != resetCodeFile || place.Dir != a.DataDir {
		t.Errorf("the answer is %+v, want the file %s in %s", place, resetCodeFile, a.DataDir)
	}
	written, err := os.ReadFile(filepath.Join(a.DataDir, resetCodeFile))
	if err != nil || !shownResetCode.Match(written) {
		t.Errorf("no code in the file: %q, %v", written, err)
	}
}

// brokenOutput is an output whose reader has gone away.
type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// TestACodeThatCouldNotBeWrittenIsNotHeld: a code nobody can read must not
// keep the next request from minting one that can be.
func TestACodeThatCouldNotBeWrittenIsNotHeld(t *testing.T) {
	writeTo(t, brokenOutput{})
	srv, a := testServer(t)
	t.Cleanup(srv.Close)
	if err := a.Auth.SetPassword("", resetOldPassword); err != nil {
		t.Fatal(err)
	}

	status, body := postTo(t, http.DefaultClient, srv.URL+"/api/auth/reset", struct{}{})
	if status != http.StatusServiceUnavailable || !bytes.Contains(body, []byte(`"resetUnavailable"`)) {
		t.Fatalf("asking with a broken output answered %d: %s", status, body)
	}

	out := &output{}
	writeTo(t, out)
	code := askForCode(t, srv.URL, out)
	if status, body := finishReset(t, http.DefaultClient, srv.URL, code, resetNewPassword); status != http.StatusOK {
		t.Errorf("the code written after the failure answered %d: %s", status, body)
	}
}

func TestACodeFileFromAnEarlierRunIsRemovedAtStart(t *testing.T) {
	a := testApp(t)
	path := filepath.Join(a.DataDir, resetCodeFile)
	if err := os.WriteFile(path, []byte("  Password reset code: ABCD-EFGH\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	Handler(a)

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a code file from a process that is gone is still there: %v", err)
	}
}

// TestTheResetIsNotReachableOverTheRelay: a sibling in the pairing group is
// let in without a session, and the group's phrase is no proof of access to
// this machine.
func TestTheResetIsNotReachableOverTheRelay(t *testing.T) {
	for _, path := range []string{"/api/auth/reset", "/api/auth/reset/finish"} {
		if relayForwardable(http.MethodPost, path) {
			t.Errorf("POST %s is forwarded to a group sibling", path)
		}
	}
}
