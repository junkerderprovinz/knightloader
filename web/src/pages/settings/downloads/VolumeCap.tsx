import { Card, Field, FieldGroup, NumberInput, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { VolumeUsageRow } from '../../../components/VolumeMeter';
import { useT, type TranslationKey } from '../../../lib/i18n';
import type { VolumeCapAction } from '../../../lib/api';
import { useDraft } from '../context';

/**
 * How much may finish downloading in one period, and what happens once it has.
 *
 * It sits beside DiskSpace because the two are the same kind of thing: a guard
 * that decides whether a download STARTS, rather than a knob that shapes one
 * that already has. Everything above them on this page shapes.
 *
 * NO MASTER TOGGLE, and no "off" among the three actions. 0 in the cap box is
 * the off state - a real answer, the way historyMax, keepFinishedDays and the
 * three disk floors already work on this page - and with no cap set an action
 * here does nothing at all. A switch above the box would be a control with no
 * field behind it on the wire, and a fourth "off" action would be a second way
 * to say the same thing that could disagree with the first.
 *
 * WHAT IT COUNTS IS NOT WHAT YOUR LINE MOVED, and the title's bubble says so
 * rather than this card implying otherwise. The figure is the sum of the sizes
 * hosters ANNOUNCED for finished downloads: a download nobody sized counts as
 * zero however many gigabytes crossed the line, and what a torrent uploads -
 * the traffic a seeder's provider actually meters - is recorded nowhere and
 * cannot be. Do not add a readout here that suggests otherwise; add the
 * measurement first.
 */

// DECIMAL gigabytes, 1e9. The disk card directly above this one is BINARY GiB,
// and both are right: a volume allowance is what a provider SELLS, and every
// provider quotes it decimally, so 500 GB is 500 000 000 000 bytes on the
// invoice and a binary reading would print 465 for the number the customer
// bought. lib/format.ts's fmtGB makes exactly this argument about a debrid
// account's allowance and is the formatter the readings use. A free-space floor
// is measured against what the filesystem reports, which is binary. The hint
// under the box says the two disagree, out loud, because somebody comparing
// them will otherwise file it as a bug.
const GB = 1e9;

// Round on the way OUT, never on the way in - DiskSpace's own idiom and for its
// own reason: rewriting the draft on mount would turn opening this page into an
// edit, so a raw byte figure somebody typed on the Advanced page reads here as
// its nearest two decimals and stays untouched on the wire until the spinner
// actually moves.
const toGB = (b: number) => Math.round((b / GB) * 100) / 100;
// Math.max(0, ...) mirrors the server's own clamp, which reads any negative as
// 0, meaning no cap. The save is never refused, so a negative would come back as
// a silently different number in the box.
const fromGB = (v: number) => Math.max(0, Math.round(v * GB));

// The granularity an allowance is actually sold at. It also means the first
// press of the stepper from 0 lands on a round number rather than on 1.
const STEP_GB = 10;

// No `max`, deliberately. The server states no ceiling for this one and serves
// none, so a bound written here would be a copy of a number that does not exist
// - and the day one appears, this spinner would be the thing disagreeing with
// it. Same call QuickSettings makes about the two concurrency spinners.

// The three actions are a closed set: they are named in settings.sanitizeVolume
// and carried out in exactly two places on the Go side, so a fourth cannot
// appear without a build that also knows what to do with it. That is why this
// is a literal list and not a fetch from /api/options the way resumeOnStart and
// the idle actions are - those two really can grow behind a running frontend.
const ACTIONS: { id: VolumeCapAction; key: TranslationKey }[] = [
  { id: 'report', key: 'settings.volume.actionReport' },
  { id: 'pause', key: 'settings.volume.actionPause' },
  { id: 'throttle', key: 'settings.volume.actionThrottle' },
];

export function VolumeCapCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.volume.titleHint')}>{t('settings.volume.title')}</SectionTitle>

      {/* The reading first, because it is the context for every number under it:
          a cap you are choosing without seeing where the counter stands is a
          number picked in the dark. It feeds itself off the same broadcast the
          shell bar's chip does and holds no timer. */}
      <VolumeUsageRow />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('settings.volume.cap')} hint={t('settings.volume.capHint')}>
          <NumberInput
            value={toGB(cfg.volumeCap)}
            min={0}
            step={STEP_GB}
            onValue={(v) => patch({ volumeCap: fromGB(v) })}
          />
        </Field>

        {/* 1..31 because that is what a day of the month is, and the server
            clamps to the same range. It does NOT clamp to the month's own
            length - a period that started on the 31st restarts on the last day
            of a short month instead, which is the arithmetic the hint promises
            and the one thing this field must not contradict. */}
        <Field label={t('settings.volume.resetDay')} hint={t('settings.volume.resetDayHint')}>
          <NumberInput
            value={cfg.volumeCapResetDay}
            min={1}
            max={31}
            onValue={(v) => patch({ volumeCapResetDay: Math.min(31, Math.max(1, Math.round(v))) })}
          />
        </Field>
      </div>

      <FieldGroup layout="row" label={t('settings.volume.action')} hint={t('settings.volume.actionHint')}>
        <Tabs
          variant="well"
          label={t('settings.volume.action')}
          active={cfg.volumeCapAction}
          onSelect={(id) => patch({ volumeCapAction: id as VolumeCapAction })}
          items={ACTIONS.map((a) => ({ id: a.id, label: t(a.key) }))}
        />
      </FieldGroup>

      {/* Only for the action it belongs to, the same conditional the end-of-queue
          countdown already uses one card down this page. A speed that applies to
          nothing is a field somebody sets and then wonders why it did nothing. */}
      {cfg.volumeCapAction === 'throttle' && (
        <Field label={t('settings.volume.throttle')} hint={t('settings.volume.throttleHint')}>
          {/* KiB/s, matching the speed limit in the Limits card above: this is a
              rate and rates in this app are binary, unlike the allowance two
              boxes up. min is 0 and not 1 even though 1 is the smallest USEFUL
              value, because 0 is what a fresh install has stored - a box whose
              floor is above what is on the wire would show a number its own
              stepper refuses to return to. The hint says what 0 does. */}
          <NumberInput
            value={Math.round(cfg.volumeCapThrottle / 1024)}
            min={0}
            step={256}
            onValue={(v) => patch({ volumeCapThrottle: Math.max(0, v) * 1024 })}
          />
        </Field>
      )}
    </Card>
  );
}
