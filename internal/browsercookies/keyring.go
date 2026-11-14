package browsercookies

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"time"
)

// keychainWait is how long the macOS Keychain may take, which includes the
// user answering its prompt.
const keychainWait = time.Minute

// keychainPassword asks the macOS Keychain for the password Chromium files
// under service and account. A variable so tests can answer for it.
var keychainPassword = cachedKeychain

var keychain struct {
	sync.Mutex
	answers map[string]keychainAnswer
}

type keychainAnswer struct {
	password []byte
	err      error
}

// cachedKeychain keeps the Keychain's answer, a refusal included, for as
// long as the process runs: asking again would put the prompt up again on
// every read.
func cachedKeychain(service, account string) ([]byte, error) {
	keychain.Lock()
	defer keychain.Unlock()
	if a, ok := keychain.answers[service]; ok {
		return a.password, a.err
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-w", "-a", account, "-s", service).Output()
	a := keychainAnswer{password: bytes.TrimSuffix(out, []byte("\n")), err: err}
	if keychain.answers == nil {
		keychain.answers = map[string]keychainAnswer{}
	}
	keychain.answers[service] = a
	return a.password, a.err
}

// secretServiceWait is how long the Linux keyring may take to answer.
const secretServiceWait = 10 * time.Second

// secretServicePassword asks the Secret Service, through libsecret's
// secret-tool, for the password Chromium stored for an application. Without
// secret-tool or a running keyring it fails, and v11 values stay unread. A
// variable so tests can answer for it.
var secretServicePassword = func(application string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), secretServiceWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "secret-tool", "lookup", "application", application).Output()
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}
