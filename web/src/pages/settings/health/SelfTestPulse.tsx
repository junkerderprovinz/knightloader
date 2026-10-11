import { Card, SectionTitle } from '../../../components/ui';
import { Pulse, type PulseCheck } from '../../../components/pictures/Pulse';
import { happened } from '../../../lib/countdown';
import { fmtDate } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { useSelfTest } from '../../../lib/useSelfTest';
import { CHECK_NAMES, STATUS_WORDS } from '../diagnostics/rows';
import { SelfTestButton } from '../diagnostics/SelfTest';
import { PageBadge } from '../ModuleToggle';

/**
 * SelfTestPulseCard draws the instance's self-test as a pulse, one bead per
 * check in the order they are made, and starts a run. The sentence behind
 * each answer is on the Diagnostics page, where the badge leads.
 */
export function SelfTestPulseCard({ hue }: { hue: number }) {
  const { t } = useT();
  const test = useSelfTest();
  const { run, error } = test;

  const checks: PulseCheck[] = (run?.planned ?? []).map((id) => {
    const res = test.resultOf(id);
    return {
      id,
      // A check this build has no name for shows its raw id.
      name: CHECK_NAMES[id] ? t(CHECK_NAMES[id]) : id,
      status: res?.status ?? 'pending',
      word: res ? t(STATUS_WORDS[res.status]) : t('settings.selftest.pending'),
    };
  });

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.selftest.hint')} right={<SelfTestButton test={test} />}>
        {t('settings.selftest.title')}
      </SectionTitle>

      {checks.length === 0 ? (
        <span className="text-sm text-carbon-textMuted">{t('settings.selftest.never')}</span>
      ) : (
        <Pulse checks={checks} running={test.running} label={t('settings.selftest.title')} />
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="text-meta text-carbon-textMuted">
          {run !== null && happened(run.finishedAt) && t('settings.selftest.lastRun', { when: fmtDate(run.finishedAt) })}
        </span>
        <PageBadge page="diagnostics" title={t('settings.nav.diagnostics')} />
      </div>
      {error && <span className="text-sm text-carbon-textSub">{t('settings.selftest.startFailed', { error })}</span>}
    </Card>
  );
}
