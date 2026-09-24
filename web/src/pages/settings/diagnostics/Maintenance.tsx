import { useCallback, useEffect, useState } from 'react';
import {
  type MaintenanceRun,
  type MaintenanceState,
  ApiError,
  fetchMaintenance,
  startMaintenance,
} from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { IconClose } from '../../../lib/icons';
import { fmtBytes, fmtDate } from '../../../lib/format';
import { useResource } from '../../../lib/useResource';
import { Button, Card, ErrorCard, FieldGroup, InfoBubble, LoadingCard, Modal, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { useDraft } from '../context';

// The database's state and the three things that can be done about it. A
// compaction can outlive any request, so the server answers 202 and the card
// polls every two seconds while a job runs. The sizes and the last verdict are
// fetched; the two schedule fields are part of the settings draft.

/** The intervals offered, in days; 0 means never. */
const INTERVALS = [
  { days: 0, key: 'settings.dbmaint.intervalNever' },
  { days: 30, key: 'settings.dbmaint.interval30' },
  { days: 90, key: 'settings.dbmaint.interval90' },
  { days: 180, key: 'settings.dbmaint.interval180' },
] as const;

/**
 * looksLikeNoRoom tells whether a run failed for lack of room. SQLite writes a
 * compaction's copy to the temporary directory, which in a container is not the
 * data volume, so its message points at the wrong disk. It matches SQLite's
 * wording because the error code does not survive JSON.
 */
function looksLikeNoRoom(error: string): boolean {
  const e = error.toLowerCase();
  return e.includes('disk is full') || e.includes('disk full') || e.includes('no space left');
}

function secondsOf(run: MaintenanceRun): string {
  return (run.durationMs / 1000).toFixed(1);
}

export function MaintenanceCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { data, failed, loading, reload, setData } = useResource<MaintenanceState>(fetchMaintenance);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [starting, setStarting] = useState('');

  const running = data?.running ?? '';

  const refresh = useCallback(() => {
    void fetchMaintenance().then(setData, () => undefined);
  }, [setData]);
  useEffect(() => {
    if (!running) return;
    const id = window.setInterval(refresh, 2000);
    return () => window.clearInterval(id);
  }, [running, refresh]);

  async function start(action: 'check' | 'compact' | 'analyze') {
    setBusy(false);
    setStarting(action);
    try {
      setData(await startMaintenance(action));
    } catch (e) {
      // 409 means another job is running; other errors show on the next poll.
      if (e instanceof ApiError && e.status === 409) setBusy(true);
      refresh();
    } finally {
      setStarting('');
    }
  }

  if (loading) return <LoadingCard label={t('common.loading')} />;
  if (failed || !data) {
    return <ErrorCard message={t('settings.dbmaint.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  const { storage, last } = data;
  const acting = running !== '' || starting !== '';

  return (
    <>
      <Card hue={hue} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.dbmaint.hint')}>{t('settings.dbmaint.title')}</SectionTitle>

        {/* FieldGroup, because a label with no control in it names nothing. */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <FieldGroup
            label={t('settings.dbmaint.storeSize')}
            hint={`${t('settings.dbmaint.storeSizeHint')} ${t('settings.dbmaint.inBundle')}`}
          >
            <span className="glim-num text-sm text-carbon-text" dir="ltr">
              {fmtBytes(storage.storeBytes)}
            </span>
          </FieldGroup>

          <FieldGroup label={t('settings.dbmaint.reclaimable')} hint={t('settings.dbmaint.reclaimableHint')}>
            <span className="glim-num text-sm text-carbon-text" dir="ltr">
              {fmtBytes(storage.storeReclaimableBytes)}
            </span>
          </FieldGroup>

          {/* settings.json appears only on the first save, so a fresh install
              has none; the bubble explains that state only. */}
          <FieldGroup
            label={t('settings.dbmaint.settingsSize')}
            hint={storage.settingsPresent ? undefined : t('settings.dbmaint.settingsMissingHint')}
          >
            <span className="glim-num text-sm text-carbon-text" dir="ltr">
              {storage.settingsPresent ? fmtBytes(storage.settingsBytes) : t('settings.dbmaint.settingsMissing')}
            </span>
          </FieldGroup>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <Action
            label={t('settings.dbmaint.check')}
            hint={t('settings.dbmaint.checkHint')}
            hue={hue}
            disabled={acting}
            pending={running === 'check' || starting === 'check'}
            pendingLabel={t('settings.dbmaint.running')}
            onClick={() => void start('check')}
          />
          {/* Confirmed first, since it rewrites the database and blocks writes. */}
          <Action
            label={t('settings.dbmaint.compact')}
            hint={t('settings.dbmaint.compactHint')}
            hue={hue}
            disabled={acting}
            pending={running === 'compact' || starting === 'compact'}
            pendingLabel={t('settings.dbmaint.running')}
            onClick={() => setConfirming(true)}
          />
          <Action
            label={t('settings.dbmaint.analyze')}
            hint={t('settings.dbmaint.analyzeHint')}
            hue={hue}
            disabled={acting}
            pending={running === 'analyze' || starting === 'analyze'}
            pendingLabel={t('settings.dbmaint.running')}
            onClick={() => void start('analyze')}
          />
        </div>

        {busy && <span className="text-sm text-statusWarn">{t('settings.dbmaint.busy')}</span>}

        <Verdict last={last} storePath={storage.storePath} tempDir={storage.tempDir} tempFreeBytes={storage.tempFreeBytes} />

        {/* A fixed set of intervals rather than a number box, which would
            invite a daily compaction on a live queue. */}
        <FieldGroup label={t('settings.dbmaint.interval')} hint={t('settings.dbmaint.intervalHint')}>
          <Tabs
            variant="well"
            label={t('settings.dbmaint.interval')}
            active={String(cfg.maintenanceIntervalDays ?? 0)}
            onSelect={(id) => patch({ maintenanceIntervalDays: Number(id) })}
            items={INTERVALS.map((i) => ({ id: String(i.days), label: t(i.key) }))}
          />
        </FieldGroup>

        {/* Absent while nothing is scheduled. */}
        {(cfg.maintenanceIntervalDays ?? 0) > 0 && (
        <ToggleRow
          hue={hue}
          label={t('settings.dbmaint.compactOnSchedule')}
          hint={t('settings.dbmaint.compactOnScheduleHint')}
          checked={cfg.maintenanceCompactOnSchedule ?? false}
          onChange={(maintenanceCompactOnSchedule) => patch({ maintenanceCompactOnSchedule })}
        />
        )}

        {data.nextRunAt && (
          <span className="text-sm text-carbon-textSub">{t('settings.dbmaint.nextRun', { when: fmtDate(data.nextRunAt) })}</span>
        )}
      </Card>

      {confirming && (
        <Modal
          title={t('settings.dbmaint.confirmTitle')}
          onClose={() => setConfirming(false)}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                onClick={() => setConfirming(false)}
              />
              <Button
                kind="ghost"
                onClick={() => {
                  setConfirming(false);
                  void start('compact');
                }}
              >
                {t('settings.dbmaint.compact')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {t('settings.dbmaint.confirmBody', { size: fmtBytes(storage.storeBytes) })}
          </p>
        </Modal>
      )}
    </>
  );
}

/**
 * Action is a maintenance button, its explanation in the (i) inside it rather
 * than in a native title, which the keyboard cannot open.
 */
function Action({
  label,
  hint,
  hue,
  disabled,
  pending,
  pendingLabel,
  onClick,
}: {
  label: string;
  hint: string;
  hue: number;
  disabled: boolean;
  pending: boolean;
  pendingLabel: string;
  onClick: () => void;
}) {
  return (
    <Button kind="secondary" hue={hue} disabled={disabled} hint={hint} onClick={onClick}>
      {pending ? pendingLabel : label}
    </Button>
  );
}

/**
 * Verdict shows what the last pass found. Null means it never ran, which is not
 * a clean result. The advice after a failed check does not suggest a backup,
 * because the backup's VACUUM INTO would likely hit the same damage.
 */
function Verdict({
  last,
  storePath,
  tempDir,
  tempFreeBytes,
}: {
  last: MaintenanceRun | null;
  storePath: string;
  tempDir: string;
  tempFreeBytes: number;
}) {
  const { t } = useT();

  if (!last) return <span className="text-sm text-carbon-textMuted">{t('settings.dbmaint.neverRun')}</span>;

  if (last.skipped) {
    return <span className="text-sm text-carbon-textSub">{t('settings.dbmaint.skippedBusy')}</span>;
  }

  const when = t('settings.dbmaint.lastRun', { when: fmtDate(last.at), seconds: secondsOf(last) });

  if (last.error) {
    return (
      <div className="flex flex-col gap-2">
        <span className="flex items-center gap-1.5 text-sm text-statusFail">
          {t('settings.dbmaint.failed', { error: last.error })}
          {looksLikeNoRoom(last.error) && (
            <InfoBubble
              label={t('settings.dbmaint.compact')}
              tip={t('settings.dbmaint.diskFullHelp', { tmp: tempDir, free: fmtBytes(tempFreeBytes) })}
            />
          )}
        </span>
        <span className="text-[11px] text-carbon-textMuted">{when}</span>
      </div>
    );
  }

  if (last.kind === 'check' && last.problems.length > 0) {
    return (
      <div className="flex flex-col gap-2">
        <span className="flex items-center gap-1.5 text-sm text-statusFail">
          {t('settings.dbmaint.checkFailed', { n: last.problems.length })}
          <InfoBubble label={t('settings.dbmaint.check')} tip={t('settings.dbmaint.checkFailedHelp', { path: storePath })} />
        </span>
        <pre
          dir="ltr"
          className="max-h-96 overflow-auto whitespace-pre-wrap break-all p-4 font-mono text-[11px] leading-relaxed text-carbon-textSub"
        >
          {last.problems.join('\n')}
        </pre>
        <span className="text-[11px] text-carbon-textMuted">{when}</span>
      </div>
    );
  }

  let line = t('settings.dbmaint.checkOk');
  if (last.kind === 'compact') {
    const freed = last.bytesBefore - last.bytesAfter;
    line =
      freed > 0
        ? t('settings.dbmaint.compacted', {
            before: fmtBytes(last.bytesBefore),
            after: fmtBytes(last.bytesAfter),
            freed: fmtBytes(freed),
          })
        : t('settings.dbmaint.compactedNothing', { before: fmtBytes(last.bytesBefore) });
  } else if (last.kind === 'analyze') {
    line = t('settings.dbmaint.analyzed');
  }

  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm text-statusOk">{line}</span>
      <span className="text-[11px] text-carbon-textMuted">{when}</span>
    </div>
  );
}
