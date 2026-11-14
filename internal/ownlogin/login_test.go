package ownlogin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// codeHoster signs a login in only with code and keeps its tokens until
// ended is set.
type codeHoster struct {
	code   string
	logins int
	ended  bool
}

var errTokenGone = errors.New("token gone")

func (h *codeHoster) signer(age time.Duration) Signer {
	return Signer{
		Login: func(_ context.Context, _, _, code string) (string, Account, error) {
			h.logins++
			switch {
			case code == "":
				return "", Account{}, &LoginError{Msg: "code needed", Err: ErrNeedsCode}
			case code != h.code:
				return "", Account{}, &LoginError{Msg: "code wrong", Err: ErrWrongCode}
			}
			return "token", Account{Premium: true}, nil
		},
		Account: func(context.Context, string) (Account, error) {
			if h.ended {
				return Account{}, errTokenGone
			}
			return Account{Premium: true}, nil
		},
		Reason:  func(error) core.Reason { return core.ReasonAuth },
		Expired: func(err error) bool { return errors.Is(err, errTokenGone) },
		Age:     age,
		None:    errors.New("no login"),
	}
}

func TestASessionOpenedWithACodeIsKeptPastTheAgeOfAPasswordSession(t *testing.T) {
	h := &codeHoster{code: "111222"}
	l := NewLogin(h.signer(time.Nanosecond))
	l.SetLogin("user", "pass")
	if st := l.SignIn(context.Background(), "111222"); st.Refused {
		t.Fatalf("signing in with the code reads as %+v", st)
	}

	time.Sleep(time.Millisecond)
	s, err := l.Session(context.Background(), true)

	if err != nil || s.Token != "token" {
		t.Errorf("Session = %+v, %v; want the token the code opened", s, err)
	}
	if h.logins != 1 {
		t.Errorf("signed in %d times, want once: a session opened with a code cannot be opened again without one", h.logins)
	}
}

func TestAnEndedCodeSessionWaitsForANewCode(t *testing.T) {
	h := &codeHoster{code: "111222"}
	l := NewLogin(h.signer(0))
	l.SetLogin("user", "pass")
	l.SignIn(context.Background(), "111222")

	h.ended = true
	_, err := l.Session(context.Background(), false)
	_, again := l.Session(context.Background(), false)

	if !errors.Is(err, ErrNewCode) || !errors.Is(again, ErrNewCode) {
		t.Errorf("Session after the end = %v, then %v; want both asking for a new code", err, again)
	}
	if st := l.Status(); st.Code != CodeEnded || l.Ready() {
		t.Errorf("the login reads as %+v, ready %v; want it waiting for a new code", st, l.Ready())
	}
	if h.logins != 1 {
		t.Errorf("signed in %d times, want once", h.logins)
	}

	h.ended = false
	if st := l.SignIn(context.Background(), "111222"); st.Code != "" || !l.Ready() {
		t.Errorf("a new code leaves the login as %+v", st)
	}
}

func TestAChangedLoginForgetsTheCodeItWaitedFor(t *testing.T) {
	h := &codeHoster{code: "111222"}
	l := NewLogin(h.signer(0))
	l.SetLogin("user", "pass")
	l.Check(context.Background())

	l.SetLogin("user", "other")

	if st := l.Status(); st.Code != "" || st.Checked {
		t.Errorf("a changed login kept %+v", st)
	}
}
