import { Card, InfoBubble, LabelBadge, SectionTitle } from '../../../components/ui';
import { fmtDate, fmtUptime } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { healthLabel, healthTone, sampleAge } from '../../../lib/useHealthReport';
import type { HealthReport } from '../../../lib/api';
import { Reading } from './Reading';

/**
 * The head card: one word for the whole instance, and the three facts a bug
 * report always opens with.
 *
 * THE BADGE IS THE SUMMARY, and it is the worst of the rows below with two
 * deliberate exceptions: a part that is not set up here and a part that cannot
 * be checked here never make it worse than "working". A fresh install has four
 * such rows - no relay, no sidecar, no yt-dlp, no accounts - and a summary that
 * called that impaired would be permanently orange on a perfectly ordinary box,
 * which is how a status light stops being read at all.
 *
 * THE (i) BESIDE IT IS ABOUT A DIFFERENT ADDRESS and is the one piece of prose
 * on this page that has to be here rather than in the docs. /api/health goes on
 * answering "ok" for as long as the process is up, whatever this page says,
 * because the container's own health check, the Click'n'Load bridge and the
 * phone app's search for instances all read it. Somebody who finds this page
 * reporting a failure and then sees the old address answering "ok" will
 * otherwise conclude one of the two is broken.
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
            {/* tip alone, with no `label` of its own: the tip IS a plain
                sentence, which InfoBubble uses as the trigger's accessible
                name. A second name here would be the same sentence twice. */}
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
        {/* fmtDate and not the raw RFC3339, because this is the only date on
            the page and it is read rather than compared. It is the SERVER's
            clock formatted in the READER's locale, which the uptime's own (i)
            says out loud - the two are routinely different machines. */}
        <Reading label={t('settings.health.startedAt')} value={fmtDate(report.startedAt)} />
      </div>

      {/* The age of the reading rather than an animated figure. The probing
          rows are shared for half a minute on the server, so this page can
          legitimately disagree with the rows on the Downloads page for that
          long, and a gauge that ticked like a live one would be claiming the
          contradiction is impossible. */}
      {age !== null && (
        <span className="text-[11px] text-carbon-textMuted">{t('settings.health.sampled', { n: age })}</span>
      )}
    </Card>
  );
}
