import { Card, LabelBadge, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { healthLabel, healthTone } from '../../../lib/useHealthReport';
import type { HealthReport } from '../../../lib/api';

/**
 * One row per part of the app that can fail on its own.
 *
 * THE STATE, THE REMEDY AND THE DETAIL ARE THE ROW'S CONTENT, not chrome, and
 * that is why none of the three is hidden behind an (i). What a bubble is for
 * on this page is the meaning of the five states, which is the same sentence
 * for every row and belongs once, on the card's own title. What a row says
 * about ITSELF changes per row and per minute, and putting that behind a hover
 * would mean the one page in the app whose whole purpose is to report a fault
 * reports it invisibly.
 *
 * THE TWO STRINGS ARE DIFFERENT KINDS OF STRING and are drawn differently on
 * purpose. `remedy` is a stable id this build looks a translated sentence up
 * from - it is advice, written here, in the reader's language. `detail` is the
 * failing service's OWN words, in whatever language it speaks, forwarded
 * verbatim: a dial error, a feed's HTTP status. It gets the monospace,
 * ltr treatment every other machine string in settings gets, because a
 * connection error mirrored into a right-to-left run is unreadable.
 *
 * NOTHING HERE IS A CONTROL. Reading a row changes nothing, and a part that
 * says it is not working has been failing for a while already - this is only
 * where somebody finds out.
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
              {/* healthLabel and never a switch over the ids: a part a newer
                  server has learnt before this build has a word for it renders
                  as its raw id, which is at least something somebody can search
                  for. A blank cell reads as a broken page. */}
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
