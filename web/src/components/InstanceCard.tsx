import { useEffect, useState } from 'react';
import { fetchTasks, type Task } from '../lib/api';
import { fmtSpeed } from '../lib/format';
import { Card, Button } from './ui';
import { IconTrash } from '../lib/icons';

interface Stats {
  online: boolean;
  active: number;
  total: number;
  speed: number;
}

// InstanceCard polls one instance and shows its live status (a my.jdownloader-
// style tile). base '' means this instance.
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
  const [stats, setStats] = useState<Stats | null>(null);
  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const list: Task[] = await fetchTasks(base);
        if (!active) return;
        const running = list.filter((t) => t.status === 'running' || t.status === 'extracting').length;
        const speed = list.reduce((s, t) => s + (t.status === 'running' ? t.speed : 0), 0);
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
    <Card hover className="flex flex-col gap-3">
      <div className="flex items-center gap-2.5">
        <span
          className={`h-2.5 w-2.5 rounded-full ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
          title={online ? 'Online' : 'Offline'}
        />
        <span className="font-semibold text-carbon-text truncate">{name}</span>
        <span className="flex-1" />
        {onRemove && (
          <Button kind="danger" icon={<IconTrash />} title={`Remove ${name}`} onClick={onRemove} />
        )}
      </div>
      <div className="text-carbon-textMuted text-xs truncate">{url}</div>
      <div className="grid grid-cols-3 gap-2 text-center">
        <Metric value={stats?.active ?? '—'} label="Active" />
        <Metric value={stats?.total ?? '—'} label="Tasks" />
        <Metric value={stats ? fmtSpeed(stats.speed) || '0' : '—'} label="Speed" />
      </div>
      {onOpen && (
        <Button kind="secondary" onClick={onOpen} className="w-full justify-center">
          Open
        </Button>
      )}
    </Card>
  );
}

function Metric({ value, label }: { value: React.ReactNode; label: string }) {
  return (
    <div className="rounded-lg bg-carbon-surface2/50 py-2">
      <div className="font-semibold tabular-nums text-carbon-text text-sm">{value}</div>
      <div className="text-carbon-textMuted text-[10px] uppercase tracking-wide">{label}</div>
    </div>
  );
}
