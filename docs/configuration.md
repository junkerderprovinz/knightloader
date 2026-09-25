# Configuration

Almost everything is set in the web interface and stored in the data
directory. The environment variables below cover what the process needs before
the interface is up, plus a few service keys that can be set here instead of on
the Accounts page. None of them is required.

| Var | Default | Meaning |
|---|---|---|
| `KL_ADDR` | `:8749` | listen address |
| `KL_DATA` | user config dir | data directory (database, settings, accounts, session key) |
| `KL_YTDLP` | `yt-dlp` (PATH) | path to the yt-dlp binary; media links route through it when present. A copy fetched by the Resolvers settings page ("keeping yt-dlp current") is started ahead of this one, and that page says so and offers to remove it again |
| `KL_TORBOX` | | TorBox API key. The Accounts page is the better place: it stores the key encrypted and applies it without a restart |
| `KL_ALLDEBRID` | | AllDebrid API key, as above |
| `KL_REALDEBRID` | | Real-Debrid API token, as above |
| `KL_DEBRIDLINK` | | Debrid-Link API key, as above |
| `KL_PREMIUMIZE` | | Premiumize.me API key, as above |
| `KL_OFFCLOUD` | | Offcloud API key, as above |
| `KL_JD` | | headless JDownloader API URL, e.g. `http://jd:3128`; the catch-all for hoster links nothing else claims |
| `KL_CNL` | `9666` | Click'n'Load listener port on `127.0.0.1`; `0` disables it |
| `KL_PROVISION_JD` | `1` | provisions a private headless JDownloader on first run and uses it as the hoster catch-all; `0` opts out, and it is skipped whenever `KL_JD` is already set |

## Services without a variable

Linksnappy and the smaller multihosters (BestDebrid, CocoLeech, CoolDebrid,
DebridItalia, Deepbrid, FakirDebrid, Mega-Debrid, MultiUp, NeoDebrid, ProLeech,
RPNet and Zevera) are entered on the Accounts page only. Linksnappy,
DebridItalia, Mega-Debrid, MultiUp and NeoDebrid take the login the website
takes; ProLeech and RPNet take the two values their API page shows. The full
list of services, and where each one issues its key, is
[`internal/accounts/catalogue.go`](https://github.com/junkerderprovinz/knightloader/blob/main/internal/accounts/catalogue.go).
