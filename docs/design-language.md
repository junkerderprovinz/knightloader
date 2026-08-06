# GlimStone — the shared design language

A warm, layered, low-noise interface system: a small light in dark masonry.

**The name.** Middle English *glimme*, "shining brightness; radiance", attested
around 1400 in *Pearl* — MS Cotton Nero A.x, the same manuscript that carries
*Sir Gawain and the Green Knight*. The Gawain-poet himself alliterates the two
halves together in line 172: *"euer glemered & glent al of grene stones."* The
later thieves'-cant sense ("douse the glim", 1700) is a narrowing of a word that
had been English for three centuries by then. The German ear hears the same
root in *glimmen* and *Glimmer* — the gold that sits in dark rock.

The CSS prefix is `glim-`.

It is used by KnightLoader, and is written so a sibling app adopts it without
rewriting components: the token **names** are the contract, the values are the
look. BombVault already reads the same `--carbon-*` names through its own
`@theme` block, so it is the intended next adopter.

## The rules that keep it calm

1. **One raised surface.** `.glim-card` is the only elevation. Never nest a card
   inside a card — group content with spacing and a section title instead.
2. **One hero per page.** Exactly one element carries weight (the speed figure
   and its curve). Everything else is supporting detail at small type.
3. **Gold marks activity, nothing else.** The accent is reserved for the active
   nav item, the single primary action, progress fills, and the brand mark.
   A page has at most one solid accent button.
4. **Four state hues.** gold = running · green = settled · red = fault ·
   neutral = waiting. Paused shares the neutral tone; its label and its resume
   control carry the difference. Never introduce a fifth hue.
5. **Hierarchy from type and colour step, not from borders.** Separators are
   hairlines at low opacity; boxes are a last resort.
6. **Secondary actions appear on hover.** A long list reads as content, not as
   a wall of buttons. The primary action for a row stays visible.
7. **Digits use `.glim-num`** (tabular + lining numerals) wherever they change or
   stack, so nothing jitters while counting.

## Tokens (the contract)

Defined in `web/src/index.css` under `:root` / `[data-theme="light"]`, and
mapped to Tailwind utilities in the `@theme` block.

| Token | Role |
|---|---|
| `--carbon-bg` | page ground |
| `--carbon-sidebar` | nav rail ground (a touch deeper than the page) |
| `--carbon-surface` | the raised surface — cards, active nav |
| `--carbon-surface2` | inputs, wells, quiet fills |
| `--carbon-surface3` | tracks, hover on surface2 |
| `--carbon-hover` | row hover on the page ground |
| `--carbon-border` | hairline separators |
| `--carbon-text` / `-sub` / `-muted` | three-step text ramp |
| `--sidebar-text` | nav label colour |
| `--accent` / `--accent-contrast` / `--accent-soft` | activity |
| `--status-{ok,fail,warn,info,neutral}-{text,bg,solid}` | states |
| `--elevation`, `--hairline` | the one shadow + its top light |
| `--focus-ring` | focus outline colour |
| `--radius-card`, `--radius-control`, `--radius-pill` | the shape engine |

Utility classes: `.glim-card` (the surface), `.glim-well` (inset grouping),
`.glim-eyebrow` (small uppercase label), `.glim-num` (tabular digits),
`.glim-page-enter`, `.glim-toast`, `.glim-live` (pulsing dot).

## The two user-owned axes

Shape and accent are settings, not constants, and both are applied once at the
app root onto `document.documentElement` — never by the page that edits them.

**Shape** sets `data-shape` on the root; the radius tokens key off it.
`round` (16px / 10px) · `soft` (8px / 5px) · `square` (0). One token drives every
radius in the system, including badges, toggles and knobs. There is no exception
list — an exception is exactly the element that later looks wrong.

**Accent** overrides `--accent` and derives `--accent-contrast` from sRGB
luminance rather than asking for a second colour. See `web/src/lib/appearance.ts`.

**Rainbow** is the accent's optional plural form: instead of one hue, a palette
of eight, handed out by position so each item in a list keeps its own colour.
Rule 3 still holds — a rainbow palette colours *activity*, not decoration.
It has three sub-switches:

- **rotation** — one shared seed offsets the palette, so the colours differ
  between reloads without a component ever disagreeing with its neighbour.
- **reactive** — everything rests neutral; colour appears on hover and stays on
  the active item. This is the restrained reading of the mode.
- **palette** — all eight hues are editable, and reset to the default in one
  click.

## Adopting it in another app

1. Copy the `:root` / `[data-theme="light"]` blocks from
   `knightloader/web/src/index.css` into the sibling app's stylesheet.
2. Copy the `.glim-card` / `.glim-well` / `.glim-eyebrow` / `.glim-num` helpers and
   the scrollbar + focus rules.
3. Add the missing tokens the sibling doesn't have yet: `--accent-soft`,
   `--elevation`, `--hairline`, `--focus-ring`, `--radius-card`,
   `--radius-control`, `--radius-pill`.
4. Replace hard-coded `rounded-lg` / `shadow-*` on panels with `.glim-card`, and
   solid accent nav fills with the rail treatment (a 3px accent bar plus a
   raised surface) so the two apps read as one family.
5. For rainbow, copy `web/src/lib/appearance.ts` — it is dependency-free and
   talks only to `document.documentElement` and its own settings object.

Nothing else is required — component markup can stay as it is, because every
colour already flows through the tokens.
