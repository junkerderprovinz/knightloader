import { useState } from 'react';
import { TestButton } from '../../../components/TestButton';
import { FieldGroup, type ButtonVerdict } from '../../../components/ui';
import { fmtUnit } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { testEventTarget, type Channel, type EventTargetTest } from '../../../lib/eventtargets';
import { IconSendTest } from '../../../lib/icons';
import { problemText } from './problems';

/**
 * TargetProbe sends one made-up event to this channel and shows how it went.
 * It works on an unsaved or switched-off channel: masked secrets go over as
 * stars and the server merges the stored ones back by the save path's rule.
 */
export function TargetProbe({ row }: { row: Channel }) {
  const { t } = useT();
  const [result, setResult] = useState<EventTargetTest | null>(null);
  const [refused, setRefused] = useState('');

  const run = async (): Promise<ButtonVerdict> => {
    setRefused('');
    setResult(null);
    try {
      const answer = await testEventTarget(row);
      setResult(answer);
      return answer.ok ? 'ok' : 'fail';
    } catch (e) {
      setRefused(String(e).replace(/^(Error|EventTargetApiError):\s*/, ''));
      return 'fail';
    }
  };

  return (
    <FieldGroup label={t('settings.eventTargets.testResult')} hint={t('settings.eventTargets.testHint')}>
      <div className="flex flex-col gap-2">
        {refused && <p dir="auto" className="text-xs text-carbon-textSub">{refused}</p>}
        <div className="flex">
          <TestButton
            icon={<IconSendTest />}
            label={t('settings.eventTargets.test')}
            busyLabel={t('settings.eventTargets.testBusy')}
            words={{ ok: t('test.sent'), fail: t('test.failed') }}
            run={run}
            resetKey={JSON.stringify(row)}
          />
        </div>
        {result && (
          <div className="glim-well flex flex-col gap-2 p-3 text-xs">
            {result.ok ? (
              <p className="text-carbon-textSub">{t('settings.eventTargets.testOk')}</p>
            ) : (
              // The translated reason first, then the server's own words.
              <>
                <p className="text-statusWarn">{problemText(t, result.code ?? 'invalid')}</p>
                {result.error && <p className="text-carbon-textMuted">{result.error}</p>}
              </>
            )}
            <p className="text-carbon-textMuted">{t('settings.eventTargets.took', { duration: fmtUnit(result.durationMs, 'ms') })}</p>
          </div>
        )}
      </div>
    </FieldGroup>
  );
}
