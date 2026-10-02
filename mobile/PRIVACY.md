# Privacy policy: KnightLoader Android app

Last updated: 2 October 2026. Applies to the versions released since then, until
this date changes.

## The short version

The app is a remote control for KnightLoader instances that you run yourself. It
shows what they are downloading and sends them the links and commands you give
it. It has no accounts, no advertising and no analytics of its own. While one of
your instances is downloading or waiting for a captcha, the app keeps its own
connection to them in the background so it can notify you, and it stops once
nothing is running.

It reaches your instances through a relay operated by the party named under "Who
is responsible". What the app and your instances say to each other is encrypted
with a key that only your own devices hold, so the relay passes it along without
being able to read it. The relay does see that a phone is connected, from which
IP address, and which instance a message is addressed to.

One feature talks to someone else, and only when you use it: some captchas load
the captcha provider's page. It is described below.

## What is stored on your phone

We back none of this up ourselves, and uninstalling the app deletes all of it.

In the Android keystore-protected storage (`expo-secure-store`):

- Your saved connections. For each instance: the name it goes by, its instance ID,
  the relay's address, and the group key and message key derived from your phrase.
  They hold no password or token, because being in the group is what lets the app
  in. Connections saved by address in older versions of the app hold the address
  and an API token instead.
- Which connection was open last.

The twelve words of your phrase are not stored. The app derives the two keys from
them when you connect, and the words are gone after that.

In the app's ordinary storage:

- A random device ID such as `phone-mf3k2a1x-4h7s9d2q`, made from the time it was
  created and a random number. It contains nothing about you or your phone. The
  relay uses it to recognise a reconnect from this phone as the same member of your
  group.
- Your settings: the interface language if you chose one, the look (theme, corner
  shape, accent colour, rainbow mode, and whether to follow an instance's look),
  the motion level, which notifications you want, and whether the app has asked
  for the notification permission yet.

If you use Android's own backup, it may include the app's storage. The saved
connections are encrypted with a key that stays in this phone's keystore and is
not part of any backup, so a restored copy cannot read them.

## What leaves your phone

### To the relay

The relay is `parleyport.halleluja.design`, on a server in Germany. Every connection to
it carries:

- A group key derived from your phrase with a one-way hash. It cannot be turned
  back into the words. Whoever presents this key joins your group, so it works like
  a password for the group.
- Your random device ID, and the ID of the instance a message is for, which the
  relay needs for routing.
- Encrypted messages (AES-256-GCM, with a second key derived from your phrase that
  the relay never receives). The relay cannot read their content, which includes
  the links you add, your download lists and the name the app gives itself.
- Your IP address, as with any connection on the internet.

The relay keeps no record of who connects or what they send, and its error log
never contains IP addresses. The one exception is a connection that fails the
relay's own handshake, for example one that closes or stalls before it identifies
itself: its IP address is held in memory to slow down repeated failures, and
deleted within 61 minutes of that address's last failed attempt.

### To your own instances

These travel through the relay, and only your instances can read them:

- Requests for what an instance is doing: its download list, queue state, speeds,
  counters, the captchas that are waiting, and its look if you follow it.
- What you do in the app: links you add, starting or stopping the queue, switching
  links and packages on or off, removing packages, and your answers to captchas.

While one of your instances is downloading or has a captcha waiting, the app also
asks your instances for their download list and the waiting captchas every 10 to
30 seconds in the background, even after you close it. It builds the
notifications from the answers on the phone. No push service, Google's included,
takes part.

A connection saved by address in an older version of the app talks to that address
directly, over HTTP or HTTPS as the address says, and sends its API token with each
request.

### When you scan a QR code

The QR code beside your phrase can be scanned instead of typing the words. The
app reads the camera image on the phone with the free ZXing library, and neither
stores nor sends it. No one else takes part.

### When you answer a captcha with a checkbox or challenge

Picture and click captchas are shown and answered inside the app. reCAPTCHA,
hCaptcha and Cloudflare Turnstile challenges run the provider's own script, which
the app loads from Google, hCaptcha or Cloudflare when you open such a captcha. The
provider then receives what a browser would send it, including your IP address and
details about the phone and its browser engine, under its own privacy policy. The
answer goes back to your instance through the relay.

### When you open a link or donate

- The Buy Me a Coffee, PayPal and GitHub buttons, the version numbers and the mail
  button open your browser or mail app. The app itself sends nothing there.
- The crypto window shows donation addresses and draws their QR codes on the phone.

The "Copy report" button in Settings puts the app version, the Android version,
the language and the look on the clipboard, for you to paste into a bug report.
Nothing is sent.

The app makes no other network requests.

## Permissions

| Permission | Used for |
| --- | --- |
| Camera | Scanning the QR code of your phrase. Asked for when you open the scanner and press "Allow access". |
| Internet | Reaching the relay and your instances. |
| Network state | Letting the app's libraries see whether the phone is online. |
| Notifications | Telling you about a captcha that waits for an answer and a download that finished or failed. Asked for the first time a download is running while the app is open, and switchable per kind in Settings. |
| Foreground service | Keeping the connection to your instances while one of them is busy. Android shows a quiet notification for as long as it runs, and it stops once nothing is running. |
| Keep awake | Letting that connection look again while the screen is off. Held only while the foreground service runs. |

The app asks for no location, contacts, storage or microphone permission.

## What the app does not do

- It has no analytics, telemetry or crash reporting of its own.
- It shows no advertising, and we do not sell data or share it with anyone.
- It does not download files onto the phone. Downloads run on your instances.

## Keeping your data safe and deleting it

- Everything between the app and your instances is encrypted as described above,
  and the connection to the relay uses TLS.
- **Remove a connection** with the bin button at the top of its downloads screen,
  or all of them in Settings. Their keys are deleted from the phone.
- **Uninstall the app** to delete everything it stored.
- **Relay data:** while a connection is open, the relay holds what it needs to
  route it (your IP address, the group key, your device ID and the encrypted
  messages passing through) and drops all of it when the connection closes. Beyond
  that it holds nothing about you except the rate-limit entry described above,
  which is deleted within 61 minutes of that address's last failed attempt. For any
  question about it, or to exercise your rights, write to the contact address
  below.

The phrase belongs to your whole group. Removing the connections on one phone does
not change the phrase on your instances; to lock a lost phone out, give your
instances a new phrase.

## Who is responsible

The relay and this policy are the responsibility of:

Georg Düringer (Halleluja Design)
privacy@halleluja.design

The relay runs on a server rented from Hetzner Online GmbH, Germany, which
processes this data on our behalf.

Under the EU General Data Protection Regulation, forwarding your messages rests on
Art. 6(1)(b), because it is the service you use the app for, and the short-lived
rate-limit entry rests on Art. 6(1)(f), our interest in keeping the relay
available. You have the right to access, correct and delete your data, to restrict
or object to its processing, to receive it in a portable format, and to complain to
a data protection supervisory authority.

## Children

The app is a tool for operating your own server software and is not directed at
children.

## Changes

When this policy changes, the date at the top changes with it, and every change is
visible in the repository's history.

## Source

The app is free software under the AGPL-3.0. Everything described here can be
checked against the source:
https://github.com/junkerderprovinz/knightloader/tree/main/mobile
