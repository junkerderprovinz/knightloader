package app

import (
	"errors"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/provision"
)

// The JDSetup values say why the desktop build has no JDownloader of its own,
// so the interface can tell someone what to do instead of only "not set up".
const (
	JDSetupStarting = "starting"
	JDSetupNoJava   = "noJava"
	JDSetupFailed   = "failed"
)

type jdSetupState struct {
	jdSetupMu     sync.Mutex
	jdSetupValue  string
	jdSetupDetail string
}

func (s *jdSetupState) setJDSetup(value, detail string) {
	s.jdSetupMu.Lock()
	s.jdSetupValue, s.jdSetupDetail = value, detail
	s.jdSetupMu.Unlock()
}

func (s *jdSetupState) jdSetup() (string, string) {
	s.jdSetupMu.Lock()
	defer s.jdSetupMu.Unlock()
	return s.jdSetupValue, s.jdSetupDetail
}

// SettingUpJD records that the desktop build is starting its own JDownloader,
// which takes minutes on the first run. UseJD or JDSetupFailed ends it.
func (a *App) SettingUpJD() { a.setJDSetup(JDSetupStarting, "") }

// JDSetupFailed records why the desktop build's own JDownloader did not start.
func (a *App) JDSetupFailed(err error) {
	if errors.Is(err, provision.ErrNoJava) {
		a.setJDSetup(JDSetupNoJava, err.Error())
		return
	}
	a.setJDSetup(JDSetupFailed, err.Error())
}
