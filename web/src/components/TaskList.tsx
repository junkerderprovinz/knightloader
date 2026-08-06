import { useState, type CSSProperties } from 'react';
import { type Task } from '../lib/api';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import {
  pause,
  resume,
  remove,
  startTasks,
  restartTasks,
  recheckTasks,
  setTaskOptions,
} from '../lib/api';
import { fmtBytes, fmtSpeed, fmtEta, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { Button, Field, Modal, TextInput } from './ui';
import { ProgressBar } from './ProgressBar';
import { StatusPill, ResolverBadge } from './StatusPill';
import {
  IconPause,
  IconPlay,
  IconTrash,
  IconCheck,
  IconRetry,
  IconFolder,
  IconSearch,
} from '../lib/icons';

export interface Selection {
  ids: Set<string>;
  toggle: (id: string) => void;
}

// One grid for the package header and its file rows, so totals sit exactly
// above the values they sum.
const ROW = 'grid items-center gap-x-4 grid-cols-[auto_minmax(0,1fr)_5.5rem_7rem_auto]';

function Checkbox({ checked, onChange }: { checked: boolean; onChange: () => void }) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      onClick={onChange}
      className={`grid h-4.5 w-4.5 shrink-0 place-items-center rounded-[var(--radius-control)] transition-colors ${
        checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent hover:bg-carbon-surface3'
      }`}
      style={{ height: '1.125rem', width: '1.125rem' }}
    >
      <IconCheck width={12} height={12} />
    </button>
  );
}

function TaskRow({
  t: task,
  base,
  selection,
  showResolver = true,
  index = 0,
}: {
  t: Task;
  base: string;
  selection?: Selection;
  showResolver?: boolean;
  /** Position in the rendered list — the rainbow palette position. */
  index?: number;
}) {
  const { t } = useT();
  const [options, setOptions] = useState(false);
  useRainbow(); // re-render this row when the palette or the mode changes
  const p = pct(task.loaded, task.size, task.status === 'done');
  const eta = fmtEta(task.loaded, task.size, task.speed);
  const collected = task.status === 'collected';
  const settled = task.status === 'done' || task.status === 'error';
  // A pending automatic retry is not the same as a dead task, and saying so
  // stops people from restarting something that is already about to restart.
  const retrying = task.status === 'error' && !!task.nextTry;

  // In rainbow mode the row owns a colour, and everything inside it that paints
  // activity — the progress fill above all — reads it through --accent without
  // knowing the mode exists. A running row counts as active, so the reactive
  // reading still shows colour where work is actually happening.
  //
  // The colour comes from the row's position, not from a hash of its id. A hash
  // is stable when rows above finish, which sounds better until three rows land
  // in the same bucket and two neighbours share a colour — which is the one
  // thing the mode exists to prevent. By position, eight adjacent rows always
  // differ.
  return (
    <div
      style={hueVars(rainbowAt(index)) as CSSProperties}
      className={`glim-hue glim-tint ${task.status === 'running' ? 'glim-active' : ''} ${ROW} relative group px-5 py-3 transition-colors hover:bg-carbon-hover/50`}
    >
      {selection ? (
        <Checkbox checked={selection.ids.has(task.id)} onChange={() => selection.toggle(task.id)} />
      ) : (
        <span />
      )}

      <div className="min-w-0">
        <div dir="ltr" className="truncate text-start text-[13.5px] text-carbon-text">{task.name || task.url}</div>
        {task.error ? (
          <div className="mt-0.5 flex items-center gap-2 text-[11px]">
            <span className="truncate text-statusFail">{task.error}</span>
            {retrying && <span className="shrink-0 text-carbon-textMuted">· {t('task.retryPending')}</span>}
          </div>
        ) : collected ? (
          <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-carbon-textMuted">
            {showResolver && (
              <>
                <ResolverBadge resolver={task.resolver} /> ·{' '}
              </>
            )}
            {task.online === 'online' ? (
              <span className="text-statusOk">{t('task.online')}</span>
            ) : task.online === 'offline' ? (
              <span className="text-statusFail">{t('task.offline')}</span>
            ) : (
              t('task.ready')
            )}
            {task.dir && <span className="truncate">· {task.dir}</span>}
          </div>
        ) : (
          // The bar already carries the percentage; the line beside it adds
          // only what the bar can't say.
          <div className="mt-1.5 flex items-center gap-3">
            <div className="w-full max-w-xs">
              <ProgressBar
                percent={p}
                active={task.status !== 'error'}
                indeterminate={task.status === 'queued'}
                tone={task.status === 'done' ? 'ok' : 'accent'}
              />
            </div>
            <span className="glim-num whitespace-nowrap text-[11px] text-carbon-textMuted">
              {fmtSpeed(task.speed)}
              {eta && `${fmtSpeed(task.speed) ? ' · ' : ''}${eta} ${t('task.left')}`}
            </span>
            {showResolver && <ResolverBadge resolver={task.resolver} />}
          </div>
        )}
      </div>

      <span className="glim-num text-right text-[13px] text-carbon-textSub">{fmtBytes(task.size)}</span>
      <div className="flex items-center gap-2">
        <StatusPill status={task.status} />
        {/* Only a real verdict is shown. An unverified download stays unmarked,
            because a tick that also means "not checked" is worse than none. */}
        {task.checksum === 'ok' && (
          <span title={t('task.checksumOk')} className="text-statusOk">
            <IconCheck width={13} height={13} />
          </span>
        )}
        {task.checksum === 'failed' && (
          <span title={t('task.checksumFail')} className="text-statusFail text-[11px] font-semibold">
            !
          </span>
        )}
      </div>

      {/* The primary action stays visible; the rest appears on hover or focus,
          so a long list reads as content instead of a wall of buttons. */}
      <div className="flex items-center justify-end gap-0.5">
        {collected && (
          <Button kind="ghost" icon={<IconPlay />} title={t('task.start')} onClick={() => startTasks([task.id], base)} />
        )}
        {task.status === 'running' && (
          <Button kind="ghost" icon={<IconPause />} title={t('task.pause')} onClick={() => pause(task.id, base)} />
        )}
        {task.status === 'paused' && (
          <Button kind="ghost" icon={<IconPlay />} title={t('task.resume')} onClick={() => resume(task.id, base)} />
        )}
        <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          {collected && (
            <Button
              kind="ghost"
              icon={<IconSearch />}
              title={t('task.recheck')}
              onClick={() => recheckTasks([task.id], base)}
            />
          )}
          <Button kind="ghost" icon={<IconFolder />} title={t('task.folder')} onClick={() => setOptions(true)} />
          {settled && (
            <Button
              kind="ghost"
              icon={<IconRetry />}
              title={t('task.restart')}
              onClick={() => restartTasks([task.id], base)}
            />
          )}
          <Button kind="danger" icon={<IconTrash />} title={t('task.remove')} onClick={() => remove(task.id, base)} />
        </div>
      </div>

      {options && <TaskOptionsDialog task={task} base={base} onClose={() => setOptions(false)} />}
    </div>
  );
}

// TaskOptionsDialog edits the per-task overrides: where this file goes and the
// password its archive needs. Both are left alone unless actually changed.
function TaskOptionsDialog({ task, base, onClose }: { task: Task; base: string; onClose: () => void }) {
  const { t } = useT();
  const [dir, setDir] = useState(task.dir ?? '');
  const [password, setPassword] = useState(task.password ?? '');
  const [error, setError] = useState('');

  async function apply() {
    const r = await setTaskOptions([task.id], { dir, password }, base);
    if (!r.ok) {
      setError(await r.text());
      return;
    }
    onClose();
  }

  return (
    <Modal
      title={task.name || task.url}
      onClose={onClose}
      footer={
        <>
          <Button onClick={apply}>{t('settings.save')}</Button>
          {error && <span className="text-statusFail text-sm">{error}</span>}
        </>
      }
    >
      <Field label={t('task.folder')} hint={t('settings.downloadDirHint')}>
        <TextInput dir="ltr" value={dir} spellCheck={false} onChange={(e) => setDir(e.target.value)} />
      </Field>
      <Field label={t('task.password')}>
        <TextInput value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
    </Modal>
  );
}

// PackageGroup is a plain block inside the list card — not a nested card.
export function PackageGroup({
  name,
  items,
  base,
  selection,
}: {
  name: string;
  items: Task[];
  base: string;
  selection?: Selection;
}) {
  const { t } = useT();
  const total = items.reduce((s, x) => s + x.size, 0);
  const loaded = items.reduce((s, x) => s + x.loaded, 0);
  const done = items.filter((x) => x.status === 'done').length;
  const allSelected = selection && items.every((x) => selection.ids.has(x.id));
  const groupPct = total > 0 ? Math.min(100, Math.round((loaded / total) * 100)) : 0;
  // When a package runs entirely through one backend, name it once here instead
  // of repeating it on every row.
  const resolvers = new Set(items.map((x) => x.resolver));
  const uniformResolver = resolvers.size === 1 ? items[0].resolver : null;

  return (
    <section>
      <div className={`${ROW} px-5 py-2.5`}>
        {selection ? (
          <Checkbox
            checked={!!allSelected}
            onChange={() => {
              const target = !allSelected;
              items.forEach((x) => {
                if (selection.ids.has(x.id) !== target) selection.toggle(x.id);
              });
            }}
          />
        ) : (
          <span />
        )}
        <div className="flex items-baseline gap-2 min-w-0">
          <span className="truncate text-[13px] font-semibold text-carbon-text">{name || t('task.ungrouped')}</span>
          <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">
            {items.length} {items.length === 1 ? t('task.file') : t('task.files')}
            {done > 0 && ` · ${done} ${t('overview.done').toLowerCase()}`}
          </span>
          {uniformResolver && <ResolverBadge resolver={uniformResolver} />}
        </div>
        {/* Size and its share sit together in the size column, so the status
            column below never has a number floating above it. */}
        <span className="glim-num text-right text-[11px] text-carbon-textMuted">
          {fmtBytes(total)}
          {total > 0 && ` · ${groupPct}%`}
        </span>
        <span />
        <span />
      </div>
      <div className="flex flex-col">
        {items.map((x, i) => (
          <TaskRow key={x.id} t={x} index={i} base={base} selection={selection} showResolver={!uniformResolver} />
        ))}
      </div>
    </section>
  );
}

// TaskListCard holds every package group on one surface.
export function TaskListCard({
  groups,
  base,
  selection,
}: {
  groups: [string, Task[]][];
  base: string;
  selection?: Selection;
}) {
  return (
    <div className="glim-card divide-y divide-carbon-border/60 py-1">
      {groups.map(([name, items]) => (
        <PackageGroup key={name || '__none'} name={name} items={items} base={base} selection={selection} />
      ))}
    </div>
  );
}

// groupByPackage groups tasks by their package, preserving insertion order.
export function groupByPackage(list: Task[]): [string, Task[]][] {
  const m = new Map<string, Task[]>();
  for (const t of list) {
    const arr = m.get(t.package || '');
    if (arr) arr.push(t);
    else m.set(t.package || '', [t]);
  }
  return [...m.entries()];
}
