import { useT } from '../lib/i18n';

export interface CountsInput {
  running: number;
  queued: number;
  collected?: number;
  done: number;
  error: number;
}

// A single quiet row of figures. Deliberately not five cards: the counters are
// supporting detail, and only the speed hero above them should carry weight.
// Zero-valued entries stay visible so the row doesn't reflow while working;
// only errors highlight, and only when there are any.
export function Counters({ counts }: { counts: CountsInput }) {
  const { t } = useT();
  const items: { label: string; value: number; tone: string }[] = [
    { label: t('overview.active'), value: counts.running, tone: 'text-statusInfo' },
    { label: t('overview.queued'), value: counts.queued, tone: 'text-carbon-text' },
    ...(counts.collected === undefined
      ? []
      : [{ label: t('overview.inCollector'), value: counts.collected, tone: 'text-carbon-text' }]),
    { label: t('overview.done'), value: counts.done, tone: 'text-carbon-text' },
    {
      label: t('overview.errors'),
      value: counts.error,
      tone: counts.error > 0 ? 'text-statusFail' : 'text-carbon-textMuted',
    },
  ];
  return (
    <div className="flex flex-wrap items-baseline gap-x-7 gap-y-2">
      {items.map((i) => (
        <div key={i.label} className="flex items-baseline gap-1.5">
          <span className={`glim-num text-[15px] font-semibold ${i.tone}`}>{i.value}</span>
          <span className="text-[11px] text-carbon-textMuted">{i.label}</span>
        </div>
      ))}
    </div>
  );
}
