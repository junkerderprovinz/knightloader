package nox

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config is what KnightLoader sets in qBittorrent's own settings file before
// every start. The rest of the file is qBittorrent's.
type Config struct {
	Port     int
	Username string
	Password string
	// Version is the qBittorrent about to start, which decides how its
	// proxy is written down.
	Version string
	// Proxy is the SOCKS5 proxy every connection goes through from the
	// start on, nil for none.
	Proxy *Proxy
}

// Proxy is a SOCKS5 proxy with a login.
type Proxy struct {
	Host       string
	Port       int
	User, Pass string
}

// startBound is the interface qBittorrent is tied to when it starts with a
// proxy: none of that name exists, so nothing goes out until KnightLoader
// ties it to loopback, past the torrents it restores and starts on its own.
const startBound = "knightloader-vpn"

// ConfigPath is qBittorrent's settings file for a qBittorrent started with
// --profile=profile. Qt names an INI file .ini on Windows and .conf elsewhere.
func ConfigPath(profile string) string {
	name := "qBittorrent.conf"
	if runtime.GOOS == "windows" {
		name = "qBittorrent.ini"
	}
	return filepath.Join(profile, "qBittorrent", "config", name)
}

// The parameters qBittorrent's Utils::Password::PBKDF2 uses.
const (
	pbkdf2Rounds  = 100000
	pbkdf2SaltLen = 16
	pbkdf2KeyLen  = 64
)

func passwordHash(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha512.New, password, salt, pbkdf2Rounds, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding
	return `"@ByteArray(` + enc.EncodeToString(salt) + ":" + enc.EncodeToString(key) + `)"`, nil
}

// WriteConfig merges c into qBittorrent.conf. The WebUI answers on 127.0.0.1
// only, and even there it asks for the login, since any program on the
// machine can reach loopback. The legal notice is accepted ahead, because
// 4.x waits for a key press on its first start otherwise.
func WriteConfig(profile string, c Config) error {
	hash, err := passwordHash(c.Password)
	if err != nil {
		return err
	}
	set := []iniSection{
		{"LegalNotice", [][2]string{{"Accepted", "true"}}},
		{"Preferences", [][2]string{
			{`WebUI\Enabled`, "true"},
			{`WebUI\Address`, "127.0.0.1"},
			{`WebUI\Port`, strconv.Itoa(c.Port)},
			{`WebUI\Username`, c.Username},
			{`WebUI\Password_PBKDF2`, hash},
			{`WebUI\LocalHostAuth`, "true"},
			{`WebUI\AuthSubnetWhitelistEnabled`, "false"},
			{`WebUI\HostHeaderValidation`, "true"},
			{`WebUI\ServerDomains`, "127.0.0.1"},
			{`WebUI\CSRFProtection`, "true"},
			{`WebUI\ClickjackingProtection`, "true"},
			{`WebUI\UseUPnP`, "false"},
			{`WebUI\HTTPS\Enabled`, "false"},
			{`WebUI\AlternativeUIEnabled`, "false"},
			// It would fetch its country database on the first start,
			// before any setting KnightLoader makes over the WebUI.
			{`Connection\ResolvePeerCountries`, "false"},
		}},
	}
	set = append(set, proxySections(c)...)
	path := ConfigPath(profile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".kl"
	if err := os.WriteFile(tmp, []byte(mergeINI(string(old), set)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// proxySections are the settings that send qBittorrent through c.Proxy, or
// none of a proxy left from a run before. Up to 4.5 the type carries the
// login; later versions keep it apart.
func proxySections(c Config) []iniSection {
	px := c.Proxy
	if px == nil {
		return []iniSection{{"Network", [][2]string{{`Proxy\Type`, "None"}}}}
	}
	network := [][2]string{
		{`Proxy\Type`, "SOCKS5_PW"},
		{`Proxy\IP`, px.Host},
		{`Proxy\Port`, strconv.Itoa(px.Port)},
		{`Proxy\Username`, px.User},
		{`Proxy\Password`, px.Pass},
		{`Proxy\OnlyForTorrents`, "false"},
		{`PortForwardingEnabled`, "false"},
	}
	if namedProxyType(c.Version) {
		network[0][1] = "SOCKS5"
		network = append(network,
			[2]string{`Proxy\AuthEnabled`, "true"},
			[2]string{`Proxy\HostnameLookupEnabled`, "true"},
			[2]string{`Proxy\Profiles\BitTorrent`, "true"},
			[2]string{`Proxy\Profiles\Misc`, "true"},
			[2]string{`Proxy\Profiles\RSS`, "true"},
		)
	}
	return []iniSection{
		{"Network", network},
		{"BitTorrent", [][2]string{
			{`Session\ProxyPeerConnections`, "true"},
			{`Session\DHTEnabled`, "false"},
			{`Session\LSDEnabled`, "false"},
			{`Session\BTProtocol`, "TCP"},
			{`Session\AnonymousModeEnabled`, "true"},
			{`Session\Interface`, startBound},
			{`Session\InterfaceName`, startBound},
		}},
	}
}

// namedProxyType reports whether qBittorrent version v names its proxy type
// without the login, as 4.6 and later do.
func namedProxyType(v string) bool {
	major, rest, _ := strings.Cut(v, ".")
	minor, _, _ := strings.Cut(rest, ".")
	ma, _ := strconv.Atoi(major)
	mi, _ := strconv.Atoi(minor)
	return ma > 4 || ma == 4 && mi >= 6
}

type iniSection struct {
	name string
	keys [][2]string
}

// mergeINI writes each key of set into its section of the QSettings file
// text, in place where the key is already there and at the section's end
// where it is not.
func mergeINI(text string, set []iniSection) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, sec := range set {
		header := "[" + sec.name + "]"
		start, end := -1, len(lines)
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if start < 0 {
				if t == header {
					start = i
				}
				continue
			}
			if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
				end = i
				break
			}
		}
		if start < 0 {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			lines = append(lines, header)
			start, end = len(lines)-1, len(lines)
		}
		var missing []string
		for _, kv := range sec.keys {
			line := kv[0] + "=" + kv[1]
			found := false
			for i := start + 1; i < end; i++ {
				if k, _, ok := strings.Cut(lines[i], "="); ok && strings.TrimSpace(k) == kv[0] {
					lines[i] = line
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, line)
			}
		}
		// After the section's last setting, so its blank separator line stays
		// in front of the next header.
		at := end
		for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		lines = append(lines[:at], append(missing, lines[at:]...)...)
	}
	return strings.Join(lines, "\n") + "\n"
}
