import { useEffect, useMemo, useRef, useState } from 'react';
import type { LogLine } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { Card, ErrorCard, InfoBubble, LoadingCard, SectionTitle, TextInput, ToggleRow } from '../../../components/ui';
import { useLogTail } from './useLogTail';

/**
 * The log itself: the lines, a box to search them, a picker for which part of
 * the app they came from, and a switch that keeps the view at the newest one.
 *
 * WHY A SOURCE PICKER AND NOT A LEVEL PICKER, since that is the first thing
 * anybody expects here. There are no levels in this application to pick from:
 * every one of its log calls goes to the standard logger with no severity
 * attached, so a menu offering Info, Warn and Error would have to guess one out
 * of the wording of each line. A guess dressed as a level is worse than no
 * filter, because it looks authoritative while hiding lines from whoever
 * trusted it. What the lines DO carry is the name of the part of the app that
 * wrote them, at the front, and that is what this filters on. The list comes
 * from the server for the same reason the source of each line does: one copy of
 * the rule, on the side that owns the lines.
 *
 * FILTERING IS LOCAL AND THE SERVER IS NOT ASKED AGAIN. Every line the view
 * holds is already here, so narrowing them is instant and costs no request -
 * and, more importantly, switching the filter back does not have to re-fetch
 * anything, so a filter is never a way to lose lines you had.
 *
 * THE COUNT UNDER THE BOX IS NOT DECORATION. "6 of 431 lines" is the difference
 * between a search that found little and a search that found nothing, and
 * between a filter that is narrowing and one that is switched off.
 */
export function LogViewerCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [follow, setFollow] = useState(false);
  const [query, setQuery] = useState('');
  const [source, setSource] = useState('');
  const [task, setTask] = useState('');
  const { lines, dropped, capacity, sources, loading, failed, reload } = useLogTail(follow);

  const shown = useMemo(() => filterLines(lines, query, source, task), [lines, query, source, task]);

  // Jumping to the newest line is part of what "follow" means; without it the
  // switch would fetch lines nobody can see. Only while following, so reading
  // something with the switch off is never interrupted by an arriving line.
  const box = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (!follow || !box.current) return;
    box.current.scrollTop = box.current.scrollHeight;
  }, [follow, shown]);

  // The source picker answers the mouse wheel while it is closed - see
  // enableSelectWheel below. A callback ref rather than useRef, because the
  // card returns early while the tail is loading and a useRef would be null
  // the one time an effect with an empty dependency list ever ran.
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

        {/* A native select, on the SearchField argument: a fixed handful of
            values that has to be reachable by keyboard and by screen reader,
            and which would take more width than the lines it narrows if it
            were a row of segments. */}
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
              // The server's own word, untranslated, exactly as the rules chip
              // in the task panel prints the engine's: it is the handle for the
              // thing, and a translated handle does not match what the lines
              // themselves say.
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

      {/* A gap is a fact about the lines below, so it sits with them rather
          than in a bubble: it changes what the view MEANS, and an explanation
          somebody has to hover to find would be one they read afterwards. */}
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
        // ltr regardless of interface direction, the same convention every
        // other path/URL/code cell in settings/ uses: log lines mix paths,
        // hosts and stack traces, none of which read correctly mirrored.
        //
        // Still a <pre>, with one block-level <span> per line rather than one
        // joined string, because a line that names a download carries a button.
        // A <button> is phrasing content and may live in a <pre>; a <div> may
        // not, which is why the rows are spans.
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
 * Rule 14's mouse-wheel addendum (GlimStone 1.8.0): a CLOSED <select> answers
 * the wheel as well, stepping one option per notch, so a menu somebody reaches
 * for constantly does not cost a click first. Clamped at both ends rather than
 * wrapping - one notch too many must not land a value from the other end of the
 * list.
 *
 * A real listener with `{ passive: false }`, never React's `onWheel`, and that
 * detail is load-bearing rather than fussy: React registers `onWheel` as a
 * PASSIVE listener on its root, so `preventDefault` inside such a handler does
 * nothing but log a warning and the page scrolls out from under the pointer
 * while the value changes. QueueBar.tsx attaches the speed field's wheel
 * handler natively for exactly this reason.
 *
 * The dispatched event is a real bubbling `change`, so the `onChange` already
 * on the element picks it up exactly as a click on an <option> would, with no
 * second code path to keep in step.
 *
 * SECOND COPY, DELIBERATELY: settings/Resolvers.tsx carries this function
 * verbatim for its own preset selects. Both are standing in for
 * web/src/lib/selectScroll.ts - GlimStone's reference/selectScroll.ts under
 * this repo's roof - which does not exist yet; the moment it does, these two
 * collapse into one import and the other thirteen <select> call sites in the
 * tree get the behaviour with them.
 */
function enableSelectWheel(select: HTMLSelectElement | null): () => void {
  if (!select) return () => {};
  const onWheel = (event: WheelEvent) => {
    if (select.disabled || select.options.length < 2 || event.deltaY === 0) return;
    // This handler IS the scroll while the pointer sits on the control, rather
    // than a bystander to it.
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

/**
 * The value the picker uses for "everything else".
 *
 * A sentinel and not the empty string, because the empty string is already the
 * server's own answer for a line that names no part of the app - and it is also
 * what "all sources" has to be. Three states, three values.
 */
const OTHER = 'other';

/** One line, with the download it names turned into a way to filter by it. */
function Row({ line, onTask, chipLabel }: { line: LogLine; onTask: (id: string) => void; chipLabel: string }) {
  const id = line.taskId;
  const at = id ? line.line.indexOf(id) : -1;
  if (!id || at < 0) return <span className="block">{line.line}</span>;
  return (
    <span className="block">
      {line.line.slice(0, at)}
      <button
        type="button"
        title={chipLabel}
        aria-label={chipLabel}
        onClick={() => onTask(id)}
        // hoverRaised and not hover: the chip is ALREADY filled with surface3,
        // and GlimStone rule 21 says hover moves UP the surface ramp from
        // whatever an element sits on. --carbon-hover is #353535 on the dark
        // ramp, 29 units BELOW surface3's #525252, so the chip would darken at
        // the one moment somebody is looking straight at it - and on the light
        // ramp it would go from #d1d1d1 back towards the surface instead of
        // away from it. Wrong in every colour mode, in opposite directions.
        className="rounded-[var(--radius-control)] bg-carbon-surface3 px-1.5 text-carbon-text
          transition-colors hover:bg-carbon-hoverRaised"
      >
        {id}
      </button>
      {line.line.slice(at + id.length)}
    </span>
  );
}

/**
 * The three filters, applied together.
 *
 * The text match is plain and case-insensitive, with no wildcards and no
 * regular expressions: a log line is full of dots, brackets, slashes and
 * question marks, so a box that quietly treated them as syntax would turn a
 * pasted file name into a pattern that matches nothing, and there would be
 * nothing on screen to say why.
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
