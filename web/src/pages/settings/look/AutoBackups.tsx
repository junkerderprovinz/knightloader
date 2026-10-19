import { useEffect, useRef } from 'react';
import { autoBackupHref, fetchAutoBackups, type AutoBackupState } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { fmtBytes, fmtDate } from '../../../lib/format';
import { useResource } from '../../../lib/useResource';
import { ErrorCard, FieldGroup, LoadingCard, NumberInput } from '../../../components/ui';
import { useDraft } from '../context';
import { SettingRow, SubCard } from '../controls';

/**
 * AutoBackupSection schedules the same archive the download button makes and
 * lists what the schedule has written. The folder sits in the data folder and
 * has no path box. The two numbers belong to the shared draft; the files and
 * the last run are read from the server.
 */
export function AutoBackupSection({ locked }: { locked: boolean }) {
  const { t } = useT();
  const { cfg, patch, dirty } = useDraft();
  const { data, failed, loading, reload, setData } = useResource<AutoBackupState>(fetchAutoBackups);

  const interval = cfg.backupIntervalDays ?? 0;

  // A save can switch the schedule on, and the first backup follows within a minute.
  const wasDirty = useRef(dirty);
  useEffect(() => {
    if (wasDirty.current && !dirty) reload();
    wasDirty.current = dirty;
  }, [dirty, reload]);

  // Polled while a run is going or due, so the new file shows without a reload.
  const due = !!data && (data.running || (data.nextRunAt !== null && Date.parse(data.nextRunAt) <= Date.now()));
  useEffect(() => {
    if (!due) return;
    const id = window.setInterval(() => {
      void fetchAutoBackups().then(setData, () => undefined);
    }, 15000);
    return () => window.clearInterval(id);
  }, [due, setData]);

  if (loading) return <LoadingCard nested label={t('common.loading')} />;
  if (failed || !data) {
    return (
      <ErrorCard nested message={t('settings.autoBackup.loadFailed')} retry={reload} retryLabel={t('common.retry')} />
    );
  }

  const newest = data.files[0];
  return (
    <SubCard label={t('settings.autoBackup.title')} hint={t('settings.autoBackup.titleHint')} className="gap-4">
      <SettingRow control="short" label={t('settings.autoBackup.interval')} hint={t('settings.autoBackup.intervalHint')}>
        <NumberInput
          value={interval}
          min={0}
          max={365}
          onValue={(v) => patch({ backupIntervalDays: Math.max(0, v) })}
        />
      </SettingRow>

      {/* Absent while nothing is scheduled. */}
      {interval > 0 && (
        <SettingRow control="short" label={t('settings.autoBackup.keep')} hint={t('settings.autoBackup.keepHint')}>
          <NumberInput
            value={cfg.backupKeep ?? 7}
            min={1}
            max={1000}
            onValue={(v) => patch({ backupKeep: Math.max(1, v) })}
          />
        </SettingRow>
      )}

      {/* FieldGroup, because a label with no control in it names nothing. */}
      <FieldGroup label={t('settings.autoBackup.where')} hint={t('settings.autoBackup.whereHint')}>
        <span className="break-all text-sm text-carbon-text" dir="ltr">
          {data.dir}
        </span>
      </FieldGroup>

      <div className="flex flex-col gap-1">
        <span className="text-sm text-carbon-textSub">
          {newest ? t('settings.autoBackup.last', { when: fmtDate(newest.createdAt) }) : t('settings.autoBackup.none')}
        </span>
        <LastRun state={data} />
        {data.nextRunAt && !data.running && (
          <span className="text-meta text-carbon-textMuted">
            {t('settings.autoBackup.next', { when: fmtDate(data.nextRunAt) })}
          </span>
        )}
      </div>

      {data.files.length > 0 && (
        <div className="flex flex-col gap-2">
          {data.files.map((f) => (
            <div key={f.name} className="flex flex-wrap items-center gap-3 text-sm">
              <span className="min-w-0 break-all text-carbon-textSub" dir="ltr">
                {f.name}
              </span>
              <span className="glim-num text-carbon-text">{fmtBytes(f.bytes)}</span>
              <span className="flex-1" />
              {/* An anchor for its download attribute, drawn as a badge. A backup
                  carries every stored password, so it waits for a login password. */}
              {!locked && (
                <a
                  href={autoBackupHref(f.name)}
                  download
                  className="inline-flex h-[var(--btn-h)] shrink-0 items-center rounded-[var(--radius-pill)] bg-carbon-surface2
                    px-2.5 text-xs font-medium text-carbon-textSub transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
                >
                  {t('settings.diagnostics.fileDownload')}
                </a>
              )}
            </div>
          ))}
        </div>
      )}
    </SubCard>
  );
}

/** LastRun says what is going on now, or why the last run wrote nothing. */
function LastRun({ state }: { state: AutoBackupState }) {
  const { t } = useT();
  const last = state.last;
  if (state.running) {
    return <span className="text-sm text-carbon-textSub">{t('settings.autoBackup.running')}</span>;
  }
  if (last?.error) {
    return (
      <span className="text-sm text-statusFail">
        {t('settings.autoBackup.failed', { when: fmtDate(last.at), error: last.error })}
      </span>
    );
  }
  if (last?.skipped) {
    return (
      <span className="text-sm text-statusWarn">
        {t('settings.autoBackup.lowSpace', {
          free: fmtBytes(last.freeBytes ?? 0),
          floor: fmtBytes(last.floorBytes ?? 0),
        })}
      </span>
    );
  }
  return null;
}
