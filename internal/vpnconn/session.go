package vpnconn

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/vpn"
	"github.com/junkerderprovinz/knightloader/internal/vpn/hideme"
)

// Login is what a provider's connect form was filled in with, by field name.
// It is sealed as one secret per provider.
type Login map[string]string

// accessTokenField keeps hide.me's access token beside the login it was
// fetched with, so a connect does not send the password every time.
const accessTokenField = "accessToken"

type dialFunc = func(ctx context.Context, network, address string) (net.Conn, error)

// session is one connection a provider opened.
type session struct {
	cfg vpn.Config
	// gateway answers NAT-PMP, where the provider hands it out per session.
	gateway netip.Addr
	// forward readies the provider for port forwarding, through the tunnel.
	forward func(ctx context.Context, dial dialFunc) error
	// askPort has the provider's API forward a port, through the tunnel.
	askPort func(ctx context.Context, dial dialFunc) (uint16, error)
	// end tells the provider the session is over.
	end func(ctx context.Context)
	// login is the login as it stands after opening, when that changed it.
	login Login
	// renews says the provider hands out sessions that can end on its side,
	// so a tunnel that stays down gets a new one.
	renews bool
	// keep runs while the tunnel stands, for what the provider wants
	// renewed: a forwarded port, or the session itself.
	keep func(ctx context.Context, k keeper)
}

// keeper is what a session's keep works with.
type keeper struct {
	dial    dialFunc
	setPort func(uint16)
	logf    func(format string, args ...any)
}

type opener func(ctx context.Context, login Login) (*session, error)

// Codes the VPN page words itself (web/src/lib/api.ts, VpnProvider.errorCode).
const (
	CodeConfigInvalid = "vpn-config-invalid"
	CodeLoginRefused  = "vpn-login-refused"
	CodeTimeout       = "vpn-timeout"
	CodeDropped       = "vpn-dropped"
	// CodeDeviceLimit is an account that has all the devices or keys it
	// allows registered already.
	CodeDeviceLimit = "vpn-device-limit"
	// CodeAccountExpired is an account with no time left.
	CodeAccountExpired = "vpn-account-expired"
	// CodeLocationUnknown is a location the provider has no server at.
	CodeLocationUnknown = "vpn-location-unknown"
	// CodeResumeFailed is a provider in use before a restart that could not
	// be connected again; what goes through the VPN waits.
	CodeResumeFailed = "vpn-resume-failed"
)

// final says a failure is one that trying again with the same login does
// not mend.
func final(code string) bool {
	switch code {
	case CodeConfigInvalid, CodeLoginRefused, CodeDeviceLimit, CodeAccountExpired, CodeLocationUnknown:
		return true
	}
	return false
}

// failure is a connect that did not work, with the code the page words it by.
type failure struct {
	code string
	err  error
}

func (f *failure) Error() string { return f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

func openConfig(_ context.Context, login Login) (*session, error) {
	cfg, err := vpn.ParseConfig(login["config"])
	if err != nil {
		return nil, &failure{CodeConfigInvalid, err}
	}
	return &session{cfg: cfg}, nil
}

// defaultHideMeServer is used when the form leaves the location empty.
const defaultHideMeServer = "nl"

func openHideMe(ctx context.Context, login Login) (*session, error) {
	c := &hideme.Client{
		Host:     cmp.Or(strings.TrimSpace(login["location"]), defaultHideMeServer),
		Username: strings.TrimSpace(login["username"]),
		Password: login["password"],
	}
	if b, err := base64.StdEncoding.DecodeString(login[accessTokenField]); err == nil {
		c.AccessToken = b
	}
	cfg, s, err := c.Connect(ctx)
	if refused(err) && len(c.AccessToken) > 0 {
		// A token from another server or one that ran out; the password
		// fetches a new one.
		c.AccessToken = nil
		cfg, s, err = c.Connect(ctx)
	}
	if err != nil {
		if refused(err) {
			return nil, &failure{CodeLoginRefused, err}
		}
		return nil, err
	}
	if s.StaleAccessToken {
		c.FetchAccessToken(ctx)
	}
	next := Login{}
	for k, v := range login {
		next[k] = v
	}
	next[accessTokenField] = base64.StdEncoding.EncodeToString(c.AccessToken)
	sess := &session{
		cfg:     cfg,
		login:   next,
		renews:  true,
		forward: func(ctx context.Context, dial dialFunc) error { return c.EnablePortForwarding(ctx, dial) },
		end:     func(ctx context.Context) { c.Disconnect(ctx, s) },
	}
	for _, g := range s.Gateway {
		if g.Is4() {
			sess.gateway = g
			break
		}
	}
	return sess, nil
}

func refused(err error) bool {
	var se *hideme.StatusError
	return errors.Is(err, hideme.ErrUpdateRequired) || errors.As(err, &se) && se.Code >= 400 && se.Code < 500
}

// classify gives an error of opening a session or setting the tunnel up its
// code, where it has one.
func classify(err error) *failure {
	var f *failure
	if errors.As(err, &f) {
		return f
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || errors.As(err, &ne) {
		return &failure{CodeTimeout, err}
	}
	return &failure{"", err}
}

// endSoon ends a session without holding up whoever let go of it.
func endSoon(s *session) {
	if s == nil || s.end == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.end(ctx)
	}()
}
