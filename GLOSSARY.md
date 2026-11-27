# Glossary

**Offline**
A link whose source says the file no longer exists, or a hoster whose site does not answer. An offline hoster is hidden from the account picker, and links to it are still accepted.

## Subscriptions

**Subscription**
Something KnightLoader checks by itself at an interval, adding each new entry to the link collector. In German it is an Abo.
_Avoid_: Source, Quelle (those mean where a link came from), feed (that is one kind), watch

**Feed subscription**
A subscription to an RSS or Atom feed.

**Channel subscription**
A subscription to a video channel or playlist that yt-dlp can list, which can also fetch the channel's older videos.
_Avoid_: Playlist subscription

**Mediathek subscription**
A subscription to a show in the public broadcasters' libraries of Germany, Austria and Switzerland, found through MediathekViewWeb.
_Avoid_: Mediathek search (KnightLoader does not search the Mediathek, it follows a search somebody saved)

**Entry**
One thing a subscription lists: a feed item, a video or an episode.
_Avoid_: Item, post

**Seen entry**
An entry a subscription has already added or passed over, and so never adds again.

**First check**
A subscription's first look at its entries, which marks all of them as seen and adds none, unless a backfill asks for older ones.
_Avoid_: Seeding (that is a torrent sharing what it downloaded)

**Backfill**
The older entries a channel subscription adds on its first check: all of them, the last few, or those of the last few days.
