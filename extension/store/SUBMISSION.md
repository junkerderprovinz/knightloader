# Store submission: KnightLoader browser extension

This file holds what the Chrome Web Store, Microsoft Edge Add-ons and Firefox
Add-ons (AMO) ask for when the extension is listed, in the order their dashboards
ask for it.

One package serves all three stores. Submit the first version by hand in each
dashboard. Edge's API only updates a product that already exists in Partner
Center. Chrome's current API (v2) only uploads to an existing item, and a new item
cannot be published before its Store listing and Privacy tabs are filled in the
dashboard. AMO's API could create the add-on, but setting up all three listings
the same way keeps the answers in one place. Later updates can be uploaded through
the APIs (see the last section), and every update is reviewed again.

Brave, Opera and Vivaldi install from the Chrome Web Store and need no submission
of their own. On Firefox the add-on is offered for desktop only (see the Firefox
section).

## Before submitting

Do these in order. The stores compare the dashboard answers with the privacy
policy at its URL and with what the extension does, so nothing below may be
skipped or reordered.

1. **The operator** is named in `extension/PRIVACY.md` ("Who is responsible"),
   with the contact address and no postal address: Art. 13 GDPR asks for the
   controller's identity and contact details, and a street address is not among
   them. Add one if a supervisory authority or a store ever asks for it. Accept
   Hetzner's data processing agreement in the Hetzner Cloud console if that has
   not been done.
2. **Deploy the relay** built from the same commit to `relay.halleluja.design`.
   The policy describes the running relay: rate-limit entries deleted within 61
   minutes, no IP addresses in its log. Older relay builds do neither, and what
   they wrote stays in the journal after the deploy, so as root on the relay host:
   - Check that the new build runs: `GET /health` reports this commit's version.
   - Clear what older builds wrote: `journalctl --rotate --vacuum-time=1s`, as one
     command so the files just rotated go too. journald cannot delete a single
     unit's lines, so this clears the whole host journal.
   - If rsyslog is installed (`dpkg -s rsyslog`), remove the relay's lines from
     `/var/log/syslog*` and `/var/log/daemon.log*` as well.
   - Check that nothing is left: `journalctl -u <relay unit> -o cat | grep -E
     '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b|\[[0-9A-Fa-f]*:[0-9A-Fa-f:.]*(%[^]]*)?\]'`
     prints nothing. `-o cat` drops the `knightloader-relay[812]:` prefix, and the
     pattern needs a colon inside the brackets, so the relay's own `[address]`
     placeholder does not match.

   Until this is done, "its error log never contains IP addresses" and "holds
   nothing about you beyond" in the policy are not true yet.
3. **Merge to `main`.** The privacy policy URL points at `main`, which serves the
   old policy until the merge.
4. **Release.** The tag `extension/vX.Y.Z` on `main` publishes "Browser Extension
   X.Y.Z" once the release workflow has checked it against the manifest. Tag from
   a clone that has run `git fetch --prune --prune-tags origin`, confirm
   `git rev-parse extension/vX.Y.Z^{commit}` equals `origin/main`, and push that
   one tag, never `--tags`.
5. **Package:** the zip that release carries. It is `extension/src` zipped as it
   is, with no build step, so a reviewer can compare it file by file with the tag.
   Submit no other zip.
6. **Reviewer instance and files** (see "Reviewer notes"): run a dedicated
   instance named "Review" whose group holds that instance only, with a web UI
   password set and its download queue paused. It is up: `review.halleluja.design`
   serves the instance, `/test/` the test page and `/test/walkthrough.mp4` the
   video, all three through a Cloudflare tunnel because the relay holds port 443
   and the host firewall opens nothing else. Its queue is paused by a schedule
   rather than by hand, so a restart cannot start a download. What is left to
   fill in are `<PHRASE>` and `<WEBUI_PASSWORD>`: they go into the dashboard
   fields and nowhere else, never into the repository, because the phrase is the
   key to the reviewer group.

Reference:

- Privacy policy URL:
  https://github.com/junkerderprovinz/knightloader/blob/main/extension/PRIVACY.md
- Test page: `test-page/index.html`, built by `test-page/build.mjs`. A trailer link
  to right-click, and a Click'n'Load button for two other files, so the two sends
  do not overlap.
- Video: `review-walkthrough.mp4`, 47 seconds with captions, recorded against a
  throwaway instance on this test page: connect with a phrase, send the
  right-clicked trailer link, catch the Click'n'Load button, see both arrive.

## Listing (all three stores)

No browser is named in the listing texts or images below. Edge's policy 1.1.2
rejects a listing that references another browser, and the same text is used
everywhere.

**Name**: KnightLoader (read from the manifest)

**Short description** (read from the manifest, 132 characters at most in Chrome;
Edge shows it read-only):

> Send a link, a selection, or the current page straight to your own KnightLoader instance.

**Summary** (AMO, 250 characters at most):

> Send links, pages and Click'n'Load buttons from the browser to your own
> self-hosted KnightLoader download manager. Set up with one connection phrase;
> no account, and messages to your instances are end-to-end encrypted.

**Description** (Edge requires 250 to 10,000 characters):

> KnightLoader is a self-hosted download manager. This extension connects your
> browser to your own KnightLoader instances, so a link you find on a page goes
> into your download queue without copying and pasting.
>
> What it does:
>
> - Right-click a link, an image, selected text or the page itself and send it to
>   one of your instances.
> - Catch Click'n'Load buttons. Many download sites offer them to hand a list of
>   links to a download manager on the same computer. The extension catches the
>   button in the page and sends the links to your instance, wherever it runs.
> - See your instances in the toolbar popup: which ones are online, how many files
>   are queued, and a button to pause or resume each queue.
> - Paste or drop several links (or a file that contains them) into the popup's
>   collector and send them together.
>
> Setting it up takes one step: enter the twelve-word connection phrase your
> KnightLoader instances share. The extension stores no server address and no
> web interface password; the phrase is the only credential it keeps.
>
> You need a running KnightLoader instance. KnightLoader is free and open source:
> https://github.com/junkerderprovinz/knightloader
>
> Privacy: messages to your instances pass through a relay. They are end-to-end
> encrypted, so the relay cannot read the links you send. There are no analytics,
> no ads and no tracking. Click'n'Load is on by default and is the main reason the
> extension asks for access to all websites; the same access lets it read the
> address and title of a page you send. Switching Click'n'Load off in the options
> stops the extension from running code in the pages you open.

**Categories**

| Store | Category |
| --- | --- |
| Chrome Web Store | Productivity, then the closest sub-category the dashboard offers (Tools) |
| Edge Add-ons | Productivity |
| AMO | Download Management (desktop; no Android categories, the add-on is not offered there) |

**Links and contact**

| Field | Value |
| --- | --- |
| Homepage / website | https://github.com/junkerderprovinz/knightloader |
| Support site | https://github.com/junkerderprovinz/knightloader/issues |
| Support email | hello@halleluja.design |
| License (AMO) | GNU Affero General Public License v3.0 |

The Chrome Web Store asks for a Limited Use statement on a page one click from the
homepage. It is in `extension/README.md`, which the homepage README links to, and
deliberately not in the privacy policy, which Edge wants to be about its own
browser rather than another one.

**Graphics** (generated by `gen-store-assets.mjs` in this folder, screenshots in
`screenshots/`)

| File | Chrome Web Store | Edge Add-ons | AMO |
| --- | --- | --- | --- |
| `store-icon-128.png` | Store icon (required) | Minimum logo size | Taken from the package |
| `logo-300.png` | | Extension logo (required) | |
| `promo-small-440x280.png` | Small promo tile (required) | Small promo tile | |
| `promo-marquee-1400x560.png` | Marquee tile | Large promo tile | |
| `screenshots/*.png` (1280x800) | Screenshots, 1 to 5 | Screenshots, up to 6 | Screenshots |

## Privacy tab (Chrome Web Store and Edge Add-ons)

Both dashboards ask the same questions. Each justification below is under the
1,000-character limit.

**Single purpose**

> Send links, pages and Click'n'Load link lists from the browser to the user's
> own self-hosted KnightLoader download manager.

**Permission justifications**

`contextMenus`

> Adds four right-click entries: send a link, an image, selected text or the
> current page to KnightLoader. This is the main way people use the extension.

`storage`

> Keeps the user's settings in the browser: the connection phrase, a random ID for
> this browser within the user's group, the default instance, and interface
> settings (language, theme, Click'n'Load on or off, countdown length). While a
> send waits in the popup, it is held in session storage until the popup reads it.

`scripting`

> Registers and unregisters the two Click'n'Load content scripts at runtime. When
> the user switches Click'n'Load off in the options, the scripts are unregistered,
> so no extension code runs in pages opened or reloaded after that.

`declarativeNetRequest`

> One static ruleset with two rules. Before showing a Click'n'Load button, download
> sites load http://127.0.0.1:9666/jdcheck.js to check whether a receiver is
> present. The two rules answer exactly that request (for 127.0.0.1 and localhost)
> with a small script bundled in the extension, so the button appears and can be
> caught. No other request is matched, blocked or changed. The ruleset is switched
> off together with Click'n'Load.

Host permission `<all_urls>`

> Mainly for Click'n'Load. The buttons can be on any website, and the script that
> catches them has to run in the page before the site's own code, in every frame,
> including blank frames a site opens for the button. Apart from declaring the two
> globals sites check (jdownloader, version), it acts only on requests
> addressed to 127.0.0.1:9666 or localhost:9666: those are stopped and their link
> list is handed to the extension. Every other request passes through unchanged
> and is not recorded. Switching the feature off in the options removes the script
> and the redirect rule for pages opened after that. The same access lets the
> extension read the address and title of the current tab when the user presses
> "Send this page", and the page title for a right-click send, which is why it
> does not also request activeTab.

`clipboardRead` (optional permission)

> Requested only when the user presses the paste button next to the phrase field,
> to paste the twelve-word connection phrase. Not requested at install.

**Remote code**: No, I am not using remote code. Every script ships in the
package; messages from the relay are data and are never executed.

**Data usage**: tick these five, leave the rest unticked.

| Category | Why |
| --- | --- |
| Web history | The address and title of a page the user chooses to send, and the title of the page a link, image, selection or Click'n'Load batch is sent from. |
| User activity | The Click'n'Load script checks, in the browser, where each request, form submission and link click in a page goes, to catch those aimed at 127.0.0.1:9666. Nothing else about them is kept or sent. |
| Website content | Links, image addresses and selected text the user sends, links pasted or loaded into the popup's collector, and link lists from Click'n'Load buttons. |
| Authentication information | The connection phrase is a credential. It stays in the browser, but a key derived from it is sent to the relay to join the user's group. |
| Location | The IP address the relay receives on every connection. It is kept only after a failed relay handshake, in memory for rate limiting, and deleted within 61 minutes of that address's last failed attempt. |

Everything the extension sends goes to the user's own instances, through a relay
operated by the developer. Chrome's exemption for clients of user-specified servers
does not cover that, because the relay belongs to the developer; declaring the data
is the accurate answer.

**Certifications**: all three apply (no sale of user data, no use unrelated to the
single purpose, no use for creditworthiness or lending).

## Firefox (AMO) specifics

- **Data collection**: the manifest declares `authenticationInfo`,
  `browsingActivity` and `websiteContent` as required, which Firefox shows at
  install: the same three kinds of data the Chrome and Edge answers declare, in
  Mozilla's categories. Mozilla counts any data handled outside the browser,
  including data sent to the user's own server, and makes no exception for
  encryption; its add-on policy 6.2.1 requires the declaration to be accurate.
  Mozilla's location category does not cover IP addresses, so it is not declared.
  The dashboard answers must match the manifest.
- **Desktop only**: the manifest has no `gecko_android` key, so AMO lists the
  add-on for desktop Firefox only. Firefox for Android has no context-menu API and
  the extension has never been tested there. Do not add Android in the dashboard;
  with the key present, AMO locks that setting.
- **Minimum version**: Firefox 140, the first desktop version with
  `data_collection_permissions`. That keeps Firefox ESR 140. Before Firefox 149,
  `action.openPopup()` needs a user gesture, so a caught Click'n'Load button cannot
  open the popup there: the toolbar icon shows "…", and a click on it starts the
  countdown. The AMO notes say so.
- **Source code**: not needed. The scripts are plain, unminified files with no
  build step; `wordlist.js` is the BIP39 English word list turned into a JS array
  (the source file's hash is in its header).
- **Privacy policy**: tick "This add-on has a privacy policy" and paste the text of
  `extension/PRIVACY.md`, or link to it.

## Reviewer notes

The extension does nothing visible until it is connected to a KnightLoader
instance. Every store rejects an item a reviewer cannot test, so the review runs
against a dedicated instance whose group contains that one instance only. With a
single instance a right-clicked link is sent straight through without a picker.
Its only visible confirmation is a check mark on the toolbar icon, and a new
extension sits behind the puzzle-piece button until it is pinned, so every set of
notes says to pin it.

**Chrome Web Store** (Test instructions tab: username 100 characters, password 100,
additional instructions 500)

The phrase goes into the Username field, because the 500 characters of additional
instructions do not hold it together with three addresses. The text below carries
the real addresses and is under the limit as it stands. Count it again if it is
edited, including line breaks, and shorten the video link if it runs over,
because the last line is what the field cuts.

- Username: `<PHRASE>`
- Password: `<WEBUI_PASSWORD>`
- Additional instructions:

> Username = connection phrase, Password = web UI password.
> 1. Options page (opens on install): paste the phrase, press Connect. Pin the icon.
> 2. Open https://review.halleluja.design/test/, right-click the trailer link > Send link to KnightLoader. Icon shows a check.
> 3. Press the Click'n'Load button there; popup counts down, sends.
> 4. Both show up at https://review.halleluja.design > Link collector.
> Video: https://review.halleluja.design/test/walkthrough.mp4

**Edge Add-ons** (Notes for certification)

> This extension sends links to a self-hosted KnightLoader download manager, so it
> needs a running instance to do anything. We run one for certification.
>
> 1. Install the extension. Its options page opens. Show KnightLoader in the
>    toolbar (Extensions button in the toolbar, then the toolbar option next to
>    KnightLoader), because its only confirmation is a check mark on its icon.
> 2. Paste this connection phrase into the Remote access field and press Connect:
>    <PHRASE>
>    One instance, "Review", appears as Online.
> 3. Open https://review.halleluja.design/test/. Right-click the trailer link and choose "Send link to
>    KnightLoader". The toolbar icon shows a green check mark.
> 4. On the same page, press the "Click'n'Load: both films" button. The extension
>    popup opens, counts down from 5 and sends the two film links.
> 5. Open https://review.halleluja.design and sign in with the password <WEBUI_PASSWORD>. The links
>    from steps 3 and 4 are listed under Link collector.
>
> A recording of these steps: https://review.halleluja.design/test/walkthrough.mp4
>
> The instance only collects links; its download queue is paused and nothing is
> downloaded.

**AMO** (Notes for reviewers)

> Same steps as the video at https://review.halleluja.design/test/walkthrough.mp4:
>
> 1. Pin the add-on (Extensions button, gear next to KnightLoader, Pin to
>    Toolbar); its only confirmation is a check mark on that icon.
> 2. Open the add-on's options, paste this connection phrase into Remote access,
>    press Connect: <PHRASE>
> 3. Open https://review.halleluja.design/test/, right-click the trailer link, "Send link to KnightLoader".
>    The add-on's toolbar icon shows a check mark.
> 4. Press "Click'n'Load: both films" on that page; the popup counts down and
>    sends. Before Firefox 149 an add-on cannot open its popup without a click:
>    the icon shows "…" instead, and clicking it starts the countdown.
> 5. Check arrival at https://review.halleluja.design, password <WEBUI_PASSWORD>, Link collector.
>
> Notes on the code, which is unminified and has no build step:
>
> - `cnl-main.js` runs in the MAIN world. It wraps fetch, XMLHttpRequest,
>   HTMLFormElement.submit, navigator.sendBeacon and window.open. Inside
>   same-origin windows the page opens, it wraps only fetch, XMLHttpRequest and
>   HTMLFormElement.submit and adds a capture-phase submit listener. It redefines
>   the `src` setter of HTMLIFrameElement, HTMLImageElement and HTMLScriptElement,
>   and adds capture-phase `submit` and link `click` listeners. It only acts on
>   URLs whose host is 127.0.0.1:9666 or localhost:9666 (the Click'n'Load
>   protocol) and passes everything else to the original functions and setters.
>   It also sets `window.jdownloader` and `window.version` when the page has not,
>   because sites check them before showing a button. The `jk` field of a
>   submission is JavaScript supplied by the site; `cnl.js` extracts the hex key
>   from it with a regular expression and never evaluates it.
> - `relay.js` talks to the relay over one WebSocket per action. Payloads are
>   sealed with AES-GCM (WebCrypto) using a key derived from the phrase; the relay
>   sees the group key and routing IDs, never the content.
> - `wordlist.js` is the phrase word list and `i18n.js` holds 42 locales, which is
>   why both are large.

## After the first listing: updates

1. Bump `version` in `extension/src/manifest.json`, write
   `.github/release-notes/extension/vX.Y.Z.md`, push the tag `extension/vX.Y.Z`
   from `main`. The release workflow checks the tag against the manifest and
   attaches the zip.
2. Upload that zip to each store. Every upload is reviewed again: Chrome in a few
   days, Edge in up to seven business days, AMO usually within a day.
3. Users' browsers pick up a published update on their own.

The upload can be automated once each listing exists: Chrome Web Store API v2
(`upload`, `publish`) with a service account, the Edge Add-ons API v1.1 with an API
key, and `web-ext sign --channel=listed` with AMO API credentials. That needs the
three sets of credentials as repository secrets.
