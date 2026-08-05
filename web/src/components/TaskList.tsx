import { type Task } from '../lib/api';
import { pause, resume, remove, startTasks, restartTasks } from '../lib/api';
import { fmtBytes, fmtSpeed, fmtEta, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, Button } from './ui';
import { ProgressBar } from './ProgressBar';
import { StatusPill, ResolverBadge } from './StatusPill';
import { IconPause, IconPlay, IconTrash, IconCheck, IconRetry } from '../lib/icons';

export interface Selection {
  ids: Set<string>;
  toggle: (id: string) => void;
}

function Checkbox({ checked, onChange }: { checked: boolean; onChange: () => void }) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      onClick={onChange}
      className={`grid h-5 w-5 shrink-0 place-items-center rounded transition-colors ${
        checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface2 text-transparent hover:bg-carbon-surface3'
      }`}
    >
      <IconCheck width={14} height={14} />
    </button>
  );
}

function TaskRow({ t: task, base, selection }: { t: Task; base: string; selection?: Selection }) {
  const { t } = useT();
  const p = pct(task.loaded, task.size, task.status === 'done');
  const eta = fmtEta(task.loaded, task.size, task.speed);
  const collected = task.status === 'collected';
  return (
    <div className="flex items-center gap-4 px-5 py-3 transition-colors hover:bg-carbon-hover/40 border-t border-carbon-border/50 first:border-t-0">
      {selection && <Checkbox checked={selection.ids.has(task.id)} onChange={() => selection.toggle(task.id)} />}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-carbon-text">{task.name || task.url}</span>
          <ResolverBadge resolver={task.resolver} />
        </div>
        {task.error && <div className="text-statusFail text-xs mt-0.5 truncate">{task.error}</div>}
        {!collected && (
          <div className="mt-1.5 flex items-center gap-3">
            <div className="flex-1 max-w-md">
              <ProgressBar percent={p} active={task.status !== 'error'} indeterminate={task.status === 'queued'} />
            </div>
            <span className="text-carbon-textMuted text-[11px] tabular-nums whitespace-nowrap">
              {p}%{fmtSpeed(task.speed) && ` · ${fmtSpeed(task.speed)}`}
              {eta && ` · ${eta} ${t('task.left')}`}
            </span>
          </div>
        )}
        {collected && !task.error && <div className="text-carbon-textMuted text-[11px] mt-0.5">{t('task.ready')}</div>}
      </div>
      <span className="w-20 text-right text-carbon-textSub text-sm tabular-nums">{fmtBytes(task.size)}</span>
      <StatusPill status={task.status} />
      <div className="flex items-center gap-0.5">
        {collected && (
          <Button kind="ghost" icon={<IconPlay />} title={t('task.start')} onClick={() => startTasks([task.id], base)} />
        )}
        {task.status === 'running' && (
          <Button kind="ghost" icon={<IconPause />} title={t('task.pause')} onClick={() => pause(task.id, base)} />
        )}
        {task.status === 'paused' && (
          <Button kind="ghost" icon={<IconPlay />} title={t('task.resume')} onClick={() => resume(task.id, base)} />
        )}
        {(task.status === 'error' || task.status === 'done') && (
          <Button kind="ghost" icon={<IconRetry />} title={t('task.restart')} onClick={() => restartTasks([task.id], base)} />
        )}
        <Button kind="danger" icon={<IconTrash />} title={t('task.remove')} onClick={() => remove(task.id, base)} />
      </div>
    </div>
  );
}

// PackageGroup renders one package card with an aggregate header and its rows.
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
  const allSelected = selection && items.every((x) => selection.ids.has(x.id));
  return (
    <Card className="flex flex-col gap-0 p-0 overflow-hidden">
      <div className="flex items-center gap-3 px-5 py-3 bg-carbon-surface2/60">
        {selection && (
          <Checkbox
            checked={!!allSelected}
            onChange={() => {
              const target = !allSelected;
              items.forEach((x) => {
                if (selection.ids.has(x.id) !== target) selection.toggle(x.id);
              });
            }}
          />
        )}
        <span className="font-semibold text-carbon-text">{name || t('task.ungrouped')}</span>
        <span className="text-carbon-textMuted text-xs">
          {items.length} {items.length === 1 ? t('task.file') : t('task.files')}
          {total > 0 && ` · ${fmtBytes(loaded)} / ${fmtBytes(total)}`}
        </span>
      </div>
      <div className="flex flex-col">
        {items.map((x) => (
          <TaskRow key={x.id} t={x} base={base} selection={selection} />
        ))}
      </div>
    </Card>
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
