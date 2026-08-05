import { useEffect, useMemo, useRef, useState } from 'react';
import {
  type Task,
  type Instance,
  type TaskStatus,
  apiBase,
  fetchTasks,
  addLinks,
  pause,
  resume,
  remove,
  connectWS,
  fetchInstances,
} from '../lib/api';
import { fmtBytes, fmtSpeed, fmtEta, pct } from '../lib/format';
import { Button, TextArea, TextInput, Card } from '../components/ui';
import { ProgressBar } from '../components/ProgressBar';
import { StatusPill, ResolverBadge } from '../components/StatusPill';
import { IconPause, IconPlay, IconTrash, IconPlus } from '../lib/icons';

const isActive = (s: TaskStatus) => s === 'running' || s === 'queued' || s === 'extracting';

export function Downloads() {
  const [tasks, setTasks] = useState<Record<string, Task>>({});
  const [links, setLinks] = useState('');
  const [pkg, setPkg] = useState('');
  const [instances, setInstances] = useState<Instance[]>([]);
  const [instance, setInstance] = useState('');
  const closer = useRef<(() => void) | null>(null);
  const base = apiBase(instance);

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  const reload = () =>
    fetchTasks(base).then((l) => setTasks(Object.fromEntries((l ?? []).map((t) => [t.id, t]))));

  useEffect(() => {
    setTasks({});
    reload();
    if (instance) {
      const iv = setInterval(reload, 2000);
      return () => clearInterval(iv);
    }
    closer.current = connectWS((type, data) => {
      if (type === 'snapshot') setTasks(Object.fromEntries((data ?? []).map((t: Task) => [t.id, t])));
      else if (type === 'task') setTasks((p) => ({ ...p, [data.id]: data }));
      else if (type === 'removed')
        setTasks((p) => {
          const n = { ...p };
          delete n[data.id];
          return n;
        });
    });
    return () => closer.current?.();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instance]);

  const list = useMemo(
    () => Object.values(tasks).sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [tasks],
  );

  // Group by package (LinkGrabber-style staging); unpackaged tasks go last.
  const groups = useMemo(() => {
    const m = new Map<string, Task[]>();
    for (const t of list) {
      const key = t.package || '';
      (m.get(key) ?? m.set(key, []).get(key)!).push(t);
    }
    return [...m.entries()];
  }, [list]);

  const stats = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      speed = 0;
    for (const t of list) {
      if (t.status === 'running' || t.status === 'extracting') running++;
      else if (t.status === 'queued') queued++;
      else if (t.status === 'done') done++;
      else if (t.status === 'error') error++;
      if (t.status === 'running') speed += t.speed;
    }
    return { running, queued, done, error, speed, total: list.length };
  }, [list]);

  async function onAdd() {
    if (!links.trim()) return;
    await addLinks(links, pkg, base);
    setLinks('');
    if (instance) reload();
  }

  const pauseAll = () => list.filter((t) => t.status === 'running').forEach((t) => pause(t.id, base));
  const resumeAll = () => list.filter((t) => t.status === 'paused').forEach((t) => resume(t.id, base));
  const clearDone = () =>
    list.filter((t) => t.status === 'done' || t.status === 'error').forEach((t) => remove(t.id, base));

  return (
    <div className="flex flex-col gap-5">
      <header className="flex items-center gap-3">
        <h1 className="text-2xl font-bold text-carbon-text">Downloads</h1>
        <span className="flex-1" />
        {instances.length > 0 && (
          <select
            value={instance}
            onChange={(e) => setInstance(e.target.value)}
            className="rounded-lg bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text outline-none"
            aria-label="Instance"
          >
            <option value="">This instance</option>
            {instances.map((i) => (
              <option key={i.name} value={i.name}>
                {i.name}
              </option>
            ))}
          </select>
        )}
      </header>

      {/* Add links */}
      <Card className="flex flex-col gap-3">
        <TextArea
          placeholder="Paste links — one URL per line…"
          rows={3}
          value={links}
          onChange={(e) => setLinks(e.target.value)}
        />
        <div className="flex items-center gap-3">
          <TextInput
            placeholder="Package (optional)"
            value={pkg}
            onChange={(e) => setPkg(e.target.value)}
            className="max-w-xs"
          />
          <span className="flex-1" />
          <Button icon={<IconPlus />} onClick={onAdd} disabled={!links.trim()}>
            Add to queue
          </Button>
        </div>
      </Card>

      {/* Status bar */}
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-card bg-carbon-surface px-4 py-3 text-sm">
        <Stat label="Active" value={stats.running} tone="text-statusInfo" />
        <Stat label="Queued" value={stats.queued} tone="text-statusNeutral" />
        <Stat label="Done" value={stats.done} tone="text-statusOk" />
        {stats.error > 0 && <Stat label="Errors" value={stats.error} tone="text-statusFail" />}
        <div className="flex items-baseline gap-1.5">
          <span className="text-carbon-textMuted text-xs">Total</span>
          <span className="font-semibold tabular-nums text-carbon-text">{fmtSpeed(stats.speed) || '—'}</span>
        </div>
        <span className="flex-1" />
        <div className="flex items-center gap-1">
          <Button kind="ghost" onClick={pauseAll} disabled={stats.running === 0}>
            Pause all
          </Button>
          <Button kind="ghost" onClick={resumeAll}>
            Resume all
          </Button>
          <Button kind="ghost" onClick={clearDone} disabled={stats.done + stats.error === 0}>
            Clear finished
          </Button>
        </div>
      </div>

      {/* Task groups */}
      {list.length === 0 ? (
        <Card className="text-center text-carbon-textMuted py-10">
          No downloads yet — paste a link above to get started.
        </Card>
      ) : (
        groups.map(([name, items]) => (
          <PackageGroup key={name || '__none'} name={name} items={items} base={base} />
        ))
      )}
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className={`font-semibold tabular-nums ${tone}`}>{value}</span>
      <span className="text-carbon-textMuted text-xs">{label}</span>
    </div>
  );
}

function PackageGroup({ name, items, base }: { name: string; items: Task[]; base: string }) {
  const total = items.reduce((s, t) => s + t.size, 0);
  const loaded = items.reduce((s, t) => s + t.loaded, 0);
  return (
    <Card className="flex flex-col gap-1 p-0 overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-2.5 bg-carbon-surface2">
        <span className="font-semibold text-carbon-text">{name || 'Ungrouped'}</span>
        <span className="text-carbon-textMuted text-xs">
          {items.length} {items.length === 1 ? 'file' : 'files'}
          {total > 0 && ` · ${fmtBytes(loaded)} / ${fmtBytes(total)}`}
        </span>
      </div>
      <div className="flex flex-col">
        {items.map((t) => (
          <TaskRow key={t.id} t={t} base={base} />
        ))}
      </div>
    </Card>
  );
}

function TaskRow({ t, base }: { t: Task; base: string }) {
  const p = pct(t.loaded, t.size, t.status === 'done');
  const eta = fmtEta(t.loaded, t.size, t.speed);
  return (
    <div className="flex items-center gap-4 px-4 py-3 border-t border-carbon-border/60 first:border-t-0">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-carbon-text">{t.name || t.url}</span>
          <ResolverBadge resolver={t.resolver} />
        </div>
        {t.error && <div className="text-statusFail text-xs mt-0.5 truncate">{t.error}</div>}
        <div className="mt-1.5 flex items-center gap-3">
          <div className="flex-1 max-w-md">
            <ProgressBar percent={p} active={t.status !== 'error'} indeterminate={t.status === 'queued'} />
          </div>
          <span className="text-carbon-textMuted text-[11px] tabular-nums whitespace-nowrap">
            {p}%
            {fmtSpeed(t.speed) && ` · ${fmtSpeed(t.speed)}`}
            {eta && ` · ${eta} left`}
          </span>
        </div>
      </div>
      <span className="w-20 text-right text-carbon-textSub text-sm tabular-nums">{fmtBytes(t.size)}</span>
      <StatusPill status={t.status} />
      <div className="flex items-center gap-0.5">
        {t.status === 'running' && (
          <Button kind="ghost" icon={<IconPause />} title="Pause" onClick={() => pause(t.id, base)} />
        )}
        {t.status === 'paused' && (
          <Button kind="ghost" icon={<IconPlay />} title="Resume" onClick={() => resume(t.id, base)} />
        )}
        <Button kind="danger" icon={<IconTrash />} title="Remove" onClick={() => remove(t.id, base)} />
      </div>
    </div>
  );
}
