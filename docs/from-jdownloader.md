# Moving over from JDownloader

KnightLoader can read the settings folder of a JDownloader 2 install and take
over what it can use. The card for this is under Settings, General, "Move over
from JDownloader". JDownloader's folder is only read. Nothing in it is changed,
so JDownloader keeps working as before.

## Pointing it at the folder

JDownloader keeps its settings in the `cfg` folder of its install folder. There
are two ways to hand it over:

- **Upload a zip.** Zip the `cfg` folder, or the whole install folder, and pick
  the zip with "Choose zip". This works from any browser, whatever machine
  KnightLoader runs on.
- **A path on the KnightLoader machine.** Type the path of the `cfg` folder, the
  JDownloader folder above it or a zip of either, and press Read. In a container
  the path is one inside the container, so mount JDownloader's folder there
  first, for example the folder JDownloader's own container uses as `/config`.

The account list in that folder holds your passwords, so delete the zip once
you are done with it.

## What you see before anything is written

Reading the folder writes nothing. A preview lists everything it found, grouped,
with a switch per item. What would change something is switched on, except an
account or rule that is off in JDownloader, a hoster login that would replace
one stored here, and the download folder. Items that cannot come over are
listed too, with the reason. Take over writes only what is switched on, and the
card then says how much came over and lists what stayed behind and why.

The preview stays valid for half an hour. KnightLoader keeps the four newest
previews, so a fifth read pushes out the oldest. The passwords stay on the
server during that time and never reach the browser.

## What comes over, and how

| From JDownloader | In KnightLoader |
|---|---|
| Hoster accounts | A hoster login on the Accounts page, which the built-in JDownloader backend then uses. KnightLoader keeps one login per hoster: the first account switched on in JDownloader comes over, and the other accounts for that hoster stay behind. |
| Debrid and multihoster accounts | An account of KnightLoader's own client for that service. A service that already has an account here gets the imported one as a second, named account, so a key you already use is never overwritten. |
| Packagizer rules | Packagizer rules, added after the ones you have. |
| Link filter rules | Link filter rules. Exceptions go to the top, the filter rules to the bottom, and the filter is set to stop at the first rule that matches, so an exception keeps its links as it did in JDownloader. |
| Extraction passwords | Added to the archive password list, without duplicates. |
| Default download folder | The download folder, if you switch it on. It is a path on the machine JDownloader ran on, so check it exists here. A path that does not fit this machine stays behind on its own. |
| Download list | Each package goes into the link collector with its name, comment and archive password, and waits there. Nothing starts until you start it. A package with several archive passwords gets the first one, and all of them go into the archive password list. |

An account or rule that is off in JDownloader arrives switched off. So do all
Packagizer rules when the Packagizer as a whole was off.

### Debrid services

JDownloader stores the API key in the account for TorBox, AllDebrid,
Premiumize.me, Zevera, Offcloud, BestDebrid, CocoLeech, CoolDebrid, Deepbrid and
FakirDebrid, and KnightLoader takes it from there. Linksnappy, DebridItalia,
Mega-Debrid, MultiUp, MyDebrid, NeoDebrid, ProLeech and RPNet use the user name
and password JDownloader stores.

Real-Debrid and Debrid-Link cannot come over. JDownloader signs in to both
through a login that hands it a token that expires, and keeps no API key. Add
the key from the service's own page on the Accounts page instead. The same goes
for an older Offcloud, CoolDebrid or Deepbrid account that still holds a website
password instead of a key.

Multihosters KnightLoader has no client for stay behind.

### Rules

A rule comes over only when KnightLoader can test everything it tests, because
a rule missing a condition would match links it never matched in JDownloader.
These conditions have no counterpart here and keep the rule out:

- where a link came from, its online or plugin status, its comment, and whether
  it is a duplicate
- "does not contain" or "does not equal" with a wildcard or a regular expression
  (a plain "does not contain" comes over)
- a size outside a range, and "is not" of a file type
- a size of exactly 0 bytes, which here looks the same as a size not known yet
- a regular expression only Java can read, such as one with a lookbehind

Plain text, wildcards and regular expressions keep JDownloader's matching:
case does not matter unless the pattern says so. A size range that starts at
0 bytes starts at 1 byte here, so a link whose size is not known yet does not
match it, as in JDownloader. JDownloader's own built-in
rules stay behind. The one that puts each package in its own folder is the
switch "Put each package in its own subfolder" under Settings, Downloads.

What a Packagizer rule sets comes over as far as KnightLoader has it: package
name, download folder, comment, priority, connections per file (at most 16) and
auto-extract. Placeholders are rewritten to KnightLoader's: `<jd:source:1>`
becomes `<jd:match:source:1>`, `<jd:orgfilename:1>` becomes
`<jd:match:filename:1>`, and so on. A placeholder KnightLoader does not have,
such as `<jd:subfolderbyplugin>` or `<jd:env:...>`, leaves that one field out.
Starting, force-starting, moving to the download list, enabling or disabling a
link, renaming or moving after the download, and skipping the rules below have
no rule action here; the preview names what a rule loses. A new file name stays
in the rule and shows in the rule test, but downloads do not use it yet. A rule
left with nothing to do stays behind.

### Download list

KnightLoader reads the newest download list that opens, as JDownloader does.
Finished links, disabled links and links whose address only a JDownloader
plugin understands stay behind. So do links from a protected container, whose
addresses JDownloader keeps hidden; add the container again. Each package's
folder is not taken over, since it is a path on JDownloader's machine. The links
land where KnightLoader's download folder and rules put them.

## What is not read

Captcha service keys, proxies, reconnect scripts and the link grabber list are
not read. A LiveHeader reconnect script can be pasted on the Reconnect page,
which reads JDownloader's format.
