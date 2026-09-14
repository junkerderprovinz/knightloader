# GlimStone

KnightLoader's UI follows **GlimStone**, a design language shared with other apps by the same author. The spec moved out of this repo — see [github.com/junkerderprovinz/glimstone](https://github.com/junkerderprovinz/glimstone) for the full rule set, palette, rationale, and copy-ready reference files.

`web/src/index.css` and `web/src/lib/appearance.ts` remain here as KnightLoader's own live implementation of the language — they are not the spec itself. When a rule changes upstream, port it here the same way any other adopting app would (see the GlimStone repo's "Adopting GlimStone in another app" section).

One thing the language deliberately keeps out of its own spec is an adopting app's easter eggs: they belong in that app's notes, and KnightLoader's are in [easter-eggs.md](easter-eggs.md). What GlimStone does hand down is the rule they all follow — an egg that changes behaviour must be switchable back off, and must not quietly become a permanent entry in a settings list.
