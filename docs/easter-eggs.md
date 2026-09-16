# Easter eggs

GlimStone has exactly one egg of its own (`storm`, below) and says plainly that
an adopting app's eggs belong in **that app's** notes rather than in the shared
language. This file is KnightLoader's.

What the language does hand down is the rule that comes with every one of them:

> **An easter egg that changes behaviour must be switchable back off, and must
> not quietly become a permanent entry in a settings list.**

The failure it was written against is worth restating, because it does not look
like a failure while you are building it: the first `storm` implementation
stored a "found it" flag, so one gesture put a fourth option in the motion
picker for ever after. That turns a secret into a setting somebody has to
explain to themselves months later with no memory of how it got there, and it
makes the picker's contents depend on history rather than on state.

Two consequences that every entry below has to satisfy:

- **The way out is the way in, or plainer.** Whatever the egg turned on, an
  ordinary interaction turns off — a second gesture, a different value in the
  same field, closing the screen. Nothing needs a reset button, because nothing
  is stored that would need resetting.
- **A chosen value persists; the fact that it was FOUND does not.** Those two
  look similar and are not. `storm` is remembered because it is the current
  setting; that the picker once offered it is remembered by nobody.

Nothing below writes to storage. Three of the five have no state at all: they are a
question asked of the current value (the limit), of a pointer that is currently
down (the blade), or of how long an element has already been on screen
(the knight). The parade keeps two numbers in a ref that dies with the tab.

## Shipped

### The blade

**Gesture:** press and hold the mark at the top of the sidebar. The blade draws
out of the rail while you hold, and swings when you let go; a shudder then runs
down the rail, one entry after the next, starting from the impact.

**Where:** `useDrawAndStrike` in `web/src/components/Sidebar.tsx` decides which
of three states the element is in; everything visible is `.kl-egg` in
`web/src/index.css`.

It is the counterpart to BombVault's own, which shatters its logo into 36 pieces
with a fire cloud. That fits a bomb; a sword's gesture is drawing and striking,
and it keeps the same rhythm — tension while held, discharge on release.

**A short press is still a click and still navigates home.** The browser fires
`click` after `pointerup` either way, so the hook suppresses only the click that
ended a hold. An egg that ate the navigation would be a broken logo rather than
a surprise.

Because it is CSS, it follows the motion setting and disappears under
`prefers-reduced-motion` without knowing that it does.

### `storm`, the fourth motion level

**Gesture:** set the motion intensity to the top level, then tap that same
option five more times. It is unreachable from any other level on purpose:
tapping "off" five times means somebody is annoyed, not curious, and a secret
that opens under annoyance is a bug report waiting to be filed.

**Where:** `stormTap()` in `web/src/lib/appearance.ts` is the whole mechanism;
the numbers are one token block, `:root[data-motion="storm"]`, in
`web/src/index.css`.

This one is GlimStone's, adopted here rather than invented here — the language
carries it as the case that establishes the rule at the top of this file. Three
details are easy to get wrong and are all load-bearing:

- The option is offered while it is **chosen**, because a picker that hid the
  value it is currently showing would be lying about the interface. Otherwise it
  is offered only for as long as the settings screen stays open.
- A persisted `storm` is **still accepted at boot**, even though no picker
  offers it — otherwise the gesture would produce a setting that silently
  forgets itself on the next reload. Validating a stored value and populating a
  picker are two different questions.
- It sits **inside** the `prefers-reduced-motion: no-preference` gate, not
  beside it, so a hidden "more animation" switch can never talk a browser out of
  an accessibility signal.

### 1337

**Gesture:** set the speed limit to exactly 1337 KiB/s. The speed curve on the
Overview page runs on the storm curve for as long as the limit stands.

**Where:** `isLeet()` in `web/src/lib/leet.ts` is the whole condition, read by
`SpeedGraph` and by the limit field in
`web/src/pages/settings/DownloadsSettings.tsx`; `.kl-storm-curve` in
`web/src/index.css` is what switches the element onto the level.

**Off:** type a different number.

**The number stays in the field.** A field that does not show what was typed into
it is a bug for one second before it is a joke. The word stands to the RIGHT of
the number, not under it: dropped into the field's own column it landed exactly
where this page's hints and error lines live, and the joke read as a complaint
about the value above it.

**The unit is the trap.** `speedLimit` is bytes per second everywhere it travels,
and every field that edits it draws KiB. The comparison is against `1337 * 1024`;
against a bare `1337` it would be listening for 1337 B/s, which nobody would ever
set and nobody would ever find.

**It scopes the level, it does not choose it.** `data-motion` on `<html>` keeps
saying whatever the reader set, because the picker, the boot reader and the phone
all take it at its word. Measured live: with the limit standing, the hero curve's
`--motion-pulse-dur` is the storm's 1.4s while `<html>` still reads `wild` and the
shell strip's own curve, outside the egg, still reads 2s. The two numbers the egg
borrows are named once (`--egg-storm-pulse-*`) and read by both the storm block
and the egg, so retuning the level retunes the egg.

### The sleeping knight

**Gesture:** leave an empty state standing. After 24 seconds the mark in it blinks
twice and settles, dimmed. Anything arriving replaces the empty state and the mark
is gone with it.

**Where:** the `icon` slot of `EmptyState` in `web/src/components/ui.tsx` carries
`.kl-doze`; the keyframe and the delay are in `web/src/index.css`.

**Off:** add a link, or set the motion intensity to `off`.

**What was ordered was a figure closing its eyes, and there is no figure.** Every
caller hands this slot a 20-unit house glyph - a download arrow on the queue, a
magnifier on a search with no hits, a keyboard on the shortcuts page. Eyelids
drawn over an arrow would be the joke explained rather than told, and swapping in
a knight's helm is the caller's decision, not this component's. So the mark itself
blinks, which reads as "this has been standing here a while" against any glyph.

**It costs nothing until it fires.** No timer, no interval, no state: one CSS
`animation-delay`, whose opening frame is exactly what the mark looks like with no
rule on it at all. An empty state that lived for two seconds was charged for
nothing.

### The parade

**Gesture:** finish eight files inside twelve seconds - a package of many closing
in one go. A row of five small shields sweeps that bubble once, and the checkmark
is drawn after they have passed.

**Where:** `PARADE_AT` and the burst counter in `web/src/lib/toast.tsx`, which is
the one funnel every notification in this app passes through; `.kl-parade` and
`.kl-parade-check` in `web/src/index.css`; `IconShield` in
`web/src/lib/icons.tsx`.

**Off:** it ends by itself, with the toast.

**Eight, and the number is a measurement.** This app finishes one task per LINK,
so a package closing produces one bubble per file. The common shapes in its own
lists are a single file and a multi-volume archive set of three to six parts, so a
threshold of three would fire on an ordinary evening and stop being a surprise.
The twelve-second window is what makes it "in one go": a counter with no window
eventually fires on any instance left running long enough, and a parade nobody can
connect to anything they did is not a parade.

**It rides the toast's own tokens and invents no second clock.** The sweep is four
content fades long, the shields are spaced by `--motion-stagger-step` and capped by
`--motion-stagger-cap` exactly as `.glim-stagger`'s rows are, and the checkmark is
held back by the length of the sweep. At motion `off` all three numbers are 0, so
the bubble simply draws its checkmark at once - measured, `kl-parade 0s delay=0s`.
Exactly one bubble in a burst carries it, and `.glim-checkmark` finally has the
consumer it has been waiting for since the motion engine's second round.
