import { useEffect, useRef } from 'react';
import { fetchLogFileState, logFileHref, type LogFileState, type LogGeneration } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { fmtBytes, fmtDate } from '../../../lib/format';
import { useResource } from '../../../lib/useResource';
import {
  Card,
  ErrorCard,
  Field,
  FieldGroup,
  InfoBubble,
  LoadingCard,
  NumberInput,
  SectionTitle,
  ToggleRow,
} from '../../../components/ui';
import { useDraft } from '../context';

/**
 * Whether this instance keeps a copy of its own log on disk, and what is
 * already there.
 *
 * WHY IT IS OFF UNTIL SOMEBODY SWITCHES IT ON. An update must not start writing
 * files nobody asked for. On an Unraid box the app's data folder is usually on
 * the array, and an unbuffered write per log line is a spinning disk that never
 * gets to sleep - so this ships off, upgrades read as off, and the (i) says so
 * in the first sentence rather than leaving somebody to discover it.
 *
 * WHY THERE IS NO PATH BOX. The server has no path setting at all, on purpose:
 * the Advanced page reflects the whole settings document into free text boxes,
 * so a path typed there that does not exist, or that the account inside the
 * container may not write, would stop the log with nothing on screen connecting
 * the two. The folder is fixed beside the database and KL_LOG_DIR moves it,
 * which is a thing somebody sets once at the machine rather than a field that
 * can be wrong quietly.
 *
 * TWO KINDS OF STATE, DELIBERATELY SEPARATE, the same split the maintenance
 * card beside this one makes: the three settings are part of the shared draft
 * and the shell autosaves them, while the file's own state - where it is, how
 * big it has grown, what went wrong - is a resource read from the server. There
 * is no Save button here for the same reason there is none anywhere else in
 * settings.
 */
export function LogFileCard({ hue, capacity }: { hue: number; capacity: number }) {
  const { t } = useT();
  const { cfg, patch, dirty } = useDraft();
  const { data, failed, loading, reload } = useResource<LogFileState>(fetchLogFileState);

  // A settings document from a server that predates this key has no logFile at
  // all. Defaulted here rather than trusted, because every control below reads
  // it and a card that threw on an older server would take the whole page with
  // it.
  const file = cfg.logFile ?? { enabled: false, maxMb: 8, keep: 3 };

  // Re-read the file's state the moment the draft stops being dirty, which is
  // the shell telling us it has saved. A timer would be a guess at the autosave
  // debounce, and polling would keep a laptop warm on a settings tab nobody is
  // looking at - the argument the maintenance card makes for its own interval.
  const wasDirty = useRef(dirty);
  useEffect(() => {
    if (wasDirty.current && !dirty) reload();
    wasDirty.current = dirty;
  }, [dirty, reload]);

  if (loading) return <LoadingCard label={t('common.loading')} />;
  if (failed || !data) {
    return <ErrorCard message={t('settings.diagnostics.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.diagnostics.fileTitle')}</SectionTitle>

      <ToggleRow
        hue={hue}
        label={t('settings.diagnostics.fileOn')}
        hint={t('settings.diagnostics.fileOnHint')}
        checked={file.enabled}
        onChange={(enabled) => patch({ logFile: { ...file, enabled } })}
      />

      {/* Dimmed rather than hidden while the switch is off: a control that
          vanishes teaches nobody what the mode can do, and somebody deciding
          whether to switch this on wants to see the size it would use first. */}
      <div className={`grid grid-cols-1 gap-4 sm:grid-cols-3 ${file.enabled ? '' : 'pointer-events-none opacity-40'}`}>
        <Field label={t('settings.diagnostics.fileSize')} hint={t('settings.diagnostics.fileSizeHint')}>
          <NumberInput
            value={file.maxMb}
            min={1}
            max={1024}
            onValue={(maxMb) => patch({ logFile: { ...file, maxMb } })}
          />
        </Field>

        <Field label={t('settings.diagnostics.fileKeep')} hint={t('settings.diagnostics.fileKeepHint')}>
          <NumberInput value={file.keep} min={0} max={20} onValue={(keep) => patch({ logFile: { ...file, keep } })} />
        </Field>

        {/* FieldGroup and not Field: a Field is a <label>, and a label with no
            control in it names nothing. This is a reading. */}
        {/* The server answers this even while nothing is armed, which is
            exactly when it is wanted: somebody deciding whether to switch the
            log on is deciding which volume it lands on. */}
        <FieldGroup label={t('settings.diagnostics.fileWhere')} hint={t('settings.diagnostics.fileWhereHint')}>
          <span className="break-all text-sm text-carbon-text" dir="ltr">
            {data.path}
          </span>
        </FieldGroup>
      </div>

      <State state={data} />

      {data.problem ? (
        <Problem state={data} capacity={capacity} />
      ) : (
        <Generations generations={data.generations} />
      )}
    </Card>
  );
}

/** One line saying whether anything is being written, and how much of the cap
 *  is used. A fact rather than an explanation, so it is a line and not a
 *  bubble. */
function State({ state }: { state: LogFileState }) {
  const { t } = useT();
  if (!state.enabled) {
    return <span className="text-sm text-carbon-textMuted">{t('settings.diagnostics.fileStateOff')}</span>;
  }
  return (
    <span className="text-sm text-carbon-textSub">
      {t('settings.diagnostics.fileStateOn', { size: fmtBytes(state.bytes), max: fmtBytes(state.maxBytes) })}
    </span>
  );
}

/**
 * What to do when the log is not being written, laid out in the order somebody
 * at three in the morning needs it: what is wrong, then what to check, then
 * whether the volume is the reason.
 *
 * THE LAST SENTENCE OF THE ADVICE IS THE IMPORTANT ONE. "The log file stopped"
 * reads as "logging stopped" unless something says otherwise, so the hint ends
 * by saying the lines are still in memory and still go into the diagnostics
 * bundle. Nothing else stopped.
 *
 * THE FREE-SPACE LINE HAS THREE STATES AND NOT TWO. The server answers "I could
 * not measure it" separately from a figure, because a readout that drew nought
 * bytes free on a platform it cannot ask is a picture of a full disk somebody
 * would go hunting through.
 */
function Problem({ state, capacity }: { state: LogFileState; capacity: number }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-2">
      <span className="flex items-center gap-1.5 text-sm text-statusFail">
        {t('settings.diagnostics.fileProblem', { error: state.problem ?? '' })}
        {/* The line count comes from the page shell, which already has it out
            of the diagnostics bundle. Typing 500 here would be a second copy of
            a number the server owns, and the two would part company the day
            somebody changed the ring. */}
        <InfoBubble tip={t('settings.diagnostics.fileProblemHint', { path: state.path, n: capacity })} />
      </span>
      <span className="text-[11px] text-carbon-textMuted">
        {state.freeKnown
          ? t('settings.diagnostics.fileProblemSpace', { free: fmtBytes(state.freeBytes ?? 0) })
          : t('settings.diagnostics.fileProblemSpaceUnknown')}
      </span>
    </div>
  );
}

/**
 * The files on disk, newest first, each one downloadable.
 *
 * A plain link and not a fetch-and-save button: the route answers text/plain
 * with a Content-Disposition, so the browser does the whole job, and a file
 * that can be gigabytes has no business being read into a Blob in a tab first.
 */
function Generations({ generations }: { generations: LogGeneration[] }) {
  const { t } = useT();
  if (generations.length === 0) {
    return <span className="text-sm text-carbon-textMuted">{t('settings.diagnostics.fileEmpty')}</span>;
  }
  return (
    <div className="flex flex-col gap-2">
      {generations.map((g) => (
        <div key={g.index} className="flex flex-wrap items-center gap-3 text-sm">
          <span className="min-w-[9rem] text-carbon-textSub">
            {g.index === 0
              ? t('settings.diagnostics.fileCurrent')
              : t('settings.diagnostics.fileOlder', { n: g.index })}
          </span>
          <span className="glim-num text-carbon-text" dir="ltr">
            {fmtBytes(g.bytes)}
          </span>
          <span className="text-[11px] text-carbon-textMuted">{fmtDate(g.modifiedAt)}</span>
          <span className="flex-1" />
          <a
            href={logFileHref(g.index)}
            download
            className="rounded-[var(--radius-control)] px-2 py-1 text-[11px] text-carbon-textMuted
              transition-colors hover:bg-carbon-hover hover:text-carbon-text"
          >
            {t('settings.diagnostics.fileDownload')}
          </a>
        </div>
      ))}
    </div>
  );
}
