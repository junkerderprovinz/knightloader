import { useEffect, useState } from 'react';
import { fetchTasks, type Task } from '../lib/api';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, Button, IconBadge } from './ui';
import { IconTrash } from '../lib/icons';

interface Stats {
  online: boolean;
  active: number;
  total: number;
  speed: number;
}

// usePeerStats polls one instance for its live figures.
function usePeerStats(base: string): Stats | null {
  const [stats, setStats] = useState<Stats | null>(null);
  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const list: Task[] = await fetchTasks(base);
        if (!alive) return;
        const running = list.filter((x) => x.status === 'running' || x.status === 'extracting').length;
        const speed = list.reduce((s, x) => s + (x.status === 'running' ? x.speed : 0), 0);
        setStats({ online: true, active: running, total: list.length, speed });
      } catch {
        if (alive) setStats({ online: false, active: 0, total: 0, speed: 0 });
      }
    };
    load();
    const iv = setInterval(load, 3000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, [base]);
  return stats;
}

// InstanceRow is the quiet form used where instances are a summary rather than
// the subject of the page: a dot, the name, and the current speed.
export function InstanceRow({ name, base, onOpen }: { name: string; base: string; onOpen?: () => void }) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const body = (
    <>
      <span
        className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
        title={online ? t('instances.online') : t('instances.offline')}
      />
      <span className="min-w-0 flex-1 truncate text-[13.5px] text-carbon-text">{name}</span>
      <span className="glim-num text-xs text-carbon-textSub">
        {stats ? fmtSpeed(stats.speed) || '—' : '—'}
      </span>
    </>
  );
  return onOpen ? (
    <button
      onClick={onOpen}
      className="flex w-full items-center gap-3 px-5 py-3 text-left transition-colors hover:bg-carbon-hover/50"
    >
      {body}
    </button>
  ) : (
    <div className="flex items-center gap-3 px-5 py-3">{body}</div>
  );
}

// One instance at a glance: a status dot, the host, and three quiet figures.
export function InstanceCard({
  name,
  url,
  base,
  onOpen,
  onRemove,
}: {
  name: string;
  url: string;
  base: string;
  onOpen?: () => void;
  onRemove?: () => void;
}) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;

  return (
    <Card hover={!!onOpen} className="group flex h-full flex-col gap-3">
      <div className="flex items-center gap-2.5">
        <span
          className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
          title={online ? t('instances.online') : t('instances.offline')}
        />
        <span className="truncate font-semibold text-carbon-text">{name}</span>
        <span className="flex-1" />
        {onRemove && (
          <span className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            <IconBadge kind="danger" icon={<IconTrash />} title={t('instances.removeTitle', { name })} onClick={onRemove} />
          </span>
        )}
      </div>

      <div className="truncate text-xs text-carbon-textMuted">{url}</div>

      <div className="flex items-baseline gap-5">
        <Metric value={stats?.active ?? '—'} label={t('instances.metricActive')} />
        <Metric value={stats?.total ?? '—'} label={t('instances.metricTasks')} />
        <Metric value={stats ? fmtSpeed(stats.speed) || '0' : '—'} label={t('instances.metricSpeed')} />
      </div>

      {onOpen && (
        <Button kind="secondary" onClick={onOpen} className="mt-auto w-full justify-center">
          {t('instances.open')}
        </Button>
      )}
    </Card>
  );
}

function Metric({ value, label }: { value: React.ReactNode; label: string }) {
  return (
    <div className="min-w-0">
      <div className="glim-num text-sm font-semibold text-carbon-text">{value}</div>
      <div className="glim-eyebrow">{label}</div>
    </div>
  );
}
