import { useMemo, useState, type SVGProps } from 'react';
import { type QueueMove, type Task, queueMove, setPackage } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, Field, IconBadge, Modal, TextInput } from './ui';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { IconArrowDown, IconArrowUp, IconBottom, IconClose, IconFolder, IconPriority, IconTop } from '../lib/icons';

// Split by hoster: one package's box forking into three per-host boxes, drawn
// solid like the glyphs in lib/icons.tsx.
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
 * PackageActions puts every package operation for a selection behind one badge
 * and menu in the selection row, since each answers which package the links
 * belong to. Moving and merging share one dialog.
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
  // Existing names for the dialog's datalist.
  const known = useMemo(
    () => [...new Set(tasks.map((x) => x.package).filter((p) => p !== ''))].sort(),
    [tasks],
  );
  // Queue order is offered for a single package only, since moving several to
  // the top has no clear order among them.
  const packages = useMemo(
    () => [...new Set(chosen.map((x) => x.package ?? ''))],
    [chosen],
  );

  if (chosen.length === 0) return null;

  async function splitByHost() {
    // One request per host, not per task.
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

  // By package name rather than the visible ids, so a filtered list cannot
  // move the package in pieces.
  async function move(where: QueueMove) {
    try {
      await queueMove({ package: packages[0] }, where, base);
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    }
  }

  return (
    <>
      {/* `labelled` like the other badges in the selection row, so the label
          setting applies to all of them. */}
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
            // IconPriority rather than an arrow, so the entry differs from the
            // four arrows in its submenu.
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
 * PackageMoveDialog moves or merges tasks into a freely typed package, with
 * known packages offered as a datalist. ListMenu opens it from the context menu
 * too.
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
          {/* The forward button ends the row, so the count goes first. */}
          <span className="glim-num text-xs text-carbon-textMuted">
            {count} {t('select.count')}
          </span>
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
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
      {/* An empty name ungroups, so it is allowed. */}
      <datalist id={listId}>
        {known.map((p) => (
          <option key={p} value={p} />
        ))}
      </datalist>
    </Modal>
  );
}
