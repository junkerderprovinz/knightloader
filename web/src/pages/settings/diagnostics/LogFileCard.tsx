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
 * LogFileCard switches the on-disk copy of the log and lists what is there. It
 * ships off so an update does not keep an array disk awake. The folder sits
 * beside the database and only KL_LOG_DIR moves it, so there is no path box.
 * The settings belong to the shared draft; the file's state is read from the
 * server.
 */
export function LogFileCard({ hue, capacity }: { hue: number; capacity: number }) {
  const { t } = useT();
  const { cfg, patch, dirty } = useDraft();
  const { data, failed, loading, reload } = useResource<LogFileState>(fetchLogFileState);

  // An older server sends no logFile.
  const file = cfg.logFile ?? { enabled: false, maxMb: 8, keep: 3 };

  // The draft turning clean means the shell has saved, so the state is re-read then.
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

      {/* The size and rotation boxes are absent while the switch is off. The
          path stays, since it tells where the log would land. */}
      <div className={`grid grid-cols-1 gap-4 ${file.enabled ? 'sm:grid-cols-3' : ''}`}>
        {file.enabled && (
          <>
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
          </>
        )}

        {/* FieldGroup, because a label with no control in it names nothing. */}
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

/** State says whether anything is being written and how much of the cap is used. */
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
 * Problem says why the log is not being written, what to check and whether the
 * volume is the reason. Free space has a third state for "could not measure",
 * so an unknown figure never reads as a full disk.
 */
function Problem({ state, capacity }: { state: LogFileState; capacity: number }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-2">
      <span className="flex items-center gap-1.5 text-sm text-statusFail">
        {t('settings.diagnostics.fileProblem', { error: state.problem ?? '' })}
        {/* The ring size comes from the diagnostics bundle, not a copy here. */}
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
 * Generations lists the files on disk, newest first. Each is a plain link,
 * since the route sends a Content-Disposition and a large file should not pass
 * through a Blob.
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
          <span className="glim-num text-carbon-text">
            {fmtBytes(g.bytes)}
          </span>
          <span className="text-[11px] text-carbon-textMuted">{fmtDate(g.modifiedAt)}</span>
          <span className="flex-1" />
          {/* An anchor for its download attribute, drawn as a badge. */}
          <a
            href={logFileHref(g.index)}
            download
            className="inline-flex h-[var(--btn-h)] shrink-0 items-center rounded-[var(--radius-pill)] bg-carbon-surface2
              px-2.5 text-xs font-medium text-carbon-textSub transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
          >
            {t('settings.diagnostics.fileDownload')}
          </a>
        </div>
      ))}
    </div>
  );
}
