import { FieldGroup } from '../../../components/ui';
import { idleProblemText } from '../../../components/IdleActionBanner';
import { happened } from '../../../lib/countdown';
import { fmtDate, fmtUnit } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import type { EventProgramStatus } from '../../../lib/eventprograms';
import { useTriggerLabel } from '../../../lib/triggers';

/**
 * ProgramHealth shows what the server knows about a stored program: whether
 * the program resolves, which it answers without running anything, and how the
 * last run since the server started ended. The problem codes are the ones the
 * end-of-queue command reports, so they read the same here as there.
 */
export function ProgramHealth({
  status,
  program,
  deployment,
  timeoutSeconds,
}: {
  status?: EventProgramStatus;
  /** What to call the program in a sentence; the path is masked once stored. */
  program: string;
  deployment: string;
  timeoutSeconds: number;
}) {
  const { t } = useT();
  const triggerLabel = useTriggerLabel();
  // The status narrowed to one with a run behind it.
  const last = status !== undefined && happened(status.lastStart) ? status : undefined;

  return (
    <FieldGroup label={t('settings.eventPrograms.status')} hint={t('settings.eventPrograms.statusHint')}>
      <div className="flex flex-col gap-1.5 text-xs">
        {status?.check ? (
          <span className="text-statusWarn">{idleProblemText(t, status.check, { program, deployment })}</span>
        ) : (
          status && <span className="text-carbon-textMuted">{t('settings.eventPrograms.checkOk')}</span>
        )}
        {!last ? (
          <span className="text-carbon-textSub">{t('settings.eventPrograms.statusUnknown')}</span>
        ) : (
          <>
            <span className="text-carbon-textMuted">
              {t('settings.eventPrograms.lastRun', {
                when: fmtDate(last.lastStart),
                event: triggerLabel(last.lastEvent ?? ''),
              })}
            </span>
            {last.lastProblem ? (
              <span className="text-statusWarn">
                {idleProblemText(t, last.lastProblem, {
                  program,
                  deployment,
                  code: last.lastExitCode,
                  output: last.lastOutput,
                  seconds: timeoutSeconds,
                })}
              </span>
            ) : (
              <>
                <span className="text-carbon-textMuted">
                  {t('settings.eventPrograms.lastRunOk', { duration: fmtDuration(last.lastDurationMs) })}
                </span>
                {last.lastOutput && (
                  <span dir="ltr" className="whitespace-pre-wrap break-words text-carbon-textMuted">
                    {t('settings.eventPrograms.output', { output: last.lastOutput })}
                  </span>
                )}
              </>
            )}
            <span className="text-carbon-textMuted">
              {last.runs === 1
                ? t('settings.eventPrograms.runCountOne')
                : t('settings.eventPrograms.runCount', { n: last.runs, failed: last.failed })}
            </span>
          </>
        )}
        {status !== undefined && status.dropped > 0 && (
          <span className="text-statusWarn">
            {status.dropped === 1
              ? t('settings.eventPrograms.droppedOne')
              : t('settings.eventPrograms.dropped', { n: status.dropped })}
          </span>
        )}
      </div>
    </FieldGroup>
  );
}

/** fmtDuration prints a run's length in milliseconds under a second and in
 *  seconds with one decimal above. */
function fmtDuration(ms: number): string {
  return ms < 1000 ? fmtUnit(ms, 'ms') : fmtUnit(Math.round(ms / 100) / 10, 's');
}
