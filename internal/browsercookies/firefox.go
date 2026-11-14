package browsercookies

import (
	"database/sql"
	"strings"
	"time"
)

// firefoxMillis is the cookies.sqlite version from which Firefox counts the
// expiry in milliseconds rather than seconds (Firefox 142).
const firefoxMillis = 16

// readFirefox reads a copied cookies.sqlite. Only the default cookie store is
// read: a container tab keeps its own, as does a site embedded in another
// (a partitioned cookie), and neither is the session a link is opened with.
func readFirefox(path string, domains []string) ([]Cookie, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT originAttributes, host, name, value, path, expiry, isSecure, isHttpOnly, lastAccessed FROM moz_cookies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cookie
	for rows.Next() {
		var (
			origin             string
			c                  Cookie
			expiry, lastAccess int64
		)
		if err := rows.Scan(&origin, &c.Host, &c.Name, &c.Value, &c.Path, &expiry, &c.Secure, &c.HTTPOnly, &lastAccess); err != nil {
			return nil, err
		}
		if strings.Contains(origin, "userContextId=") || strings.Contains(origin, "partitionKey=") || !matches(c.Host, domains) {
			continue
		}
		if expiry > 0 {
			if version >= firefoxMillis {
				c.Expires = time.UnixMilli(expiry).UTC()
			} else {
				c.Expires = time.Unix(expiry, 0).UTC()
			}
		}
		if lastAccess > 0 {
			c.LastUsed = time.UnixMicro(lastAccess).UTC()
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
