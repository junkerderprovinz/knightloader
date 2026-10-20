package backup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// StateFiles are the files in the data directory besides the database and
// settings.json that make up an instance: its login, its accounts with the
// credential key they are sealed under (hoster and debrid logins, header
// profiles, captcha keys, VPN logins, media server and media hook tokens),
// its API and assistant tokens, its scripts, its pairing and the few records
// it keeps across restarts. Caches, logs, tools, downloads in progress and the
// backups themselves are not among them.
var StateFiles = []string{
	auth.FileName,
	accounts.KeyringName,
	"accounts.json",
	"account_meta.json",
	"tokens.json",
	"token-scopes.json",
	"mcp-clients.json",
	"scripts.json",
	"group.json",
	"instances.json",
	"instance-id",
	"torrent-totals.json",
	"vpn.json",
	"captcha-order-checked",
}

// checkStateNames refuses a manifest naming a file outside StateFiles, which
// a restore would otherwise write or remove anywhere.
func checkStateNames(names []string) error {
	for _, name := range names {
		if !slices.Contains(StateFiles, name) {
			return fmt.Errorf("the backup names %q, which is not a file of an instance", name)
		}
	}
	return nil
}

// readStateFiles reads the state files a manifest names out of an upload and
// checks each the way its own package reads it, so a damaged one is refused
// while the upload can still be named.
func readStateFiles(zr *zip.Reader, names []string, budget *int64) (map[string][]byte, error) {
	if err := checkStateNames(names); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, name := range names {
		if _, err := fs.Stat(zr, name); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		b, err := readEntry(zr, name, maxStateFileBytes, budget)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(name, ".json") && !json.Valid(b) {
			return nil, fmt.Errorf("the backup's %s is damaged", name)
		}
		files[name] = b
	}
	if state, ok := files[auth.FileName]; ok {
		if _, _, err := auth.PasswordIn(state); err != nil {
			return nil, fmt.Errorf("the backup's %w", err)
		}
	}
	accountsJSON, hasAccounts := files["accounts.json"]
	keyring, hasKeyring := files[accounts.KeyringName]
	if hasAccounts && !hasKeyring {
		return nil, errors.New("the backup carries accounts.json without the key it is sealed under")
	}
	if hasKeyring {
		if err := accounts.Verify(accountsJSON, keyring); err != nil {
			return nil, fmt.Errorf("the backup's stored accounts do not open: %w", err)
		}
	}
	return files, nil
}
