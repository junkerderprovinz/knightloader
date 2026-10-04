# Privacy policy: KnightLoader browser extension

Last updated: 2 October 2026. Applies to version 1.4.0 and later, until this
date changes.

## The short version

The extension sends what you choose to send (a link, an image address, selected
text, the current page, or links you paste, drop or load into its collector) to
KnightLoader instances that you run yourself. Two features, off until you switch
them on, also hand browser downloads over to your instance and list the video
and audio a page plays so you can send them. It has no analytics, no
advertising, no accounts and no tracking.

It reaches your instances through a relay operated by the party named under "Who
is responsible". The relay passes messages along but cannot read them, because
they are encrypted with a key that only your own devices hold. It does see that a
browser is connected, from which IP address, and which instance a message is
addressed to.

## What is stored in your browser

Everything below is kept in the browser's own extension storage, in your browser
profile. We back none of it up, and removing the extension removes all of it.

- Your connection phrase: the twelve words your KnightLoader instances share.
  It is kept in the extension's own database, which the scripts the extension
  runs on websites for Click'n'Load cannot read.
- Which of your instances is the default target.
- Settings: interface language, whether Click'n'Load interception is on, the
  Click'n'Load countdown length, whether the clipboard watch is on and why it
  last switched itself off, and whether the "pin the extension" hint has been
  shown.
- Whether taking over downloads and finding media are on, and the rules for
  taking over downloads: the file types, the minimum size, the sites to leave
  alone and the key that keeps a download in the browser.
- A random ID for this browser on your group's list of clipboard watchers,
  created the first time you switch the watch on.
- Appearance: theme, corner shape, accent colour and rainbow palette, whether to
  follow an instance's appearance, and, while you follow one, a copy of your own
  appearance settings so they can be restored.

While a send waits in the popup, for you to pick an instance or for the
Click'n'Load countdown, it is held in the browser's session storage: what is being
sent (the link, image address, selected text or page address with the page title,
or the links and package name of a caught Click'n'Load button), which instance is
the default, and the list of your instances with their names. The popup deletes
that entry as soon as it reads it, and the browser clears session storage when it
closes.

While finding media is on, the session storage also holds, for each open tab, the
address of the page and the addresses of the video and audio streams it loaded,
up to 30 per tab. A new page in the tab starts its list afresh, closing the tab
deletes it, and switching the feature off deletes all of them.

In Firefox, while the clipboard watch is on, session storage also holds a short
checksum of the last text read from the clipboard, so the watch can tell a new
copy from an old one after Firefox pauses the extension. The text itself is not
stored.

## What leaves your browser

### To the relay

The relay is `parleyport.halleluja.design`, on a server in Germany. Every connection
to it carries:

- A group key derived from your phrase with a one-way hash. It cannot be turned
  back into the words, and the phrase itself never leaves the browser. Whoever
  presents this key joins your group, so it works like a password for the group.
- A random ID of 40 hexadecimal characters for this connection, and the ID of
  the instance a message is for, which the relay needs for routing. The browser
  picks a new ID for every connection and stores none of them, so the ID does
  not link one connection to the next.
- Encrypted messages (AES-GCM, with a second key derived from your phrase that the
  relay never receives). The relay cannot read their content, which includes the
  links you send and the names of your instances. An instance still on
  KnightLoader 1.0.0 is the exception: it sends its own name unencrypted, which
  version 1.1.0 and later no longer do.
- Your IP address, as with any connection on the internet.

The relay keeps no record of who connects or what they send, and its error log
never contains IP addresses. The one exception is a connection that fails the
relay's own handshake, for example one that closes or stalls before it identifies
itself (a scanner, or a popup closed while it was connecting): its IP address is
held in memory to slow down repeated failures, and deleted within 61 minutes of
that address's last failed attempt.

### To your own instances

These travel through the relay, and only your instances can read them:

- When you send something from a page: the link, image address, selected text or
  page address you chose, and the page title, which your instance uses as the
  package name.
- When a Click'n'Load button is caught (see below): the links decoded from that
  button's submission, and the package name the site gave or else the page title.
- When you use the popup's link collector: the links found in text you paste or
  drop there, or in files you pick or drop there. The browser reads a file
  locally; only the links it contains are sent, never the file, under a fixed
  package name ("From the browser", in the extension's language) rather than a
  page title.
- When taking over downloads is on and a download matches your rules: the
  download's address, the cookies your browser holds for that address, the page
  the download came from, your browser's user agent and the file name your
  browser gave the download. Before that, the
  extension asks your instances for their own web addresses, so a download from
  one of them is never handed back. Your instance uses the cookies for that one
  download, keeps them in memory only, never writes them to its database or its
  log, and drops them when the download finishes or is removed.
- When you send a stream from the popup's media list: the stream's address, the
  page it played on and that page's title, the cookies your browser holds for the
  stream's address, and your browser's user agent, under the same rules. Cookies
  from a private window are never sent.
- While the popup or options page is open: requests for your instances' queue
  status and web addresses, and a request for an instance's appearance settings
  if you chose to follow them.
- While the clipboard watch is on (see below): the links in text you copy
  anywhere on your computer, sent to your default instance. Once a minute, this
  browser's watcher ID and a label for the browser and system, such as
  "Firefox, Windows", so your other devices can tell you this browser is
  watching. When you switch the watch on, a request for the group's list of
  watchers and, if you choose to, a request to switch another device's watch
  off.

The extension makes no other network requests.

## The clipboard watch

The watch is off until you switch it on in the options, and your browser asks
you then whether the extension may read the clipboard. While it is on, the
extension reads the clipboard about once a second. Text that contains a link
has its links sent to your default instance; the rest of that text, and any
text without a link, stays in the browser and is not kept. Switching the watch
off, or taking the clipboard permission away in the browser's settings, stops
the reading.

## What happens inside the pages you visit

This section is about one feature, Click'n'Load. It runs only after you allow it,
and you can switch it off again in the options.

Click'n'Load is how a website hands a list of links to a download manager: its
button sends the list to `http://127.0.0.1:9666`, the address JDownloader listens
on. Such a button can be on any site, so to catch it the extension has to run a
small script in every page and frame. That needs access to all websites, which
the extension does not get at install. Your browser asks for it when you switch
Click'n'Load on, and you can say no.

While Click'n'Load is on:

- The script checks where the page is about to send something: fetch,
  XMLHttpRequest, form submissions, beacons, window.open, clicks on links, and
  addresses given to iframe, image and script elements. In a window the page opens
  on its own site, it also checks fetch, XMLHttpRequest and form submissions.
  Anything aimed at `127.0.0.1:9666` or
  `localhost:9666` is stopped, its link list is handed to the extension, and the
  page is told it succeeded. Everything else is passed on unchanged, and nothing
  about it is kept or sent.
- The script sets two global variables, `jdownloader` and `version`, which sites
  read to decide whether to show a Click'n'Load button at all.
- For sites that decide by loading `http://127.0.0.1:9666/jdcheck.js`, a
  declarative network rule answers that request with a file bundled in the
  extension.

Apart from a submission aimed at that address, the script does not read page
content, form fields, passwords or cookies, and it does not change how a page
looks.

Switching Click'n'Load off removes the script, switches the network rule off and
gives the access to all websites back to the browser. From then on, no code from
this extension runs in pages you open or reload. A tab that was already open keeps
the script until you reload it, but a button caught there is no longer sent
anywhere. The same happens when you take the access away in the browser's own
extension settings.

## Taking over downloads

This feature is off until you switch it on in the options, and switching it on asks
your browser for access to downloads, cookies, notifications and all websites.

When a download starts, the extension checks it against your rules in the
browser. A download from this computer, your local network or a private window
always stays in the browser, and so does one you start while holding the key you
chose. A download that matches is held while your instance is asked, and only
cancelled once the instance has the link. If anything fails, or the instance
holds the link back as already downloaded or filtered, the browser carries on
with it. A short notification says which instance took it.

While the feature is on, a small script runs in every page. It notices when you
press a mouse button while holding Alt, Shift, Ctrl or Cmd, and tells the extension
which of those keys were held. It reads nothing else in the page.

## Finding media

This feature is off until you switch it on in the options, and switching it on asks
your browser for access to all websites, the requests they make, and cookies.

While it is on, the extension looks at the address and content type of the
responses pages receive, in the browser, to pick out video, audio and HLS or DASH
playlists. It keeps those in session storage as described above and ignores
everything else. Nothing leaves the browser until you press a stream's send
button in the popup.

## Permissions

| Permission | Used for |
| --- | --- |
| `activeTab` | Reading the address and title of the tab you are on, and only when you press the toolbar button or pick a right-click entry there. |
| `contextMenus` | The four right-click entries: send link, image, selection, page. |
| `storage` | The settings listed above. |
| `scripting` | Adding and removing the Click'n'Load script. |
| `declarativeNetRequest` | Answering the `127.0.0.1:9666/jdcheck.js` probe while Click'n'Load is on. |
| Access to all websites (optional) | Running the Click'n'Load script in pages that may carry a button, and answering the `jdcheck.js` probe. Asked for only when you switch Click'n'Load on, and given back when you switch it off. |
| `clipboardRead` (optional) | Pasting your phrase with the paste button, and the clipboard watch. Requested when you press the button or switch the watch on. |
| `offscreen` | In Chrome and Edge, a hidden extension page that reads the clipboard while the watch is on, since the background script cannot. |
| `alarms` | Renewing this browser's place on the list of clipboard watchers once a minute, and in Firefox waking the watch after the browser paused it. |
| `downloads` (optional) | Seeing a download start, holding it while your instance is asked, and cancelling it once the instance has it. Asked for when you switch taking over downloads on, and given back when you switch it off. |
| `cookies` (optional) | Reading the cookies for the one address being handed over, so a download behind a login works on your instance. Asked for with taking over downloads or finding media. |
| `notifications` (optional) | The short note that names the instance a download went to. Asked for with taking over downloads. |
| `webRequest` (optional) | Seeing which video and audio streams a page loads. Asked for with finding media, and given back when you switch it off. |
| Access to all websites (optional) | Also needed for the cookies of any download site and for the media a page loads. Kept while any of Click'n'Load, taking over downloads or finding media is on. |

## What the extension does not do

- It has no analytics, telemetry or crash reporting.
- It shows no advertising, and it does not sell data or share it with anyone.
- It does not record your browsing history beyond the media list of each open tab
  described above, and a page's address leaves your
  browser only when you send that page or something on it.
- It runs no remote code. Everything the extension runs ships inside the package.

## Your choices and your data

- **See what is stored:** the options page shows your phrase (behind the eye
  button), your default instance and every setting.
- **Switch Click'n'Load off** in the options, as described above.
- **Switch taking over downloads or finding media off** in the options. Each gives
  back the permissions no other feature still uses.
- **Switch the clipboard watch off** in the options, or from another device
  when it asks whether to keep both.
- **Leave the group** with the bin button next to the phrase: the phrase, the
  default instance are deleted.
- **Remove the extension** to delete everything it stored.
- **Relay data:** while a connection is open, the relay holds what it needs to
  route it (your IP address, the group key, the connection's ID and the encrypted
  messages passing through) and drops all of it when the connection closes.
  Beyond that it holds nothing about you except the rate-limit entry described
  above, which is deleted within 61 minutes of that address's last failed
  attempt. For any question about it, or to exercise your rights, write to the
  contact address below.

## Who is responsible

The relay and this policy are the responsibility of:

Georg Düringer (Halleluja Design)
privacy@halleluja.design

The relay runs on a server rented from Hetzner Online GmbH, Germany, which
processes this data on our behalf.

Under the EU General Data Protection Regulation, forwarding your messages rests on
Art. 6(1)(b), because it is the service you use the extension for, and the
short-lived rate-limit entry rests on Art. 6(1)(f), our interest in keeping the
relay available. You have the right to access, correct and delete your data, to
restrict or object to its processing, to receive it in a portable format, and to
complain to a data protection supervisory authority.

## Children

The extension is a tool for operating your own server software and is not directed
at children.

## Changes

When this policy changes, the date at the top changes with it, and every change is
visible in the repository's history.

## Source

The extension is free software under the AGPL-3.0. Everything described here can
be checked against the source:
https://github.com/junkerderprovinz/knightloader/tree/main/extension/src
