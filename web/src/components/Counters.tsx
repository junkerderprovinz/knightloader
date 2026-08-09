import { useMemo } from 'react';
import type { Task } from '../lib/api';
import { fmtBytes, fmtEta } from '../lib/format';
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

// --- The strip in the shell bar --------------------------------------------

/** Which rows the strip is describing. */
export type StripScope = 'total' | 'visible' | 'selected';

/**
 * A row still owes work. Finished, failed and staged-in-the-collector are all
 * out, each for its own reason: the first two are over, and a link nobody has
 * started would move the total every time somebody pasted something.
 *
 * This is the server's rule as well (app.Counters), on purpose — the strip and
 * GET /api/queue/counters must not be able to disagree about what is left.
 */
const owed = (x: Task) => x.status !== 'done' && x.status !== 'error' && x.status !== 'collected';

interface Figures {
  /** Rows in scope that are switched off, whether or not they are being counted. */
  off: number;
  total: number;
  loaded: number;
  /** Bytes that are actually going to be fetched — what the ETA divides. */
  remaining: number;
  speed: number;
}

/**
 * weigh adds up one set of rows.
 *
 * Two exclusions, and neither is tidying.
 *
 * A row whose size nobody knows yet is out of BOTH byte sums, the same way the
 * server leaves it out of its own (app.Counters). Counting its loaded bytes
 * while its size counts as zero is how the strip ends up reading "3.1 GiB of
 * 2.4 GiB", which is not a rounding error to the person looking at it — it is
 * the header contradicting itself.
 *
 * `remaining` is deliberately not `total - loaded`. A disabled link is not going
 * to be fetched, so counting its bytes into the ETA would put a number in front
 * of somebody that no amount of waiting ever works off; it stays out of the
 * division even when the reader has asked to see its bytes in the total.
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
 * OverviewStrip is what the shell bar says about the work, on every page.
 *
 * Two rules about which rows it counts, and they are different on purpose:
 *
 *   total    the queue. Finished, failed and collected rows are out, because
 *            "total" has no list beside it to agree with and what it means is
 *            what is still owed.
 *   visible  exactly the rows the list published, with no status rule laid over
 *   selected them at all. These two DO have a list underneath them, and a strip
 *            that quietly dropped rows the reader can see is the disagreement
 *            this whole arrangement exists to avoid.
 *
 * The ids come from lib/listview.ts and the figures are computed here from one
 * task stream, so the header and the list are never a tick apart on a number
 * that reads as one.
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

  // Falls back for the render only, and never writes the preference back:
  // somebody who chose "visible" on the download page has not changed their mind
  // by opening Settings for a moment.
  const effective: StripScope = view ? scope : 'total';

  const rows = useMemo(() => {
    const pick = (ids: Iterable<string>) => {
      const out: Task[] = [];
      for (const id of ids) {
        const x = tasks[id];
        // A row can leave the stream between the list publishing its ids and this
        // running; it is simply not counted rather than counted as zero.
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

  // fmtEta divides (size - loaded) by the speed, so handing it the remainder as
  // the size and nothing loaded is the same sum with one term — rather than a
  // second seconds-formatter that would round differently from the one in the
  // list's own ETA column.
  const eta = fmtEta(0, f.remaining, f.speed);

  // fmtBytes answers "—" for nothing at all, which is right in a table cell and
  // wrong here: "— of 5 GiB" reads as a fault, "0 B of 5 GiB" reads as a queue
  // that has not started.
  const bytes = (n: number) => (n > 0 ? fmtBytes(n) : '0 B');

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5" role="group" aria-label={t('strip.label')}>
      <span className="flex items-baseline gap-1.5">
        <span className="glim-num text-[15px] font-semibold leading-none text-carbon-text">{bytes(f.loaded)}</span>
        <span className="text-[11px] text-carbon-textMuted">{t('strip.of')}</span>
        <span className="glim-num text-[13px] leading-none text-carbon-textSub">{bytes(f.total)}</span>
        <InfoBubble tip={t('strip.hint')} />
      </span>

      {eta && (
        <span className="glim-num text-[11px] text-carbon-textMuted">
          {eta} {t('task.left')}
        </span>
      )}

      {/* Offered only while a list is mounted. On Settings or Accounts there is
          nothing to be visible in, and two tabs reading zero would be two things
          to read past on the way to the one that means something. */}
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

      {/* Only when the scope actually holds one. A switch that is present in
          every state is furniture; here it appears exactly when it explains why
          the total is smaller than the row count suggests. */}
      {f.off > 0 && (
        <span className="flex items-center gap-1.5">
          <Toggle checked={includeDisabled} onChange={onIncludeDisabled} label={t('strip.includeDisabled')} />
          <InfoBubble tip={t('strip.includeDisabledHint')} />
        </span>
      )}
    </div>
  );
}
