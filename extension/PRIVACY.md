# Privacy policy: KnightLoader browser extension

Last updated: 16 September 2026. Applies to version 1.23.0 and later, until this
date changes.

## The short version

The extension sends what you choose to send (a link, an image address, selected
text, or the current page) to KnightLoader instances that you run yourself. It
has no analytics, no advertising, no accounts and no tracking.

It reaches your instances through a relay operated by this project. The relay
passes messages along but cannot read them, because they are encrypted with a key
that only your own devices hold. It does see that a browser is connected, from
which IP address, and which instance a message is addressed to.

## What is stored in your browser

Everything below is kept in the browser's own extension storage, in your browser
profile. We back none of it up, and removing the extension removes all of it.

- Your connection phrase: the twelve words your KnightLoader instances share.
- A random browser ID of 40 hexadecimal characters, generated on first use. It
  contains nothing about you or your device. The relay uses it to recognise a
  reconnect from this browser as the same member of your group.
- Which of your instances is the default target.
- Settings: interface language, whether Click'n'Load interception is on, the
  Click'n'Load countdown length, and whether the "pin the extension" hint has
  been shown.
- Appearance: theme, corner shape, accent colour and rainbow palette, whether to
  follow an instance's appearance, and, while you follow one, a copy of your own
  appearance settings so they can be restored.

While a send waits for you to pick an instance, the link, its title and the list
of your instances are held in the browser's session storage. The popup deletes
that entry as soon as it reads it, and the browser clears session storage when it
closes.

## What leaves your browser

### To the relay

The relay is `relay.halleluja.design`, operated by this project on a server in
Germany. Every connection to it carries:

- A group key derived from your phrase with a one-way hash. It cannot be turned
  back into the words, and the phrase itself never leaves the browser.
- Your random browser ID and the ID of the instance a message is for, which the
  relay needs for routing.
- Encrypted messages (AES-GCM, with a second key derived from your phrase that the
  relay never receives). The relay cannot read their content, which includes the
  links you send and the names of your instances.
- Your IP address, as with any connection on the internet.

The relay keeps no record of who connects or what they send, with two exceptions,
both about failed connections. When a connection fails the relay's own handshake,
its IP address is held in memory for rate limiting, for at most one hour. When a
connection fails the TLS handshake before it reaches the relay (usually a
scanner), the web server library writes the IP address to the server's system log.

### To your own instances

These travel through the relay, and only your instances can read them:

- When you send something: the link, image address, selected text or page address
  you chose, and the page title, which your instance uses as the package name.
- When a Click'n'Load button is caught (see below): the links decoded from that
  button's submission, and the package name the site gave or else the page title.
- While the popup or options page is open: requests for your instances' queue
  status and web addresses, a request to pause or resume a queue when you press
  that button, and a request for an instance's appearance settings if you chose to
  follow them.

The extension makes no other network requests.

## What happens inside the pages you visit

This section is about one feature, Click'n'Load, which is on by default and can be
switched off in the options.

Click'n'Load is how a website hands a list of links to a download manager: its
button sends the list to `http://127.0.0.1:9666`, the address JDownloader listens
on. Such a button can be on any site, so to catch it the extension has to run a
small script in every page and frame. That is why it asks for access to all
websites when you install it.

While Click'n'Load is on, that script does three things:

- It checks where the page's own requests are going (fetch, XMLHttpRequest, form
  submissions and beacons). A request aimed at `127.0.0.1:9666` or
  `localhost:9666` is stopped, its link list is handed to the extension, and the
  page is told it succeeded. Every other request is passed on unchanged, and
  nothing about it is kept or sent.
- It sets two global variables, `jdownloader` and `version`, which sites read to
  decide whether to show a Click'n'Load button at all.
- For sites that decide by loading `http://127.0.0.1:9666/jdcheck.js`, a
  declarative network rule answers that request with a file bundled in the
  extension.

Apart from a submission aimed at that address, the script does not read page
content, form fields, passwords or cookies, and it does not change how a page
looks. Switching Click'n'Load off unregisters the scripts, so no code from this
extension runs in the pages you visit until you switch it back on.

## Permissions

| Permission | Used for |
| --- | --- |
| `contextMenus` | The four right-click entries: send link, image, selection, page. |
| `storage` | The settings listed above. |
| `scripting` | Registering and unregistering the Click'n'Load scripts. |
| `declarativeNetRequest` | Answering the `127.0.0.1:9666/jdcheck.js` probe. |
| Access to all websites | Running the Click'n'Load script in pages that may carry a button, and reading the current tab's address and title when you press send in the toolbar popup. |
| `clipboardRead` (optional) | Pasting your phrase with the paste button. Requested only when you press it, and read only then. |

## What the extension does not do

- It has no analytics, telemetry or crash reporting.
- It shows no advertising, and it does not sell or share data with anyone.
- It does not read or record your browsing history. It sees a page's address only
  when you send that page or something on it.
- It runs no remote code. Everything the extension runs ships inside the package.

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

## Contact

privacy@halleluja.design
