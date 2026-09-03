# Privacy Policy — KnightLoader Browser Extension

Last updated: 28 August 2026

## The short version

This extension has no server of its own, and nothing it stores leaves your browser.
What it sends goes to a KnightLoader instance you run yourself.

One honest footnote, because the sentence above used to stop here and that was not
the whole truth: to reach an instance your browser has no direct route to, the
message travels through a RELAY, and unless you have pointed the extension at one
of your own, that relay is operated by this project. It cannot read what it
carries - see below - but it is a third party in the path, and it sees that you
are there. If that matters to you, run your own relay; the same twelve words work
against it.

## What is stored, and where

The extension stores five kinds of thing, using the browser's own extension
storage:

- **Your connection phrase** — the twelve words your own KnightLoader instances
  share. It is what lets this browser join your group, and it is the only thing
  that has to be entered.
- **Which instance in that group is the default.**
- **The interface language**, if you picked one instead of following the browser.
- **Whether Click'n'Load interception is switched on.**
- **Your appearance settings**: theme, corner shape, accent colour, and the
  rainbow palette.

That is the complete list. It lives in your browser profile. It is not backed up
by us, and removing the extension removes it. While a send is in flight, the link
and title you chose are held in the browser's session storage just long enough for
the send-to window to read them once, and are deleted on that read.

**The phrase itself never leaves your browser.** What is sent to the relay is a
key derived from it by a one-way hash, which cannot be turned back into the
words — that is what lets somebody else run a relay without being able to
reconstruct anybody's phrase, us included.

## What is sent, when, and to whom

When you press send — the toolbar button, or a right-click entry — the extension
sends the address of the link, image, selection, or page you chose, together with
its title, to **the instance you selected**. That instance is a server you run.
Nothing goes anywhere else.

It travels through a relay, which is what makes an instance reachable even when
your browser has no route to it. The relay carries the message without being able
to read it: everything except the routing — which instance it is for — is
encrypted with a second key derived from your phrase, and the relay is never given
that key.

The extension does not read the contents of the pages you visit. It asks the
browser for the address and title of the current tab at the moment you press send,
using the `activeTab` permission, which grants that access only for that click and
only for that tab.

## Host permissions

The extension asks for access to websites when you install it, and uses it for
exactly one feature: Click'n'Load. Catching those buttons means running code
inside the page that carries one, and such a button can be on any site, so the set
cannot be narrowed in advance.

It is on from the start, because it is what most people install this for.
Switching it off in the options **removes that code from your pages** rather than
merely disabling it: the content scripts are unregistered, and nothing of this
extension runs in any page you visit until you switch it back on.

Nothing else uses that access. Instances are reached through the relay by phrase,
not by address, so there is no site the extension needs permission for beyond
this. Sending a link or a page reads the address and title of the current tab at
the moment you press send, which `activeTab` covers on its own.

## What Click'n'Load interception does with what it sees

Only one thing: it recognises a submission aimed at a download manager, decodes
the list of links inside it, and offers to send that list to an instance you
chose. It does not read, store, or transmit anything else from the page, and the
links go to your own server and nowhere else.

## What the extension does not do

- No accounts, no sign-up, no identifiers.
- No analytics, no telemetry, no crash reporting.
- No advertising, and no data sold or shared with anyone.
- No reading, injecting into, or modifying the pages you visit.
- No remote code. Everything the extension runs ships inside the package.

## Children

The extension is a tool for operating your own server software. It collects nothing
from anybody, of any age.

## Changes

If this policy ever changes, the date at the top changes with it, and the change is
visible in the repository's history.

## Source

The extension is free software under the AGPL-3.0. Everything described above can
be checked against the source:
https://github.com/junkerderprovinz/knightloader/tree/main/extension/src

## Contact

privacy@knightloader.app
