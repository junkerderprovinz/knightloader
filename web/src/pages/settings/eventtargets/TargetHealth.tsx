import { FieldGroup } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import type { EventTargetStatus } from '../../../lib/eventtargets';

/**
 * What the server knows about this target right now.
 *
 * ONLY EVER DRAWN FOR A STORED ROW. A target that has never been saved has no
 * worker and no health, so every line here would read as a fault on something
 * that does not exist yet.
 *
 * THE ABSENT-STATUS CASE IS THE ONE THAT MATTERS, and it is deliberately quiet.
 * The health table lives in memory beside the dispatcher and the targets
 * themselves live in settings.json, so a target with no lastAttempt has sent
 * nothing SINCE THE SERVER STARTED, which is a different statement from "never".
 * A target that has been delivering for a year reads as silent for the seconds
 * after a container update, and drawing that as a problem would be a false alarm
 * on every boot. So the counters are withheld in that state rather than printed
 * as zeroes.
 *
 * A REFUSAL IS NOT AN ERROR AND IS DRAWN SEPARATELY. lastError is empty whenever
 * the far end answered at all, whatever it answered; the answer is in lastStatus
 * and lastCode. Folding the two together would report a wrong token as a network
 * problem the target will get over, and send somebody to look at their firewall.
 */
export function TargetHealth({ status }: { status?: EventTargetStatus }) {
  const { t } = useT();
  const known = status !== undefined && status.lastAttempt !== undefined;

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
              {status.lastOk
                ? `${t('settings.eventTargets.lastOk')}: ${fmtWhen(status.lastOk)}`
                : t('settings.eventTargets.lastOkNever')}
            </span>
            <span className="text-carbon-textMuted">{t('settings.eventTargets.sentCount', { n: status.sent })}</span>
            {/* Only when it has happened. A permanent "0 dropped" line is a line
                everybody stops reading, and this number only means anything the
                moment it is not zero. */}
            {status.dropped > 0 && (
              <span className="text-statusWarn">{t('settings.eventTargets.dropped', { n: status.dropped })}</span>
            )}
            {/* The typed reason first, because it is the one in the reader's own
                language and the one that says what to try. The server's own
                sentence follows it where there is one, since a transport failure
                nothing recognised is still better read than guessed at. */}
            {status.lastCode && <span className="text-statusWarn">{problemText(t, status.lastCode)}</span>}
            {status.lastError && <span className="text-carbon-textMuted">{status.lastError}</span>}
          </>
        )}
      </div>
    </FieldGroup>
  );
}

/**
 * The sentence for a notify.Problem code, or the code itself.
 *
 * The fallback is the whole reason this is a function rather than a template
 * literal at the call site: a build newer than this frontend can classify a
 * failure this one has no key for, and `t()` on a key the catalogue does not
 * have returns undefined despite its `string` type - which the first
 * .toLowerCase() downstream turns into a blank page. Showing the bare code is
 * ugly and true; showing nothing is neither.
 */
function problemText(t: (key: TranslationKey) => string, code: string): string {
  const key = `settings.eventTargets.problem.${code}` as TranslationKey;
  const text = t(key) as string | undefined;
  return text && text !== key ? text : code;
}

/** An RFC3339 stamp in the reader's own locale, or the raw string when the
 *  server sends something this browser will not parse. Never a relative "3
 *  minutes ago": this value is fetched once, on mount, so a relative time would
 *  go on ageing on screen while the number behind it stood still. */
function fmtWhen(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}
