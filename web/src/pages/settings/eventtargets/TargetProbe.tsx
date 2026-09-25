import { useState } from 'react';
import { Button, FieldGroup } from '../../../components/ui';
import { fmtUnit } from '../../../lib/format';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  MAX_RESPONSE_BODY,
  testEventTarget,
  usableAddress,
  type EventTargetRow,
  type EventTargetTest,
} from '../../../lib/eventtargets';

/**
 * TargetProbe sends one made-up event to this target and shows the answer as it
 * came back. It works on an unsaved row: masked header values go over as stars
 * and the server merges the stored ones back by the save path's rule. A broken
 * row comes back as a 400 with a code, a refusal by the far end as a normal
 * result, and the two are shown differently.
 */
export function TargetProbe({ row }: { row: EventTargetRow }) {
  const { t } = useT();
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<EventTargetTest | null>(null);
  const [refused, setRefused] = useState('');

  const run = async () => {
    setBusy(true);
    setRefused('');
    setResult(null);
    try {
      setResult(await testEventTarget(row));
    } catch (e) {
      setRefused(String(e).replace(/^(Error|EventTargetApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  return (
    <FieldGroup label={t('settings.eventTargets.testResult')} hint={t('settings.eventTargets.testHint')}>
      <div className="flex flex-col gap-2">
        <Button className="w-fit" disabled={busy || !usableAddress(row.url)} onClick={() => void run()}>
          {busy ? t('settings.eventTargets.testBusy') : t('settings.eventTargets.test')}
        </Button>

        {refused && <p dir="auto" className="text-xs text-statusWarn">{refused}</p>}

        {result && (
          <div className="glim-well flex flex-col gap-2 p-3 text-xs">
            {/* What was sent comes first, since a refusal only reads against it. */}
            <p className="text-carbon-textMuted">{t('settings.eventTargets.testSent')}</p>
            <pre dir="ltr" className="overflow-x-auto whitespace-pre-wrap break-all text-carbon-textSub">
              {`${result.sent.method} ${result.sent.url}\n${Object.keys(result.sent.headers)
                .sort()
                .map((k) => `${k}: ${result.sent.headers[k]}`)
                .join('\n')}${result.sent.body ? `\n\n${result.sent.body}` : ''}`}
            </pre>

            {result.error ? (
              // Nothing answered: the translated reason, then the transport's own words.
              <>
                <p className="text-statusWarn">
                  {result.code ? problemText(t, result.code) : t('settings.eventTargets.testNoAnswer')}
                </p>
                <p className="text-carbon-textMuted">{result.error}</p>
              </>
            ) : (
              <>
                <p className={result.status >= 200 && result.status < 300 ? 'text-carbon-textSub' : 'text-statusWarn'}>
                  {t('settings.eventTargets.testResult')}: {result.statusText || String(result.status)}
                </p>
                {result.code && <p className="text-statusWarn">{problemText(t, result.code)}</p>}
                {result.body ? (
                  <pre dir="ltr" className="overflow-x-auto whitespace-pre-wrap break-all text-carbon-textMuted">
                    {result.body}
                  </pre>
                ) : (
                  <p className="text-carbon-textMuted">{t('settings.eventTargets.testEmptyBody')}</p>
                )}
                {result.truncated && (
                  <p className="text-carbon-textMuted">
                    {t('settings.eventTargets.testTruncated', { n: MAX_RESPONSE_BODY })}
                  </p>
                )}
              </>
            )}
            <p className="text-carbon-textMuted">
              {t('settings.eventTargets.took', { duration: fmtUnit(result.durationMs, 'ms') })}
            </p>
          </div>
        )}
      </div>
    </FieldGroup>
  );
}

/** problemText returns the sentence for a notify.Problem code, or the code itself. */
function problemText(t: (key: TranslationKey) => string, code: string): string {
  const key = `settings.eventTargets.problem.${code}` as TranslationKey;
  const text = t(key) as string | undefined;
  return text && text !== key ? text : code;
}
