import { type Task } from '../lib/api';
import { pause, resume, remove, startTasks, restartTasks } from '../lib/api';
import { fmtBytes, fmtSpeed, fmtEta, pct } from '../lib/format';
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

function TaskRow({ t, base, selection }: { t: Task; base: string; selection?: Selection }) {
  const p = pct(t.loaded, t.size, t.status === 'done');
  const eta = fmtEta(t.loaded, t.size, t.speed);
  const collected = t.status === 'collected';
  return (
    <div className="flex items-center gap-4 px-5 py-3 transition-colors hover:bg-carbon-hover/40 border-t border-carbon-border/50 first:border-t-0">
      {selection && <Checkbox checked={selection.ids.has(t.id)} onChange={() => selection.toggle(t.id)} />}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-carbon-text">{t.name || t.url}</span>
          <ResolverBadge resolver={t.resolver} />
        </div>
        {t.error && <div className="text-statusFail text-xs mt-0.5 truncate">{t.error}</div>}
        {!collected && (
          <div className="mt-1.5 flex items-center gap-3">
            <div className="flex-1 max-w-md">
              <ProgressBar percent={p} active={t.status !== 'error'} indeterminate={t.status === 'queued'} />
            </div>
            <span className="text-carbon-textMuted text-[11px] tabular-nums whitespace-nowrap">
              {p}%{fmtSpeed(t.speed) && ` · ${fmtSpeed(t.speed)}`}
              {eta && ` · ${eta} left`}
            </span>
          </div>
        )}
        {collected && !t.error && (
          <div className="text-carbon-textMuted text-[11px] mt-0.5">Ready to download</div>
        )}
      </div>
      <span className="w-20 text-right text-carbon-textSub text-sm tabular-nums">{fmtBytes(t.size)}</span>
      <StatusPill status={t.status} />
      <div className="flex items-center gap-0.5">
        {collected && (
          <Button kind="ghost" icon={<IconPlay />} title="Start" onClick={() => startTasks([t.id], base)} />
        )}
        {t.status === 'running' && (
          <Button kind="ghost" icon={<IconPause />} title="Pause" onClick={() => pause(t.id, base)} />
        )}
        {t.status === 'paused' && (
          <Button kind="ghost" icon={<IconPlay />} title="Resume" onClick={() => resume(t.id, base)} />
        )}
        {(t.status === 'error' || t.status === 'done') && (
          <Button kind="ghost" icon={<IconRetry />} title="Restart" onClick={() => restartTasks([t.id], base)} />
        )}
        <Button kind="danger" icon={<IconTrash />} title="Remove" onClick={() => remove(t.id, base)} />
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
  const total = items.reduce((s, t) => s + t.size, 0);
  const loaded = items.reduce((s, t) => s + t.loaded, 0);
  const allSelected = selection && items.every((t) => selection.ids.has(t.id));
  return (
    <Card className="flex flex-col gap-0 p-0 overflow-hidden">
      <div className="flex items-center gap-3 px-5 py-3 bg-carbon-surface2/60">
        {selection && (
          <Checkbox
            checked={!!allSelected}
            onChange={() => {
              const target = !allSelected;
              items.forEach((t) => {
                if (selection.ids.has(t.id) !== target) selection.toggle(t.id);
              });
            }}
          />
        )}
        <span className="font-semibold text-carbon-text">{name || 'Ungrouped'}</span>
        <span className="text-carbon-textMuted text-xs">
          {items.length} {items.length === 1 ? 'file' : 'files'}
          {total > 0 && ` · ${fmtBytes(loaded)} / ${fmtBytes(total)}`}
        </span>
      </div>
      <div className="flex flex-col">
        {items.map((t) => (
          <TaskRow key={t.id} t={t} base={base} selection={selection} />
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
