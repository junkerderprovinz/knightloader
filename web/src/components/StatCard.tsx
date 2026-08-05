import type { ReactNode } from 'react';
import { Card } from './ui';

// StatCard is a dashboard tile: a big value + label, with an optional accent tone.
export function StatCard({
  label,
  value,
  sub,
  tone = 'text-carbon-text',
  icon,
}: {
  label: string;
  value: ReactNode;
  sub?: string;
  tone?: string;
  icon?: ReactNode;
}) {
  return (
    <Card className="flex items-start gap-3">
      {icon && <div className="text-carbon-textMuted mt-0.5">{icon}</div>}
      <div className="min-w-0">
        <div className={`text-2xl font-bold tabular-nums leading-tight ${tone}`}>{value}</div>
        <div className="text-carbon-textSub text-xs mt-0.5">{label}</div>
        {sub && <div className="text-carbon-textMuted text-[11px] mt-0.5">{sub}</div>}
      </div>
    </Card>
  );
}
