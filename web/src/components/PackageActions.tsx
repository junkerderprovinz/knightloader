import { useMemo, useState, type SVGProps } from 'react';
import { type QueueMove, type Task, queueMove, setPackage } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, Field, IconBadge, Modal, TextInput } from './ui';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { IconArrowDown, IconArrowUp, IconBottom, IconFolder, IconPriority, IconTop } from '../lib/icons';

// One glyph lib/icons.tsx has no equivalent for yet. It follows that file's
// own house style (solid fill, never a stroked outline), so a badge built
// from it sits at the same visual weight as the four page-level badges beside
// it (jdp, 2026-08-24: "die sollen in der gleichen zeile wie die
// quadratischen badges erscheinen").
//
// The queue-order glyph that used to sit beside it here was the filled twin of
// ListToolbar.tsx's own stroke-based one. Both are gone: the filled drawing is
// now lib/icons.tsx's IconPriority, imported above, so the badge and the menu
// entry that mean the same thing are one drawing again.

/** Split by hoster: one package's box forking into three per-host boxes. */
const IconSplitHost = (p: SVGProps<SVGSVGElement>) => (
  <svg width={22} height={22} viewBox="0 0 20 20" fill="currentColor" className="shrink-0" aria-hidden {...p}>
    <rect x="8" y="2.5" width="4" height="3" rx="1" />
    <rect x="9.3" y="5.5" width="1.4" height="2" />
    <rect x="4.5" y="7.5" width="11" height="1.4" rx=".7" />
    <rect x="3.8" y="8.9" width="1.4" height="2.5" />
    <rect x="9.3" y="8.9" width="1.4" height="2.5" />
    <rect x="14.8" y="8.9" width="1.4" height="2.5" />
    <rect x="2" y="11.4" width="5" height="4" rx="1" />
    <rect x="7.5" y="11.4" width="5" height="4" rx="1" />
    <rect x="13" y="11.4" width="5" height="4" rx="1" />
  </svg>
);

// hostOf is the grouping label for "split by hoster". It mirrors what the
// backend uses for its per-host concurrency limit, so the two agree on what
// counts as one host.
function hostOf(raw: string): string {
  try {
    return new URL(raw).hostname.replace(/^www\./, '');
  } catch {
    return '';
  }
}

/**
 * PackageActions is everything a selection's PACKAGE can be told, behind one
 * badge in Collector.tsx's and Downloads.tsx's shared selection row (jdp,
 * 2026-08-24: "die sollen in der gleichen zeile wie die quadratischen badges
 * erscheinen, nicht in einer neuen Zeile").
 *
 * It was three badges until 2026-09-14. They were not three ideas: every one of
 * them answers "which package do these links belong to", which is what makes
 * them a menu rather than a row. Three separately-labelled buttons for one idea
 * is also what made the row unreadable - "In ein Paket verschieben", "Nach
 * Hoster aufteilen" and "Ganzes Paket verschieben" side by side are three long
 * German sentences that have to be read to the end before they can be told
 * apart, and they come before the verbs somebody actually came for.
 *
 * Moving and merging are the same operation seen from two sides — several
 * tasks ending up under one name — so they share a dialog instead of being two
 * half-features.
 */
export function PackageActions({
  tasks,
  selected,
  base,
  onDone,
}: {
  tasks: Task[];
  selected: Set<string>;
  base: string;
  onDone?: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [dialog, setDialog] = useState(false);
  const order = useContextMenu();

  const chosen = useMemo(() => tasks.filter((x) => selected.has(x.id)), [tasks, selected]);
  // Existing names feed the datalist, so moving into a package that already
  // exists is a pick rather than a re-typing exercise.
  const known = useMemo(
    () => [...new Set(tasks.map((x) => x.package).filter((p) => p !== ''))].sort(),
    [tasks],
  );
  // Which packages the selection sits in. The queue-order entries are offered
  // for one and only one: "send this package to the top" over three packages at
  // once has no defensible answer about which of them arrives there first.
  const packages = useMemo(
    () => [...new Set(chosen.map((x) => x.package ?? ''))],
    [chosen],
  );

  if (chosen.length === 0) return null;

  async function splitByHost() {
    // One request per host rather than per task: the endpoint already takes a
    // list, and a package with forty parts should not become forty calls.
    const byHost = new Map<string, string[]>();
    for (const task of chosen) {
      const h = hostOf(task.url);
      if (!h) continue;
      const ids = byHost.get(h);
      if (ids) ids.push(task.id);
      else byHost.set(h, [task.id]);
    }
    for (const [host, ids] of byHost) await setPackage(ids, host, base);
    onDone?.();
  }

  // Named rather than sent as the ids on screen: the ids a filtered list can
  // produce are the rows that survived the filter, and a package that arrives
  // at the top of the queue in pieces is worse than one that did not move.
  async function move(where: QueueMove) {
    try {
      await queueMove({ package: packages[0] }, where, base);
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    }
  }

  return (
    <>
      {/* ONE badge, not three (jdp, 2026-09-14, screenshot of the selection
          row: "wenn man auf alle auswählen klickt kommen viele buttons ...
          schaffen wir es alle buttons in eine Zeile zu packen?").
          Measured, at 1366 points in German with Beschriftung on "text and
          glyph": the three badges cost 175 + 162 + 183 points plus their gaps,
          536 of the 1030 the row's whole column has. Behind one word they cost
          75. Nothing moved into a right-click-only place - this badge is a
          button, its menu is ContextMenu's own role="menu" with arrow keys, and
          every entry keeps the name it had on its badge.
          `labelled`, because this strip is rendered INSIDE Collector.tsx's and
          Downloads.tsx's selection rows between badges that all opt in.
          Without it, a person who has set Beschriftung to "text" reads "Clear
          all", a wordless glyph, then "Start selected" in one line - which is
          the defect the label engine exists to prevent, not a compact strip
          somebody chose. GlimStone 1.8.0 rule 13: the variant decides the
          SHAPE, never whether the words appear. */}
      <IconBadge
        labelled
        icon={<IconFolder width={16} height={16} />}
        title={t('pkg.menu')}
        aria-label={t('pkg.menu')}
        aria-haspopup="menu"
        aria-expanded={!!order.anchor}
        onClick={(e) => order.openAt(anchorBelow(e.currentTarget))}
      />
      {order.anchor && (
        <ContextMenu
          anchor={order.anchor}
          label={t('pkg.menu')}
          onClose={order.close}
          groups={[
            {
              id: 'package',
              items: [
                {
                  id: 'move',
                  label: t('pkg.moveTitle'),
                  icon: <IconFolder width={14} height={14} />,
                  onSelect: () => setDialog(true),
                },
                {
                  id: 'split',
                  label: t('pkg.splitByHost'),
                  icon: <IconSplitHost width={14} height={14} />,
                  onSelect: () => void splitByHost(),
                },
              ],
            },
            // Offered for one package and only one, unchanged: "send this
            // package to the top" over three at once has no defensible answer
            // about which of them arrives there first. A distinct glyph (three
            // descending bars, not an arrow) rather than the four arrows its
            // own submenu opens with, so the entry and its contents are not
            // four identical arrows in a column.
            ...(packages.length === 1
              ? [
                  {
                    id: 'order',
                    items: [
                      {
                        id: 'queueOrder',
                        label: t('pkg.queueOrder'),
                        icon: <IconPriority width={14} height={14} />,
                        submenu: [
                          {
                            id: 'steps',
                            items: [
                              { id: 'top', label: t('task.moveTop'), icon: <IconTop width={14} height={14} />, onSelect: () => void move('top') },
                              { id: 'up', label: t('task.moveUp'), icon: <IconArrowUp width={14} height={14} />, onSelect: () => void move('up') },
                              { id: 'down', label: t('task.moveDown'), icon: <IconArrowDown width={14} height={14} />, onSelect: () => void move('down') },
                              { id: 'bottom', label: t('task.moveBottom'), icon: <IconBottom width={14} height={14} />, onSelect: () => void move('bottom') },
                            ],
                          },
                        ],
                      },
                    ],
                  },
                ]
              : []),
          ]}
        />
      )}
      {dialog && (
        <PackageMoveDialog
          count={chosen.length}
          suggestion={chosen[0]?.package ?? ''}
          known={known}
          onClose={() => setDialog(false)}
          onApply={async (name) => {
            await setPackage(chosen.map((x) => x.id), name, base);
            setDialog(false);
            onDone?.();
          }}
        />
      )}
    </>
  );
}

/**
 * The move/merge dialogue: one free-typed name, with the packages already on
 * screen offered as a datalist.
 *
 * Exported since 2026-09-06, because the badge above it turned out not to be
 * how anybody looks for this (jdp: "in der linkliste kann ich links nicht
 * markieren und in ein Paket verschieben, welches ich frei bennnenn kann. wie
 * in JD" - the capability was there, behind a folder glyph in the selection
 * row, and JDownloader puts it in the right-click menu). ListMenu now opens
 * this same dialogue from there, rather than growing a second one that could
 * drift from this one's behaviour.
 */
export function PackageMoveDialog({
  count,
  suggestion,
  known,
  onClose,
  onApply,
}: {
  count: number;
  suggestion: string;
  known: string[];
  onClose: () => void;
  onApply: (name: string) => void;
}) {
  const { t } = useT();
  const [name, setName] = useState(suggestion);
  const listId = 'kl-known-packages';

  return (
    <Modal
      title={t('pkg.moveTitle')}
      onClose={onClose}
      footer={
        <>
          {/* The count first, then the spacer, then the pair. GlimStone 1.14.0
              asks for the control that goes ahead at the END of its row, which
              is not the same as merely right of its partner: this row used to
              read Cancel, Merge, spacer, count, so the button somebody came for
              sat mid-row with a number to its right. A reading is not a control
              and has no business in the hand's position. */}
          <span className="glim-num text-xs text-carbon-textMuted">
            {count} {t('select.count')}
          </span>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => onApply(name.trim())}>{t('pkg.merge')}</Button>
        </>
      }
    >
      <Field label={t('pkg.name')} hint={t('collector.movePrompt')}>
        <TextInput
          autoFocus
          list={listId}
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onApply(name.trim());
          }}
        />
      </Field>
      {/* An empty name ungroups, which is a legitimate thing to want, so it is
          not blocked — the datalist just makes the common case one click. */}
      <datalist id={listId}>
        {known.map((p) => (
          <option key={p} value={p} />
        ))}
      </datalist>
    </Modal>
  );
}
