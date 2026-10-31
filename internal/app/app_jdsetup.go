package app

import "sync"

// The JDSetup values say how far the desktop build has come with its own
// JDownloader while KL_JD is still empty.
const (
	JDSetupStarting = "starting"
	JDSetupFailed   = "failed"
)

type jdSetupState struct {
	jdSetupMu   sync.Mutex
	jdSetupNow  string
	jdSetupWhy  string
	jdSetupDone int64
	jdSetupSize int64
}

// SettingUpJD records that the desktop build is setting up its own
// JDownloader, which takes minutes the first time while the Java runtime
// arrives. UseJD or JDSetupFailed ends it.
func (a *App) SettingUpJD() {
	a.jdSetupMu.Lock()
	a.jdSetupNow, a.jdSetupWhy, a.jdSetupDone, a.jdSetupSize = JDSetupStarting, "", 0, 0
	a.jdSetupMu.Unlock()
}

// JDSetupProgress records how much of the Java runtime has arrived; total is
// -1 while unknown.
func (a *App) JDSetupProgress(done, total int64) {
	a.jdSetupMu.Lock()
	a.jdSetupDone, a.jdSetupSize = done, total
	a.jdSetupMu.Unlock()
}

// JDSetupFailed records why the desktop build's own JDownloader did not start.
func (a *App) JDSetupFailed(err error) {
	a.jdSetupMu.Lock()
	a.jdSetupNow, a.jdSetupWhy = JDSetupFailed, err.Error()
	a.jdSetupMu.Unlock()
}

func (a *App) clearJDSetup() {
	a.jdSetupMu.Lock()
	a.jdSetupNow, a.jdSetupWhy, a.jdSetupDone, a.jdSetupSize = "", "", 0, 0
	a.jdSetupMu.Unlock()
}

// jdSetupStatus fills in st, a status without a JDownloader, with how far the
// setup has come.
func (a *App) jdSetupStatus(st *JDStatus) {
	a.jdSetupMu.Lock()
	defer a.jdSetupMu.Unlock()
	st.Setup, st.Detail, st.SetupDone, st.SetupTotal = a.jdSetupNow, a.jdSetupWhy, a.jdSetupDone, a.jdSetupSize
}
