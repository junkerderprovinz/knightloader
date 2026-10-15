# Configuration

Almost everything is set in the web interface and stored in the data
directory. The environment variables below cover what the process needs before
the interface is up, plus a few service keys that can be set here instead of on
the Accounts page. None of them is required.

| Var | Default | Meaning |
|---|---|---|
| `KL_ADDR` | `:8749` | listen address |
| `KL_BASE_PATH` | | path the interface and the API are served under behind a reverse proxy, e.g. `/kl` for `https://example.com/kl/`. Unset means the root, or the prefix a proxy sends in `X-Forwarded-Prefix`; see [Behind a reverse proxy](installing.md#behind-a-reverse-proxy) |
| `KL_DATA` | user config dir | data directory (database, settings, accounts, session key) |
| `KL_APP_KEY` | | 64 hexadecimal characters, made once with `openssl rand -hex 32`. The password is then hashed with Argon2id peppered with it, and the secrets in the data directory and the backup archive are sealed with it, so a copy of the data directory alone opens nothing. Without it everything is stored as before. Keep it: KnightLoader does not start on a sealed data directory without the same key. See [App key](installing.md#app-key) |
| `KL_YTDLP` | `yt-dlp` (PATH) | path to the yt-dlp binary; media links route through it when present. A copy fetched by the Updates card under Settings, General or by its daily update is started ahead of this one, and that card says so and offers to remove it again |
| `KL_TORBOX` | | TorBox API key. The Accounts page is the better place: it stores the key encrypted and applies it without a restart |
| `KL_ALLDEBRID` | | AllDebrid API key, as above |
| `KL_REALDEBRID` | | Real-Debrid API token, as above |
| `KL_DEBRIDLINK` | | Debrid-Link API key, as above |
| `KL_PREMIUMIZE` | | Premiumize.me API key, as above |
| `KL_OFFCLOUD` | | Offcloud API key, as above |
| `KL_JD` | | headless JDownloader API URL, e.g. `http://jd:3128`; the catch-all for hoster links nothing else claims |
| `KL_CNL` | `9666` | Click'n'Load listener port on `127.0.0.1`; `0` disables it |
| `KL_PROVISION_JD` | `1` | provisions a private headless JDownloader on first run and uses it as the hoster catch-all; `0` opts out, and it is skipped whenever `KL_JD` is already set |
| `UMASK` | the container's own, usually `022` | octal mask for every file and folder KnightLoader and the programs it starts create, read the way linuxserver.io images read it: `000`, `002`, `022`. Downloads are created 0666 and folders 0777 before the mask. `000` is the Unraid convention and lets the SMB account move and delete downloads. A value that is not octal is logged and ignored |
| `PUID` | `1000` | the user id KnightLoader runs as. The container starts as root, gives the empty folders Docker has just created to `PUID`:`PGID` and then switches to that account. `0` and a value that is not a number stop the start. Not read when the container is started with `--user`, as on Unraid; see [Installing](installing.md#as-a-container) |
| `PGID` | `1000` | the group id KnightLoader runs as, as for `PUID` |

## Services without a variable

Linksnappy and the smaller multihosters (BestDebrid, CocoLeech, CoolDebrid,
DebridItalia, Deepbrid, FakirDebrid, Mega-Debrid, MultiUp, MyDebrid, NeoDebrid,
ProLeech, RPNet and Zevera) are entered on the Accounts page only. Linksnappy,
DebridItalia, Mega-Debrid, MultiUp, MyDebrid and NeoDebrid take the login the
website takes; ProLeech and RPNet take the two values their API page shows. The full
list of services, and where each one issues its key, is
[`internal/accounts/catalogue.go`](https://github.com/junkerderprovinz/knightloader/blob/main/internal/accounts/catalogue.go).

## Trusted proxies

KnightLoader takes five failed sign-ins a minute from one address and refuses
the next until the first of them is a minute old. The right password is
refused too while the address waits, and a wrong password gets the same answer
however close it was. Passwords and two-factor codes share one count, passkeys
and reset codes have one each. Behind a reverse proxy every request arrives
from the proxy, so one person guessing makes everybody wait.

**Trusted proxies** under Settings > Security, beside the password, fixes that.
The field shows up once a request has come through a proxy in the last day,
told by its `X-Forwarded-For`, `Forwarded` or `X-Real-IP` header, and stays
while the list has entries.
Enter the proxy's address, one IP address or CIDR range per line, such as
`172.18.0.5` or `10.10.0.0/28`. For a request from one of them, KnightLoader
counts the right-most address in `X-Forwarded-For` that is not itself a
trusted proxy. List only proxies you run: every machine on the list can put
any address it likes into that header, and with it guess without ever being
made to wait. A range must cover your proxies and no one who signs in. If
KnightLoader's port is also published directly, leave out the gateway of its
Docker network, because a visitor on that port can arrive from the gateway.
KnightLoader refuses a range wider than `/28` for IPv4 or `/124` for IPv6,
sixteen addresses either way, so a whole home network such as a `/24` never
counts as a proxy. A wider range an
earlier version took stays in the list, is not used, and is named under the
field until you change it.
Empty, the default, means every request counts for the address it connected
from. An IPv6 caller counts by its `/64`, since one connection usually gets
that many addresses, and a whole `/48` together gets 20 failed sign-ins a
minute. At most two password checks run at once, and the rest wait. The relay an instance serves under Pairing reads the same
list for its own count of failed handshakes.

## A forgotten password

**Forgot password?** on the sign-in page has KnightLoader write a code where
only somebody with access to the machine can read it:

- In a container, to the container log. On Unraid: Docker tab, KnightLoader
  icon, **Logs**. Elsewhere `docker logs` followed by the name of the container.
- A server started from a terminal or as a service, to its own output: that
  terminal, or the service's journal (`journalctl -u` followed by the name of
  the service).
- The desktop app, and a server whose output goes nowhere, to the file
  `password-reset-code.txt` in the data directory. On Linux and macOS only
  the user KnightLoader runs as can read it; on Windows it takes the
  permissions of the data directory, which by default is your own profile.
  The file is removed once the code is used or has run out.

Enter the code and a new password on the sign-in page and you are signed in. A
code is valid for 10 minutes, and a new one is written at most once a minute.
It has twelve characters, so wrong guesses do not use it up, and nobody can
keep you from resetting by guessing. The new password switches two-factor
authentication off, removes every passkey and every API token, signs out every
other browser and replaces the key sessions are signed with, so a copy of
`auth.json` taken earlier opens nothing either. An instance in a pairing group
leaves it: it gets new connection words that nobody else holds, a relay key
entered by hand is removed, and every phone, browser extension and other
instance has to pair again with the new words, which Settings > Pairing shows
once you are signed in. For 30 days it tells the devices that still hold the
old words that it was reset, in an answer signed with its old key, and on a
relay it serves itself the old words reach this instance and nobody else (see
[Connecting](connecting.md)). The page after the reset lists what
was removed.
Settings that run something, such as event
programs, scripts and the end-of-queue action, are left as they are: if
somebody else may have had access, look through them.

The code is never sent over the network, and it appears neither on the log
page, in the log file nor in a diagnostics bundle. Somebody who only reaches
the web interface cannot reset anything.

Anybody who can read the container log or the service's output can reset the
password and take the instance over. That is root on the host, anybody who can
use the Docker socket, even only to read from it, and access that is otherwise
read-only: a log viewer such as Dozzle, or a log shipper and everybody who
reads what it collects. Keep that access to people who may own this instance.
