import { Card, LabelBadge, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { healthLabel, healthTone } from '../../../lib/useHealthReport';
import type { HealthReport } from '../../../lib/api';

/**
 * PartsCard lists every part of the app that can fail on its own, with its
 * state, remedy and detail in plain view. `remedy` is an id for a translated
 * hint; `detail` is the failing service's own message, shown verbatim.
 */
export function PartsCard({ hue, report }: { hue: number; report: HealthReport }) {
  const { t } = useT();

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.health.partsHint')}>{t('settings.health.parts')}</SectionTitle>

      <ul className="flex flex-col gap-4">
        {report.subsystems.map((s) => (
          <li key={s.id} className="flex flex-col gap-1.5">
            <div className="flex flex-wrap items-center gap-2">
              {/* A part this build has no label for shows its raw id. */}
              <span className="text-sm text-carbon-text">{healthLabel(t, 'health.part.', s.id)}</span>
              <LabelBadge tone={healthTone(s.state)} label={healthLabel(t, 'health.state.', s.state)} />
            </div>
            {s.remedy && (
              <span className="text-xs leading-relaxed text-carbon-textSub">
                {healthLabel(t, 'health.remedy.', s.remedy)}
              </span>
            )}
            {s.detail && (
              <span className="glim-num break-all text-[11px] text-carbon-textMuted" dir="ltr">
                {s.detail}
              </span>
            )}
          </li>
        ))}
      </ul>
    </Card>
  );
}
