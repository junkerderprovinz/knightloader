# KEEP — the shared design language

A warm, layered, low-noise interface system. It is used by KnightLoader and by
BombVault (branch `feat/keep-design`), and is written so a sibling app adopts it
without rewriting components:
the token **names** are the contract, the values are the look.

## The rules that keep it calm

1. **One raised surface.** `.keep-card` is the only elevation. Never nest a card
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
7. **Digits use `.keep-num`** (tabular + lining numerals) wherever they change or
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
| `--radius-card`, `--radius-control` | 16px / 10px |

Utility classes: `.keep-card` (the surface), `.keep-well` (inset grouping),
`.keep-eyebrow` (small uppercase label), `.keep-num` (tabular digits),
`.keep-page-enter`, `.keep-toast`, `.keep-live` (pulsing dot).

## Adopting it in another app

BombVault already reads the same `--carbon-*` names through its own
`@theme` block, so:

1. Copy the `:root` / `[data-theme="light"]` blocks from
   `knightloader/web/src/index.css` into the sibling app's stylesheet.
2. Copy the `.keep-card` / `.keep-well` / `.keep-eyebrow` / `.keep-num` helpers and the
   scrollbar + focus rules.
3. Add the missing tokens the sibling doesn't have yet: `--accent-soft`,
   `--elevation`, `--hairline`, `--focus-ring`, `--radius-card`,
   `--radius-control`.
4. Replace hard-coded `rounded-lg` / `shadow-*` on panels with `.keep-card`, and
   solid accent nav fills with the rail treatment (a 3px accent bar plus a
   raised surface) so the two apps read as one family.

Nothing else is required — component markup can stay as it is, because every
colour already flows through the tokens.
