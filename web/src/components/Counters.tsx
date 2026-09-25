import { useMemo } from 'react';
import type { Task } from '../lib/api';
import { fmtEta, fmtTotal } from '../lib/format';
import { useT } from '../lib/i18n';
import type { ListView } from '../lib/listview';
import { InfoBubble, Toggle } from './ui';
import { Tabs } from './Tabs';

export interface CountsInput {
  running: number;
  queued: number;
  collected?: number;
  done: number;
  error: number;
}

// A quiet row of figures below the speed hero. Zeros stay so the row does not
// reflow, and only a non-zero error count is highlighted.
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
          <span className={`glim-num text-[14px] font-semibold ${i.tone}`}>{i.value}</span>
          <span className="text-[11px] text-carbon-textMuted">{i.label}</span>
        </div>
      ))}
    </div>
  );
}

export type StripScope = 'total' | 'visible' | 'selected';

// A row still owes work unless it is done, failed or only collected. The
// server's app.Counters uses the same rule, so the two cannot disagree.
const owed = (x: Task) => x.status !== 'done' && x.status !== 'error' && x.status !== 'collected';

interface Figures {
  /** Rows in scope that are switched off, whether or not they are being counted. */
  off: number;
  total: number;
  loaded: number;
  /** Bytes that will actually be fetched, which the ETA divides. */
  remaining: number;
  speed: number;
}

/**
 * weigh adds up one set of rows. A row of unknown size stays out of both byte
 * sums, as in app.Counters, or loaded could exceed total. `remaining` leaves
 * disabled rows out even when the total includes them, since they will never
 * be fetched.
 */
function weigh(rows: Task[], includeDisabled: boolean): Figures {
  const f: Figures = { off: 0, total: 0, loaded: 0, remaining: 0, speed: 0 };
  for (const x of rows) {
    const disabled = !x.enabled;
    if (disabled) f.off++;
    if (x.size > 0 && (!disabled || includeDisabled)) {
      f.total += x.size;
      f.loaded += x.loaded;
    }
    if (!disabled && x.status !== 'done' && x.status !== 'error' && x.size > x.loaded) {
      f.remaining += x.size - x.loaded;
    }
    if (x.status === 'running') f.speed += x.speed;
  }
  return f;
}

/**
 * OverviewStrip is the shell bar's summary of the work on every page. "Total"
 * counts only what is owed; "visible" and "selected" count exactly the rows
 * the list published through lib/listview.ts, so they match the list.
 */
export function OverviewStrip({
  tasks,
  view,
  scope,
  onScope,
  includeDisabled,
  onIncludeDisabled,
}: {
  /** Every task in the instance being shown, by id. */
  tasks: Record<string, Task>;
  /** What the list on screen is showing, or null when no list is mounted. */
  view: ListView | null;
  scope: StripScope;
  onScope: (next: StripScope) => void;
  includeDisabled: boolean;
  onIncludeDisabled: (next: boolean) => void;
}) {
  const { t } = useT();

  // Falls back for this render only; the stored preference stays.
  const effective: StripScope = view ? scope : 'total';

  const rows = useMemo(() => {
    const pick = (ids: Iterable<string>) => {
      const out: Task[] = [];
      for (const id of ids) {
        const x = tasks[id];
        // A row can leave the stream after the list published its id.
        if (x) out.push(x);
      }
      return out;
    };
    return {
      total: Object.values(tasks).filter(owed),
      visible: view ? pick(view.visible) : [],
      selected: view ? pick(view.selected) : [],
    };
  }, [tasks, view]);

  const f = useMemo(() => weigh(rows[effective], includeDisabled), [rows, effective, includeDisabled]);

  // Reuses fmtEta so the strip rounds like the list's ETA column.
  const eta = fmtEta(0, f.remaining, f.speed);

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5" role="group" aria-label={t('strip.label')}>
      <span className="flex items-baseline gap-1.5">
        <span className="glim-num text-[14px] font-semibold leading-none text-carbon-text">{fmtTotal(f.loaded)}</span>
        <span className="text-[11px] text-carbon-textMuted">{t('strip.of')}</span>
        <span className="glim-num text-[12px] leading-none text-carbon-textSub">{fmtTotal(f.total)}</span>
        <InfoBubble tip={t('strip.hint')} />
      </span>

      {eta && (
        <span className="glim-num text-[11px] text-carbon-textMuted">
          {eta} {t('task.left')}
        </span>
      )}

      {/* Only while a list is mounted, since there is nothing visible otherwise. */}
      {view && (
        <Tabs
          select="one"
          size="sm"
          label={t('strip.scope')}
          active={effective}
          onSelect={(id) => onScope(id as StripScope)}
          items={[
            { id: 'total', label: t('strip.total'), badge: rows.total.length },
            { id: 'visible', label: t('strip.visible'), badge: rows.visible.length },
            { id: 'selected', label: t('strip.selected'), badge: rows.selected.length },
          ]}
        />
      )}

      {/* Only when the scope holds a disabled row. */}
      {f.off > 0 && (
        <span className="flex items-center gap-1.5">
          <Toggle checked={includeDisabled} onChange={onIncludeDisabled} label={t('strip.includeDisabled')} />
          <InfoBubble tip={t('strip.includeDisabledHint')} />
        </span>
      )}
    </div>
  );
}
