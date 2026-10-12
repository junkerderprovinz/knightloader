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
            {/* The state at the row's end, so the states of all parts stand in one column. */}
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
              {/* A part this build has no label for shows its raw id. */}
              <span className="min-w-0 text-sm text-carbon-text">{healthLabel(t, 'health.part.', s.id)}</span>
              <span className="ms-auto">
                <LabelBadge tone={healthTone(s.state)} label={healthLabel(t, 'health.state.', s.state)} />
              </span>
            </div>
            {s.remedy && (
              <span className="text-xs leading-relaxed text-carbon-textSub">
                {healthLabel(t, 'health.remedy.', s.remedy)}
              </span>
            )}
            {s.detail && (
              <span className="glim-num break-all text-meta text-carbon-textMuted" dir="ltr">
                {s.detail}
              </span>
            )}
          </li>
        ))}
      </ul>
    </Card>
  );
}
