import { Card, InfoBubble, LabelBadge, SectionTitle } from '../../../components/ui';
import { fmtDate, fmtUptime } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { healthLabel, healthTone, sampleAge } from '../../../lib/useHealthReport';
import type { HealthReport } from '../../../lib/api';
import { Reading } from './Reading';

/**
 * OverallCard sums the instance up in one word, next to version, uptime and
 * start time. The (i) explains that /api/health keeps answering "ok" while the
 * process runs, because the container health check, the Click'n'Load bridge
 * and the phone app all read it.
 */
export function OverallCard({ hue, report }: { hue: number; report: HealthReport }) {
  const { t } = useT();
  const age = sampleAge(report);

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle
        hint={t('settings.health.titleHint')}
        right={
          <span className="flex items-center gap-2">
            <LabelBadge tone={healthTone(report.status)} label={healthLabel(t, 'health.state.', report.status)} />
            {/* No label: InfoBubble names the trigger after the tip. */}
            <InfoBubble tip={t('settings.health.oldHealthHint')} />
          </span>
        }
      >
        {t('settings.health.title')}
      </SectionTitle>

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <Reading label={t('settings.health.version')} value={report.version} />
        <Reading
          label={t('settings.health.uptime')}
          hint={t('settings.health.uptimeHint')}
          value={fmtUptime(report.uptimeSeconds)}
        />
        <Reading label={t('settings.health.startedAt')} value={fmtDate(report.startedAt)} />
      </div>

      {/* The server caches the probes for half a minute, so the page shows
          how old the reading is instead of pretending to be live. */}
      {age !== null && (
        <span className="text-[11px] text-carbon-textMuted">{t('settings.health.sampled', { n: age })}</span>
      )}
    </Card>
  );
}
