import { Card, SectionTitle } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { formatShortcut } from '../../../lib/commands/shortcuts';

/**
 * The keys that belong to the download list and the collector list.
 *
 * It is here because the page above it promises to be unfiltered - "every
 * command this build declares, across every surface" (Shortcuts.tsx's own doc
 * comment). Ten keys that work everywhere the list is drawn and appear nowhere
 * on the one page that lists keys would quietly make that promise false, and
 * the only way to find out they exist would be to press them by accident.
 *
 * NO Change and NO Reset button, unlike every row above. These are not
 * commands and they are not in the override store: they are the list widget's
 * own keys, handled on the focused row itself (components/listKeyboard.ts),
 * which is also why they cannot take a key away from a rebindable command - a
 * row has to have the focus before any of them mean anything.
 *
 * The chords go through formatShortcut like every other row on the page, so
 * Ctrl reads Strg in German and a Mac shows its own glyphs, and the two
 * Delete rows reuse the command labels the toolbar already has rather than
 * inventing a second wording for the same act.
 */

interface KeyRow {
  label: TranslationKey;
  /** One or more chords, each rendered as its own keycap. */
  combos: string[];
}

function keyRows(rtl: boolean): KeyRow[] {
  // In a right-to-left interface the tree opens toward the reading direction,
  // exactly as the twisty already points that way - so the card has to name the
  // key the person will actually press, not the one an English build uses.
  const open = rtl ? 'left' : 'right';
  const close = rtl ? 'right' : 'left';
  return [
    { label: 'keys.list.move', combos: ['up', 'down'] },
    { label: 'keys.list.moveKeep', combos: ['mod+up', 'mod+down'] },
    { label: 'keys.list.edges', combos: ['home', 'end'] },
    { label: 'keys.list.pick', combos: ['space'] },
    { label: 'keys.list.range', combos: ['shift+up', 'shift+down'] },
    { label: 'keys.list.properties', combos: ['enter'] },
    { label: 'keys.list.open', combos: [open] },
    { label: 'keys.list.close', combos: [close] },
    { label: 'keys.list.menu', combos: ['menu', 'shift+f10'] },
    // Owned by ListToolbar's own removal, not by the list keys - listed here
    // anyway because the person looking for "how do I delete this" is looking
    // at this card, and where the handler lives is not their problem.
    { label: 'task.remove', combos: ['delete'] },
    { label: 'task.removeWithFiles', combos: ['shift+delete'] },
  ];
}

export function ListKeysCard({ hue }: { hue: number }) {
  const { t } = useT();
  const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';

  return (
    // Same shape as a command group above: the title in its own padded block
    // and the rows dividing from each other underneath, so this card reads as
    // one more group rather than as a different kind of thing.
    <Card hue={hue} padding="none" className="flex flex-col">
      <div className="p-5 pb-0">
        <SectionTitle hint={t('settings.shortcuts.listHint')}>{t('settings.shortcuts.listTitle')}</SectionTitle>
      </div>
      <div className="flex flex-col divide-y divide-carbon-border/60">
        {keyRows(rtl).map((row) => (
          <div key={row.label} className="flex items-center gap-3 px-4 py-2.5">
            <span className="min-w-0 flex-1 truncate text-sm text-carbon-text">{t(row.label)}</span>
            {/* One keycap per chord, never one string with a separator in it:
                a joining character would be a piece of user-visible text with
                no key behind it, and it would need translating to say the same
                thing in a language that does not read "or" as a slash. */}
            <div className="flex shrink-0 items-center gap-1.5">
              {row.combos.map((combo) => (
                <kbd
                  key={combo}
                  className="glim-num rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-[11px]
                    font-medium text-carbon-textSub"
                >
                  {formatShortcut(combo, t)}
                </kbd>
              ))}
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}
