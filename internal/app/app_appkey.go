package app

import (
	"fmt"
	"log"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// keepCopiesBeforeSealing copies the files a first start with an app key is
// about to rewrite sealed. An auth.json still in the clear is what marks that
// first start; a settings.json in the clear on an install already sealed came
// from a restored backup, whose secrets the owner holds anyway.
func keepCopiesBeforeSealing(dataDir string, key *appkey.Key) error {
	if key == nil || !auth.NeedsSealing(dataDir) {
		return nil
	}
	dir, err := appkey.KeepCopies(dataDir, []string{auth.FileName, accounts.KeyringName, settings.FileName}, time.Now())
	if err != nil {
		return fmt.Errorf("app key: %w", err)
	}
	if dir != "" {
		log.Printf("app key: sealing the secrets in %s for the first time. The files as they were are kept in %s; "+
			"delete that folder once KnightLoader runs, since anybody who copies it can read them", dataDir, dir)
	}
	return nil
}
