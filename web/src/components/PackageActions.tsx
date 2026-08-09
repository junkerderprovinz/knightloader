import { useMemo, useState } from 'react';
import { type QueueMove, type Task, queueMove, setPackage } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, Field, Modal, TextInput } from './ui';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { IconArrowDown, IconArrowUp, IconBottom, IconTop } from '../lib/icons';

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
 * PackageActions is the package-organising strip shown above a selection.
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
      <Button kind="ghost" className="px-2.5 text-xs" onClick={() => setDialog(true)}>
        {t('pkg.moveTitle')}
      </Button>
      <Button kind="ghost" className="px-2.5 text-xs" onClick={splitByHost}>
        {t('pkg.splitByHost')}
      </Button>
      {/* One word rather than four more icon buttons beside the four the strip
          already carries for the selection. The two sets do different things to
          different rows, and side by side as identical arrows they would be
          told apart only by hovering for a tooltip. */}
      {packages.length === 1 && (
        <Button
          kind="ghost"
          className="px-2.5 text-xs"
          onClick={(e) => order.openAt(anchorBelow(e.currentTarget))}
        >
          {t('pkg.queueOrder')}
        </Button>
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
