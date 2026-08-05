import { type Task } from '../lib/api';
import { pause, resume, remove, startTasks, restartTasks } from '../lib/api';
import { fmtBytes, fmtSpeed, fmtEta, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { Button } from './ui';
import { ProgressBar } from './ProgressBar';
import { StatusPill, ResolverBadge } from './StatusPill';
import { IconPause, IconPlay, IconTrash, IconCheck, IconRetry } from '../lib/icons';

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
      className={`grid h-4.5 w-4.5 shrink-0 place-items-center rounded-[5px] transition-colors ${
        checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent hover:bg-carbon-surface3'
      }`}
      style={{ height: '1.125rem', width: '1.125rem' }}
    >
      <IconCheck width={12} height={12} />
    </button>
  );
}

function TaskRow({ t: task, base, selection }: { t: Task; base: string; selection?: Selection }) {
  const { t } = useT();
  const p = pct(task.loaded, task.size, task.status === 'done');
  const eta = fmtEta(task.loaded, task.size, task.speed);
  const collected = task.status === 'collected';
  const settled = task.status === 'done' || task.status === 'error';

  return (
    <div className={`${ROW} group px-5 py-3 transition-colors hover:bg-carbon-hover/50`}>
      {selection ? (
        <Checkbox checked={selection.ids.has(task.id)} onChange={() => selection.toggle(task.id)} />
      ) : (
        <span />
      )}

      <div className="min-w-0">
        <div className="truncate text-[13.5px] text-carbon-text">{task.name || task.url}</div>
        {task.error ? (
          <div className="mt-0.5 truncate text-[11px] text-statusFail">{task.error}</div>
        ) : collected ? (
          <div className="mt-0.5 text-[11px] text-carbon-textMuted">
            <ResolverBadge resolver={task.resolver} /> · {t('task.ready')}
          </div>
        ) : (
          <div className="mt-1.5 flex items-center gap-3">
            <div className="w-full max-w-xs">
              <ProgressBar
                percent={p}
                active={task.status !== 'error'}
                indeterminate={task.status === 'queued'}
                tone={task.status === 'done' ? 'ok' : 'accent'}
              />
            </div>
            <span className="kl-num whitespace-nowrap text-[11px] text-carbon-textMuted">
              {p}%{fmtSpeed(task.speed) && ` · ${fmtSpeed(task.speed)}`}
              {eta && ` · ${eta} ${t('task.left')}`}
            </span>
            <ResolverBadge resolver={task.resolver} />
          </div>
        )}
      </div>

      <span className="kl-num text-right text-[13px] text-carbon-textSub">{fmtBytes(task.size)}</span>
      <StatusPill status={task.status} />

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
    </div>
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
          <span className="kl-num shrink-0 text-[11px] text-carbon-textMuted">
            {items.length} {items.length === 1 ? t('task.file') : t('task.files')}
            {done > 0 && ` · ${done} ${t('overview.done').toLowerCase()}`}
          </span>
        </div>
        <span className="kl-num text-right text-[11px] text-carbon-textMuted">{fmtBytes(total)}</span>
        <span className="kl-num text-[11px] text-carbon-textMuted">{total > 0 ? `${groupPct}%` : ''}</span>
        <span />
      </div>
      <div className="flex flex-col">
        {items.map((x) => (
          <TaskRow key={x.id} t={x} base={base} selection={selection} />
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
    <div className="kl-card divide-y divide-carbon-border/60 py-1">
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
