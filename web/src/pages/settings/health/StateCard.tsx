import { Card, ErrorCard, SectionTitle } from '../../../components/ui';
import type { HealthReport, StartupReport } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { useSelfTest } from '../../../lib/useSelfTest';
import { SelfTestButton, SelfTestFold } from '../diagnostics/SelfTest';
import { StartupFold } from '../diagnostics/StartupReport';
import { PartsList } from './Parts';

/**
 * StateCard answers whether this instance works: every part with its traffic
 * light, then the self-test and what was checked at startup as rows that open.
 * `startup` is undefined while the diagnostics bundle it comes in is missing,
 * and the row is left out then rather than claim nothing was checked.
 */
export function StateCard({
  hue,
  report,
  failed,
  startup,
}: {
  hue: number;
  report: HealthReport | null;
  failed: boolean;
  startup?: StartupReport | null;
}) {
  const { t } = useT();
  const test = useSelfTest();

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.health.stateHint')} right={<SelfTestButton test={test} />}>
        {t('settings.health.stateTitle')}
      </SectionTitle>

      {/* A failed poll after the first report leaves that report standing, and
          the hook polls every ten seconds, so there is no retry button. */}
      {report ? (
        <PartsList report={report} />
      ) : failed ? (
        <ErrorCard nested message={t('settings.health.loadFailed')} />
      ) : (
        <span className="text-sm text-carbon-textMuted">{t('common.loading')}</span>
      )}

      <div className="flex flex-col gap-3">
        <SelfTestFold test={test} />
        {startup !== undefined && <StartupFold report={startup} />}
      </div>
    </Card>
  );
}
