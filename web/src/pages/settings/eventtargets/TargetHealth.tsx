import { FieldGroup } from '../../../components/ui';
import { happened } from '../../../lib/countdown';
import { fmtDate } from '../../../lib/format';
import { useT, type TranslationKey } from '../../../lib/i18n';
import type { EventTargetStatus } from '../../../lib/eventtargets';

/**
 * TargetHealth shows what the server knows about a stored target. The health
 * lives in memory, so no lastAttempt means nothing was sent since the server
 * started, and the counters are withheld rather than shown as zeroes. A refusal
 * by the far end is shown apart from a transport error.
 */
export function TargetHealth({ status }: { status?: EventTargetStatus }) {
  const { t } = useT();
  const known = status !== undefined && happened(status.lastAttempt);

  return (
    <FieldGroup label={t('settings.eventTargets.status')} hint={t('settings.eventTargets.droppedHint')}>
      <div className="flex flex-col gap-1.5 text-xs">
        {!known ? (
          <span className="text-carbon-textSub">{t('settings.eventTargets.statusUnknown')}</span>
        ) : (
          <>
            <span className="text-carbon-textMuted">
              {t('settings.eventTargets.lastAttempt')}: {fmtWhen(status.lastAttempt)}
            </span>
            <span className="text-carbon-textMuted">
              {happened(status.lastOk)
                ? `${t('settings.eventTargets.lastOk')}: ${fmtWhen(status.lastOk)}`
                : t('settings.eventTargets.lastOkNever')}
            </span>
            <span className="text-carbon-textMuted">{t('settings.eventTargets.sentCount', { n: status.sent })}</span>
            {status.dropped > 0 && (
              <span className="text-statusWarn">{t('settings.eventTargets.dropped', { n: status.dropped })}</span>
            )}
            {/* The translated reason first, then the server's own words. */}
            {status.lastCode && <span className="text-statusWarn">{problemText(t, status.lastCode)}</span>}
            {status.lastError && <span className="text-carbon-textMuted">{status.lastError}</span>}
          </>
        )}
      </div>
    </FieldGroup>
  );
}

/**
 * problemText returns the sentence for a notify.Problem code, or the code
 * itself when this build has no key for it, since `t()` returns undefined then.
 */
function problemText(t: (key: TranslationKey) => string, code: string): string {
  const key = `settings.eventTargets.problem.${code}` as TranslationKey;
  const text = t(key) as string | undefined;
  return text && text !== key ? text : code;
}

/**
 * fmtWhen formats an RFC3339 stamp in the reader's locale, or returns it raw
 * when it does not parse. It is absolute because the value is fetched once.
 */
function fmtWhen(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : fmtDate(iso);
}
