import { useState } from 'react';
import { Button, FieldGroup } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  MAX_RESPONSE_BODY,
  testEventTarget,
  usableAddress,
  type EventTargetRow,
  type EventTargetTest,
} from '../../../lib/eventtargets';

/**
 * Send one made-up event to this target, right now, and show the answer exactly
 * as it came back.
 *
 * IT REALLY SENDS. There is no dry run and deliberately no offer of one: the
 * whole worth of this button is that it answers about the request the target
 * will really make, and a dry run answers about a different request. The copy
 * behind the (i) says so in as many words, because somebody pressing this is
 * about to make their phone buzz.
 *
 * IT WORKS ON A ROW THAT HAS NOT BEEN SAVED, which is most of its value: a topic
 * name, a token and a body shape are typed blind, and the alternative is to save
 * them and wait for something to fail. The one thing it needs from the stored
 * copy is the header VALUES, which this browser has never been shown - so the
 * draft goes over with eight stars where each token is, and the server merges
 * the real one back under exactly the rule the save path uses (same row, same
 * address, same header name). That is why the answer shows stars again: nothing
 * about this exchange hands the browser a secret it did not already have.
 *
 * TWO SHAPES OF FAILURE, DRAWN DIFFERENTLY ON PURPOSE. A row that is itself
 * wrong (no address, a scheme that is not http, a method outside the three, a
 * placeholder that is never closed) comes back as a 400 with a typed code, and
 * is shown as something to fix here. A far end that refused comes back as a
 * perfectly normal 200 result with a status in it, and is shown as what somebody
 * else's server said. Showing them the same way would send people looking in the
 * wrong place.
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

        {refused && <p className="text-xs text-statusWarn">{refused}</p>}

        {result && (
          <div className="glim-well flex flex-col gap-2 p-3 text-xs">
            {/* What was sent, first: half of reading a refusal is seeing what
                the far end was actually given. dir=ltr and pre-wrap, because
                this is a request and not prose. */}
            <p className="text-carbon-textMuted">{t('settings.eventTargets.testSent')}</p>
            <pre dir="ltr" className="overflow-x-auto whitespace-pre-wrap break-all text-carbon-textSub">
              {`${result.sent.method} ${result.sent.url}\n${Object.keys(result.sent.headers)
                .sort()
                .map((k) => `${k}: ${result.sent.headers[k]}`)
                .join('\n')}${result.sent.body ? `\n\n${result.sent.body}` : ''}`}
            </pre>

            {result.error ? (
              // Nothing answered at all. The sentence for the typed code where
              // there is one, then the transport's own words, which for an
              // unusual failure say more than any category could.
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
            <p className="text-carbon-textMuted">{t('settings.eventTargets.testDuration', { ms: result.durationMs })}</p>
          </div>
        )}
      </div>
    </FieldGroup>
  );
}

/** The sentence for a notify.Problem code, or the code itself - see
 *  TargetHealth's own copy of this and the reason the fallback exists. */
function problemText(t: (key: TranslationKey) => string, code: string): string {
  const key = `settings.eventTargets.problem.${code}` as TranslationKey;
  const text = t(key) as string | undefined;
  return text && text !== key ? text : code;
}
