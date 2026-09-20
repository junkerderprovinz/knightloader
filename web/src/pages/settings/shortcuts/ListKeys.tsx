import { Card, SectionTitle } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { formatShortcut } from '../../../lib/commands/shortcuts';

/**
 * ListKeys shows the keys of the download and collector lists. They are the list
 * widget's own keys on the focused row (components/listKeyboard.ts), not
 * commands, so they cannot be rebound or reset here.
 */

interface KeyRow {
  label: TranslationKey;
  /** One or more chords, each rendered as its own keycap. */
  combos: string[];
}

function keyRows(rtl: boolean): KeyRow[] {
  // In a right-to-left interface the tree opens toward the reading direction.
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
    // Handled by ListToolbar, but people look for it on this card.
    { label: 'task.remove', combos: ['delete'] },
    { label: 'task.removeWithFiles', combos: ['shift+delete'] },
  ];
}

export function ListKeysCard({ hue }: { hue: number }) {
  const { t } = useT();
  const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';

  return (
    <Card hue={hue} padding="none" className="flex flex-col">
      <div className="p-5 pb-0">
        <SectionTitle hint={t('settings.shortcuts.listHint')}>{t('settings.shortcuts.listTitle')}</SectionTitle>
      </div>
      <div className="flex flex-col divide-y divide-carbon-border/60">
        {keyRows(rtl).map((row) => (
          <div key={row.label} className="flex items-center gap-3 px-4 py-2.5">
            <span className="min-w-0 flex-1 truncate text-sm text-carbon-text">{t(row.label)}</span>
            {/* One keycap per chord, so no separator needs translating. */}
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
