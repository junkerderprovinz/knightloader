# Subscriptions

A subscription is something KnightLoader checks by itself at an interval. Each
new entry goes to the link collector like a link you pasted, so the filter,
the crawler, the Packagizer and the mirror check all apply to it. You find
them in the **Subscriptions** card under Settings > Downloads, and in the
phone app under Settings > Subscriptions.

There are three kinds:

- **Feed**: an RSS or Atom feed, the kind a podcast, a blog or a GitHub
  release page publishes.
- **Channel or playlist**: a YouTube channel, an `@handle` or a playlist, or a
  channel on another site yt-dlp can list. It can also fetch the channel's
  older videos.
- **Mediathek search**: a search in the public broadcasters' libraries of
  Germany, Austria and Switzerland, run through
  [MediathekViewWeb](https://mediathekviewweb.de/).

The kind is fixed once the subscription is saved. Another kind is another
subscription, so remove the row and add a new one.

## Adding one

Paste a link into the address field. KnightLoader works out what it is:

- A YouTube channel, `@handle` or playlist is named after the channel and
  offers two ways in. **New videos only** follows the channel's own feed.
  **Older videos too** makes it a channel subscription, which can also bring
  the videos that are already there. The phone app calls them **Subscribe to
  new videos** and **Subscribe with older videos**.
- A MediathekViewWeb search, copied from the address bar after you searched
  there, offers **Subscribe to this search**. The channel, topic and title of
  the search carry over, and so do its shortest and longest length. Words
  without a mark go to the topic, or to the title when the search names a
  topic as well. A search in the description has no field here and is left
  out.
- Anything else is fetched once as a feed. When it is no feed but yt-dlp can
  list it, it becomes a channel subscription.

**Test** fetches the subscription once and shows its first ten entries, with
their length where it is known, and for each one it would skip, why: **No
title match**, **Excluded**, **Too short**, **Too long** or **Downloaded
before**. A test adds nothing and marks nothing as seen.

## The first check

A new subscription marks every entry already there as seen and adds none, so
it does not fetch a whole back catalogue on the day you add it. From then on
it adds what appears. A channel subscription can hand over older videos on
its first check instead. **Older videos** decides how many: **All**, the
**Last videos** or those of the **Last days**. Left on **None**, it only
notes them, as a feed does.

## What gets added

- **Title filter** and **Exclude filter**: regular expressions tried on each
  entry's title. An entry is added when it matches the title filter (or there
  is none) and does not match the exclude filter.
- **Shortest (minutes)** and **Longest (minutes)**: 0 sets no limit. The
  length comes from the feed where it gives one. Otherwise yt-dlp looks the
  video up, for links it can handle. An entry whose length nobody can tell is
  taken.
- **What to fetch**: **Site preset** leaves it to the preset for the site, as
  for a pasted link. **Own choice** picks the rows a video gets (video, audio,
  thumbnail, subtitles, description) and the quality they are capped at.
  **Audio only** fetches just the sound. A Mediathek search has no yt-dlp
  rows. It has a **Quality** of its own, HD, Normal or Low, with the next
  lower one taken where a programme lacks it, and **Subtitles** fetches the
  broadcaster's subtitle file beside the video.
- **Downloaded before**: an entry that was downloaded already, under any link,
  playlist or subscription, goes to the rejected links in the link collector
  instead, marked as already downloaded with the date and the folder.
  **Download anyway** adds it from there. Switch on **Also take what was
  downloaded before** to let a subscription add repeats.
- **Starts on its own**: off by default, so entries wait in the link collector
  unless the instance confirms links automatically. Switched on, this
  subscription's entries start downloading as soon as they arrive. Somebody
  else writes the feed, so switch it on only for one you trust.

## Names and folders

**Package name** decides how a subscription's entries are grouped. With
**Entry title** every entry gets a package of its own. With **Channel or
show** everything goes into one package and one folder named after the
subscription's **Name**, or the feed's own name when that is empty. Files from
a Mediathek search are always named after the programme's title, since the
names their servers give them say nothing.

**Library layout** chooses what a media server sees:

- **Standard**: the files are stored like any other download. A video or an
  audio file from yt-dlp gets an NFO beside it only when **Write an NFO file
  beside the download** is on in the yt-dlp settings.
- **Series**: the layout Jellyfin and Plex read as a show. Each show gets its
  folder with `tvshow.nfo`, and the channel's picture and banner as poster and
  fanart where the site has them. Each year is a season, and every file gets
  an episode NFO beside it:
  `Show/Season 2026/Show - S2026E112701 - Title.mkv`. The episode number is
  the upload's month and day and a counter for that day. This works on videos
  yt-dlp downloads, not on the files of a Mediathek search.

## SponsorBlock

For YouTube videos KnightLoader can ask
[SponsorBlock](https://sponsor.ajay.app/) which parts are sponsor reads,
intros, reminders to subscribe and so on, and either cut them out or mark them
as chapters. The default for every video is
the SponsorBlock card in the yt-dlp settings. Under **SponsorBlock** a
subscription keeps that **Default** or makes its **Own choice**.

## When a video is blocked where you are

Many broadcasters only serve some programmes inside their own country. Such a
download fails as **Blocked here**, and its row says that the site does not
serve this video to this region. That holds for videos yt-dlp fetches and for
the plain files a Mediathek search brings. When yt-dlp fetches an ORF programme outside Austria, ORF sends a
short placeholder clip instead, and KnightLoader treats that the same way.

What helps is a connection from a country the broadcaster serves. With a VPN
set up on the Integrations page, switch on **yt-dlp and streams** for videos
yt-dlp fetches, and **Hosters and direct links** for the files of a Mediathek
search. Those files can also go through a proxy under **Outbound
connections** on the Network page, which yt-dlp does not use. Otherwise set
`HTTP_PROXY` and `HTTPS_PROXY` in the server's environment, and yt-dlp takes
them from there.

## Checking and timing

A new subscription is fetched right after you save it, and every subscription
again each time the server starts. After that it is fetched at its interval,
15 minutes unless you set another under **Check every (minutes)**. Saving the
settings keeps each subscription's remaining wait, and a shorter interval
takes effect at once. The status in each subscription's sheet shows when it
was last checked and what went wrong if it did. In the web interface it also
shows whether the first check has run and how many entries the subscription
remembers.
