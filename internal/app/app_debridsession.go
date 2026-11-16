package app

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sync/atomic"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// linksnappySessions keeps Linksnappy's session sealed in the store of the
// app that runs. Its clients are built where no app is at hand, for routing,
// for every account reading and for every check, and a sign-in from a new
// place waits for a link Linksnappy mails.
var linksnappySessions = &sealedSessions{site: "linksnappy.com"}

// sealedSessions keeps the session of a debrid login under
// hosterSessionService, one per username, sealed with a print of the login
// so a changed password leaves it behind.
type sealedSessions struct {
	site  string
	store atomic.Pointer[accounts.Store]
}

func (s *sealedSessions) account(user string) string {
	sum := sha256.Sum256([]byte(user))
	return s.site + "/" + hex.EncodeToString(sum[:8])
}

func (s *sealedSessions) Session(user, pass string) string {
	st := s.store.Load()
	if st == nil {
		return ""
	}
	cred, err := st.GetCredential(hosterSessionService, s.account(user))
	if err != nil || cred.Username != loginPrint(user, pass) {
		return ""
	}
	return cred.APIKey
}

func (s *sealedSessions) KeepSession(user, pass, cookies string) {
	st := s.store.Load()
	if st == nil {
		return
	}
	var cred accounts.Credential
	if cookies != "" {
		cred = accounts.Credential{Username: loginPrint(user, pass), APIKey: cookies}
	}
	if err := st.SetCredential(hosterSessionService, s.account(user), cred); err != nil {
		log.Printf("%s: keeping the session failed: %v", s.site, err)
	}
}
