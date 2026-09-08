import { useCallback, useEffect, useState } from 'react';
import {
  type MaintenanceRun,
  type MaintenanceState,
  ApiError,
  fetchMaintenance,
  startMaintenance,
} from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { fmtBytes, fmtDate } from '../../../lib/format';
import { useResource } from '../../../lib/useResource';
import { Button, Card, ErrorCard, FieldGroup, InfoBubble, LoadingCard, Modal, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { useDraft } from '../context';

/**
 * The database's own state, and the three things that can be done about it.
 *
 * WHY IT IS ON THIS PAGE AND NOT A RAIL ENTRY OF ITS OWN. "How big is it" and
 * "do something about it" are the same question asked twice, and the sizes are
 * already in the diagnostics bundle the card above builds. Splitting them would
 * put the number on one page and the button on another.
 *
 * WHY IT POLLS. A compaction rewrites the whole file and holds the database's
 * one connection for as long as that takes; on a large store that outlives any
 * request, so the server answers 202 and this asks again every two seconds
 * until `running` clears. The interval only exists while something is running -
 * a page that polls a finished job for ever is a laptop that gets warm on a
 * settings tab nobody is looking at.
 *
 * TWO KINDS OF STATE, DELIBERATELY SEPARATE. The sizes and the last verdict are
 * a resource this page fetches; the two schedule fields are part of the
 * settings draft, so they go through patch() and the shell autosaves 600ms
 * later (Settings.tsx). There is no Save button here for the same reason there
 * is none anywhere else in settings.
 */

/** The intervals offered, in days. 0 is a real answer and reads as such. */
const INTERVALS = [
  { days: 0, key: 'settings.dbmaint.intervalNever' },
  { days: 30, key: 'settings.dbmaint.interval30' },
  { days: 90, key: 'settings.dbmaint.interval90' },
  { days: 180, key: 'settings.dbmaint.interval180' },
] as const;

/**
 * Whether a failed run failed because something ran out of room.
 *
 * It matters because the message names the wrong disk. SQLite writes a
 * compaction's full second copy to the TEMPORARY directory, which on a
 * container is the writable layer rather than the mounted data volume, and
 * "database or disk is full" sends an operator to look at a data volume with
 * terabytes free. Matched on the substrings SQLite itself produces rather than
 * on an error code, because the code does not survive the trip through JSON.
 */
function looksLikeNoRoom(error: string): boolean {
  const e = error.toLowerCase();
  return e.includes('disk is full') || e.includes('disk full') || e.includes('no space left');
}

/** Seconds, one decimal, from the milliseconds the server measured. */
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

  // The poll, and nothing else on this page owns a timer. It is created only
  // while something is running and cleared by the effect's own return, so a
  // card sitting idle costs nothing at all.
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
      // 409 is the one refusal with a sentence of its own: something else is
      // already running, which is a fact the page can state rather than an
      // error it has to apologise for. Everything else falls through to the
      // resource's own failure handling on the next poll.
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

        {/* FieldGroup and not Field for all three: a Field is a `<label>`, and
            a label with no control in it names nothing. These are readings.
            The size row carries the bundle note in its own bubble rather than
            as a sentence under the numbers - the two belong to the same
            thought, and a loose line here would be the one explanation on the
            page that is not in an (i). */}
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

          {/* "0 bytes" would be a claim about a file that is not there, and on
              a fresh install that is the normal state: the server reads
              settings.json and never writes it, so it appears the first time
              somebody saves a settings page. The bubble is only there in that
              state, because a file name that is present and has a size needs
              no explaining and an (i) that says nothing is furniture. */}
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
          {/* The only one behind a confirm, and the dialog names the file size:
              it rewrites the whole database and holds every other write until
              it is done. The other two read, or are over in a moment. */}
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

        {/* FieldGroup, not Field, for the same reason the readouts above use
            one: a Field hands a click on its caption to the first control
            inside it, which for a tab strip means clicking the words sets the
            first tab. A segmented control and not a number box, because a box
            invites "1", and a database that compacts itself daily on a live
            queue is the outcome nobody typed on purpose. */}
        <FieldGroup label={t('settings.dbmaint.interval')} hint={t('settings.dbmaint.intervalHint')}>
          <Tabs
            variant="well"
            label={t('settings.dbmaint.interval')}
            active={String(cfg.maintenanceIntervalDays ?? 0)}
            onSelect={(id) => patch({ maintenanceIntervalDays: Number(id) })}
            items={INTERVALS.map((i) => ({ id: String(i.days), label: t(i.key) }))}
          />
        </FieldGroup>

        {/* Dimmed rather than hidden when there is no schedule: a control that
            vanishes teaches nobody what the mode can do. A Toggle, never a
            checkbox. */}
        <ToggleRow
          hue={hue}
          label={t('settings.dbmaint.compactOnSchedule')}
          hint={t('settings.dbmaint.compactOnScheduleHint')}
          disabled={(cfg.maintenanceIntervalDays ?? 0) === 0}
          checked={cfg.maintenanceCompactOnSchedule ?? false}
          onChange={(maintenanceCompactOnSchedule) => patch({ maintenanceCompactOnSchedule })}
        />

        {/* A fact, not an explanation, so it is a line and not a bubble. */}
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
              <Button kind="ghost" onClick={() => setConfirming(false)}>
                {t('common.cancel')}
              </Button>
              <Button
                kind="danger"
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
 * One button with its own explanation beside it.
 *
 * The (i) is a sibling of the button rather than a title on it, because a
 * native tooltip does not open on focus, cannot be read by anybody using the
 * keyboard alone, and is the one place in this app where an explanation is
 * allowed to be invisible. Every other caption on the page carries its bubble
 * the same way.
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
    <span className="inline-flex items-center gap-1.5">
      <Button kind="secondary" hue={hue} disabled={disabled} onClick={onClick}>
        {pending ? pendingLabel : label}
      </Button>
      <InfoBubble tip={hint} label={label} />
    </span>
  );
}

/**
 * What the last pass found.
 *
 * NOTHING HAVING RUN IS NOT A CLEAN BILL OF HEALTH, which is why the first
 * branch is a sentence and not a green tick: the server sends null for "never
 * run here" precisely so this can tell it apart from "ran and found nothing".
 *
 * A FAILED CHECK IS THE THREE-IN-THE-MORNING ANSWER and is laid out in that
 * order: what is wrong, where the file is, what to do first. The help text
 * deliberately does not say "just take a backup" - the download button under
 * Backup and restore is VACUUM INTO, which reads every page and will very
 * likely fail on the same damage the check just found.
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
        {/* ltr regardless of interface direction, the same convention the log
            block above this card and every other path cell in settings/ uses:
            these lines are page numbers and tree names, none of which reads
            correctly mirrored. */}
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
