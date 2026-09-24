import { useEffect, useMemo, useRef, useState } from 'react';
import type { LogLine } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { Card, ErrorCard, InfoBubble, LoadingCard, SectionTitle, TextInput, ToggleRow, useTooltip } from '../../../components/ui';
import { useLogTail } from './useLogTail';

/**
 * LogViewerCard shows the log with a search box, a source picker and a switch
 * that follows the newest line. It filters by source because the log calls
 * carry no severity, and it filters locally so no line is lost by changing the
 * filter.
 */
export function LogViewerCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [follow, setFollow] = useState(false);
  const [query, setQuery] = useState('');
  const [source, setSource] = useState('');
  const [task, setTask] = useState('');
  const { lines, dropped, capacity, sources, loading, failed, reload } = useLogTail(follow);

  const shown = useMemo(() => filterLines(lines, query, source, task), [lines, query, source, task]);

  // Only while following, so an arriving line never interrupts reading.
  const box = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (!follow || !box.current) return;
    box.current.scrollTop = box.current.scrollHeight;
  }, [follow, shown]);

  // A callback ref, because the card returns early while loading and a plain
  // ref would still be null when the effect ran.
  const [picker, setPicker] = useState<HTMLSelectElement | null>(null);
  useEffect(() => enableSelectWheel(picker), [picker]);

  if (loading) return <LoadingCard label={t('common.loading')} />;
  if (failed) {
    return <ErrorCard message={t('settings.diagnostics.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.diagnostics.logHint', { n: capacity })}>
        {t('settings.diagnostics.logTitle')}
      </SectionTitle>

      <div className="flex flex-wrap items-center gap-3">
        <span className="flex min-w-[12rem] flex-1 items-center gap-1.5">
          <TextInput
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('settings.diagnostics.logSearch')}
            aria-label={t('settings.diagnostics.logSearch')}
          />
          <InfoBubble tip={t('settings.diagnostics.logSearchHint')} label={t('settings.diagnostics.logSearch')} />
        </span>

        <span className="flex items-center gap-1.5">
          <select
            ref={setPicker}
            value={source}
            onChange={(e) => setSource(e.target.value)}
            aria-label={t('settings.diagnostics.logSource')}
            className="glim-select h-9 appearance-none rounded-[var(--radius-control)] bg-carbon-surface2 px-3 pe-7
              text-sm text-carbon-textSub outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
          >
            <option value="">{t('settings.diagnostics.logSourceAll')}</option>
            {sources.map((s) => (
              // Untranslated, so it matches what the lines say.
              <option key={s} value={s} dir="ltr">
                {s}
              </option>
            ))}
            <option value={OTHER}>{t('settings.diagnostics.logSourceOther')}</option>
          </select>
          <InfoBubble tip={t('settings.diagnostics.logSourceHint')} label={t('settings.diagnostics.logSource')} />
        </span>
      </div>

      <ToggleRow
        hue={hue}
        label={t('settings.diagnostics.logFollow')}
        hint={t('settings.diagnostics.logFollowHint')}
        checked={follow}
        onChange={setFollow}
      />

      {task && (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-sm text-carbon-textSub">{t('settings.diagnostics.logTaskOnly')}</span>
          <span className="glim-num text-sm text-carbon-text" dir="ltr">
            {task}
          </span>
          <button
            type="button"
            onClick={() => setTask('')}
            className="rounded-[var(--radius-control)] px-2 py-1 text-[11px] text-carbon-textMuted
              transition-colors hover:bg-carbon-hover hover:text-carbon-text"
          >
            {t('settings.diagnostics.logTaskClear')}
          </button>
        </div>
      )}

      {dropped > 0 && (
        <span className="text-sm text-statusWarn">
          {t('settings.diagnostics.logGap', { n: dropped, cap: capacity })}
        </span>
      )}

      <span className="text-[11px] text-carbon-textMuted">
        {t('settings.diagnostics.logMatches', { shown: shown.length, total: lines.length })}
      </span>

      {lines.length === 0 ? (
        <span className="text-sm text-carbon-textMuted">{t('settings.diagnostics.logEmpty')}</span>
      ) : shown.length === 0 ? (
        <span className="text-sm text-carbon-textMuted">{t('settings.diagnostics.logNoMatches')}</span>
      ) : (
        // One block-level <span> per line, because a line may carry a button
        // and a <pre> may hold phrasing content but no <div>.
        <pre
          ref={box}
          dir="ltr"
          className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-[var(--radius-control)]
            bg-carbon-surface2 p-4 font-mono text-[11px] leading-relaxed text-carbon-textSub"
        >
          {shown.map((line) => (
            <Row key={line.seq} line={line} onTask={setTask} chipLabel={t('settings.diagnostics.logTaskChip')} />
          ))}
        </pre>
      )}
    </Card>
  );
}

/**
 * enableSelectWheel lets a closed <select> step one option per wheel notch,
 * clamped at both ends, and fires a bubbling `change` for its onChange. It uses
 * a native non-passive listener because React's onWheel is passive and cannot
 * prevent the page scroll. Resolvers.tsx has a copy.
 */
function enableSelectWheel(select: HTMLSelectElement | null): () => void {
  if (!select) return () => {};
  const onWheel = (event: WheelEvent) => {
    if (select.disabled || select.options.length < 2 || event.deltaY === 0) return;
    event.preventDefault();
    const delta = event.deltaY > 0 ? 1 : -1;
    const next = Math.min(select.options.length - 1, Math.max(0, select.selectedIndex + delta));
    if (next === select.selectedIndex) return;
    select.selectedIndex = next;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  };
  select.addEventListener('wheel', onWheel, { passive: false });
  return () => select.removeEventListener('wheel', onWheel);
}

/** The picker's value for lines without a source; '' already means all sources. */
const OTHER = 'other';

/** Row shows one line; the download it names becomes a filter chip. */
function Row({ line, onTask, chipLabel }: { line: LogLine; onTask: (id: string) => void; chipLabel: string }) {
  const id = line.taskId;
  const at = id ? line.line.indexOf(id) : -1;
  if (!id || at < 0) return <span className="block">{line.line}</span>;
  return (
    <span className="block">
      {line.line.slice(0, at)}
      <TaskChip id={id} label={chipLabel} onTask={onTask} />
      {line.line.slice(at + id.length)}
    </span>
  );
}

/** TaskChip is the download id in a line, a component of its own so only the
 *  lines that carry one pay for the tooltip. */
function TaskChip({ id, label, onTask }: { id: string; label: string; onTask: (id: string) => void }) {
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={label}
        {...tipHoverProps}
        onClick={() => onTask(id)}
        // hoverRaised, because hover moves up the ramp from surface3 and
        // plain hover sits below it.
        className="rounded-[var(--radius-control)] bg-carbon-surface3 px-1.5 text-carbon-text
          transition-colors hover:bg-carbon-hoverRaised"
      >
        {id}
      </button>
      {tip.node}
    </>
  );
}

/**
 * filterLines applies the three filters together. The text match is plain and
 * case-insensitive, so a pasted file name is never read as a pattern.
 */
function filterLines(lines: LogLine[], query: string, source: string, task: string): LogLine[] {
  const needle = query.trim().toLowerCase();
  return lines.filter((l) => {
    if (task && l.taskId !== task) return false;
    if (source === OTHER) {
      if (l.source) return false;
    } else if (source && l.source !== source) {
      return false;
    }
    if (needle && !l.line.toLowerCase().includes(needle)) return false;
    return true;
  });
}
