import { useEffect, useState } from 'react';
import { fetchTasks, type Task } from '../lib/api';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, Button } from './ui';
import { IconTrash } from '../lib/icons';

interface Stats {
  online: boolean;
  active: number;
  total: number;
  speed: number;
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
  const [stats, setStats] = useState<Stats | null>(null);

  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const list: Task[] = await fetchTasks(base);
        if (!active) return;
        const running = list.filter((x) => x.status === 'running' || x.status === 'extracting').length;
        const speed = list.reduce((s, x) => s + (x.status === 'running' ? x.speed : 0), 0);
        setStats({ online: true, active: running, total: list.length, speed });
      } catch {
        if (active) setStats({ online: false, active: 0, total: 0, speed: 0 });
      }
    };
    load();
    const iv = setInterval(load, 3000);
    return () => {
      active = false;
      clearInterval(iv);
    };
  }, [base]);

  const online = stats?.online ?? false;

  return (
    <Card hover={!!onOpen} className="group flex flex-col gap-3">
      <div className="flex items-center gap-2.5">
        <span
          className={`h-2 w-2 shrink-0 rounded-full ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
          title={online ? t('instances.online') : t('instances.offline')}
        />
        <span className="truncate font-semibold text-carbon-text">{name}</span>
        <span className="flex-1" />
        {onRemove && (
          <span className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            <Button
              kind="danger"
              icon={<IconTrash />}
              title={t('instances.removeTitle', { name })}
              onClick={onRemove}
            />
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
        <Button kind="secondary" onClick={onOpen} className="w-full justify-center">
          {t('instances.open')}
        </Button>
      )}
    </Card>
  );
}

function Metric({ value, label }: { value: React.ReactNode; label: string }) {
  return (
    <div className="min-w-0">
      <div className="kl-num text-sm font-semibold text-carbon-text">{value}</div>
      <div className="kl-eyebrow">{label}</div>
    </div>
  );
}
