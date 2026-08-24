import { useMemo, useState, type SVGProps } from 'react';
import { type QueueMove, type Task, queueMove, setPackage } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, Field, IconBadge, Modal, TextInput } from './ui';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { IconArrowDown, IconArrowUp, IconBottom, IconFolder, IconTop } from '../lib/icons';

// Two glyphs lib/icons.tsx has no equivalent for yet. Both follow that
// file's own house style (solid fill, never a stroked outline) rather than
// ListToolbar.tsx's local stroke-based glyphs, so a badge built from either
// one sits at the same visual weight as the four page-level badges beside it
// (jdp, 2026-08-24: "die sollen in der gleichen zeile wie die quadratischen
// badges erscheinen").

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

/** Queue order: three descending bars, the wait order seen side-on - the
 *  filled twin of ListToolbar.tsx's own local (unexported, stroke-based)
 *  IconPriority, which draws the same idea for the same reason. */
const IconQueueOrder = (p: SVGProps<SVGSVGElement>) => (
  <svg width={22} height={22} viewBox="0 0 20 20" fill="currentColor" className="shrink-0" aria-hidden {...p}>
    <rect x="4" y="4.7" width="12" height="1.6" rx=".8" />
    <rect x="4" y="9.2" width="8" height="1.6" rx=".8" />
    <rect x="4" y="13.7" width="4" height="1.6" rx=".8" />
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
 * PackageActions is the package-organising badges merged into Collector.tsx's
 * own selection-mode action row (jdp, 2026-08-24: "die sollen in der
 * gleichen zeile wie die quadratischen badges erscheinen, nicht in einer
 * neuen Zeile") - three square IconBadges, not the text buttons this used to
 * render, so they read as the same kind of control as the badges around them
 * rather than as a different, unlabelled control floating among them.
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
      <IconBadge
        icon={<IconFolder width={16} height={16} />}
        title={t('pkg.moveTitle')}
        aria-label={t('pkg.moveTitle')}
        onClick={() => setDialog(true)}
      />
      <IconBadge
        icon={<IconSplitHost width={16} height={16} />}
        title={t('pkg.splitByHost')}
        aria-label={t('pkg.splitByHost')}
        onClick={splitByHost}
      />
      {/* A distinct glyph (three descending bars, not an arrow) rather than
          the four arrow icons the queue-order submenu itself opens with — the
          two sets do different things to different rows, and side by side as
          identical arrows they would be told apart only by hovering for a
          tooltip. */}
      {packages.length === 1 && (
        <IconBadge
          icon={<IconQueueOrder width={16} height={16} />}
          title={t('pkg.queueOrder')}
          aria-label={t('pkg.queueOrder')}
          onClick={(e) => order.openAt(anchorBelow(e.currentTarget))}
        />
      )}
      {order.anchor && (
        <ContextMenu
          anchor={order.anchor}
          label={t('pkg.queueOrder')}
          onClose={order.close}
          groups={[
            {
              id: 'order',
              items: [
                { id: 'top', label: t('task.moveTop'), icon: <IconTop width={14} height={14} />, onSelect: () => void move('top') },
                { id: 'up', label: t('task.moveUp'), icon: <IconArrowUp width={14} height={14} />, onSelect: () => void move('up') },
                { id: 'down', label: t('task.moveDown'), icon: <IconArrowDown width={14} height={14} />, onSelect: () => void move('down') },
                { id: 'bottom', label: t('task.moveBottom'), icon: <IconBottom width={14} height={14} />, onSelect: () => void move('bottom') },
              ],
            },
          ]}
        />
      )}
      {dialog && (
        <PackageDialog
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

function PackageDialog({
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
          <Button onClick={() => onApply(name.trim())}>{t('pkg.merge')}</Button>
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <span className="flex-1" />
          <span className="glim-num text-xs text-carbon-textMuted">
            {count} {t('select.count')}
          </span>
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
