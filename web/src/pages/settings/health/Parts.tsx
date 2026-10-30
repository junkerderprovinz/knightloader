import { useNavigate } from 'react-router-dom';
import { Button, LabelBadge } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { healthLabel, healthTone } from '../../../lib/useHealthReport';
import type { HealthReport, HealthSubsystem } from '../../../lib/api';
import { requestJump } from '../jump';
import { IconLog } from '../../../lib/icons';

/**
 * PartsList lists every part of the app that can fail on its own, with its
 * state, remedy and detail in plain view. `remedy` is an id for a translated
 * hint; `detail` is the failing service's own message, shown verbatim. A part
 * in trouble that writes to the log leads to its lines there.
 */
export function PartsList({ report }: { report: HealthReport }) {
  const { t } = useT();

  return (
    <ul data-new="health-parts" className="flex flex-col gap-4">
      {report.subsystems.map((s) => (
        <li key={s.id} className="flex flex-col gap-1.5">
          {/* The state at the row's end, so the states of all parts stand in one column. */}
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
            {/* A part this build has no label for shows its raw id. */}
            <span className="min-w-0 text-sm text-carbon-text">
              {s.id === 'jd' ? (
                <span data-new="jd-state">{healthLabel(t, 'health.part.', s.id)}</span>
              ) : (
                healthLabel(t, 'health.part.', s.id)
              )}
            </span>
            <span className="ms-auto">
              <LabelBadge tone={healthTone(s.state)} label={healthLabel(t, 'health.state.', s.state)} />
            </span>
          </div>
          <Reason part={s} />
        </li>
      ))}
    </ul>
  );
}

/**
 * Reason is why a part is not fine: the remedy, the service's own words and
 * the way to its log lines. A part in trouble that says nothing of its own
 * says so, rather than leave a red state without a word.
 */
function Reason({ part: s }: { part: HealthSubsystem }) {
  const { t } = useT();
  const navigate = useNavigate();
  const trouble = s.state === 'degraded' || s.state === 'failed';
  if (!trouble && !s.remedy && !s.detail) return null;

  return (
    <div data-new="health-reason" className="flex flex-col gap-1.5">
      {s.remedy ? (
        <span className="text-xs leading-relaxed text-carbon-textSub">
          {healthLabel(t, 'health.remedy.', s.remedy)}
        </span>
      ) : (
        trouble &&
        !s.detail && (
          <span className="text-xs leading-relaxed text-carbon-textSub">{t('settings.health.noReason')}</span>
        )
      )}
      {s.detail && (
        <span className="glim-num break-all text-meta text-carbon-textMuted" dir="ltr">
          {s.detail}
        </span>
      )}
      {trouble && s.log && (
        <span className="flex justify-end">
          <Button
            icon={<IconLog />}
            kind="secondary"
            onClick={() => {
              requestJump({ page: 'health', title: 'settings.diagnostics.logCardTitle' });
              navigate(`/settings/health?log=${encodeURIComponent(s.log ?? '')}`);
            }}
          >
            {t('settings.health.openLog')}
          </Button>
        </span>
      )}
    </div>
  );
}
