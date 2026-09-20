import { Card, Field, FieldGroup, NumberInput, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { VolumeUsageRow } from '../../../components/VolumeMeter';
import { useT, type TranslationKey } from '../../../lib/i18n';
import type { VolumeCapAction } from '../../../lib/api';
import { useDraft } from '../context';

// The volume cap: how much may finish downloading in one period and what
// happens once it has. A cap of 0 is off. The figure sums the sizes hosters
// announced for finished downloads, not the traffic on the line.

// Decimal gigabytes, because providers sell allowances decimally; the disk card
// uses binary GiB because it reads the filesystem.
const GB = 1e9;

// Rounded for display only, so opening the page does not rewrite the draft.
const toGB = (b: number) => Math.round((b / GB) * 100) / 100;
// The server reads a negative as 0, so none is sent.
const fromGB = (v: number) => Math.max(0, Math.round(v * GB));

const STEP_GB = 10;

// A literal list, since a new action needs a build that knows how to carry it
// out (settings.sanitizeVolume).
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

        {/* The server clamps to 1..31 and restarts a period set for the 31st
            on the last day of a short month. */}
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

      {cfg.volumeCapAction === 'throttle' && (
        <Field label={t('settings.volume.throttle')} hint={t('settings.volume.throttleHint')}>
          {/* KiB/s like the speed limit. min is 0 because a fresh install
              stores 0. */}
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
