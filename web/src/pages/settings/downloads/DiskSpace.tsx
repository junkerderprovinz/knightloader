import { DiskVolumeRow } from '../../../components/DiskSpaceTile';
import { Card, Field, NumberInput, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { downloadsVolume, spaceHint, useDiskSpace } from '../../../lib/useDiskSpace';
import { useDraft } from '../context';

// The three disk floors decide whether a download may start and whether a
// running one goes back to the queue. Each is off at 0 and stays editable even
// where the platform cannot report free space, since the settings travel with
// the instance.

// GiB in the boxes, bytes on the wire, binary like every byte count in the app.
const GIB = 1024 ** 3;

// Rounded for display only, so opening the page does not rewrite the draft.
const toGiB = (b: number) => Math.round((b / GIB) * 100) / 100;
// clampDiskBytes reads a negative as 0, so none is sent.
const fromGiB = (v: number) => Math.max(0, Math.round(v * GIB));

// The server's maxDiskBytes (1 << 50) in GiB.
const MAX_GIB = 1024 * 1024;

// Half a GiB, also the reserve's shipped value.
const STEP_GIB = 0.5;

export function DiskSpaceCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // sanitizeDiskSpace raises the start floor to the stop floor, or the watcher
  // and the dispatcher would stop and restart a transfer every fifteen seconds.
  const lowFloorGiB = toGiB(cfg.diskCriticalSpace);

  // Only the download folder; the Overview tile shows the others.
  const report = useDiskSpace();
  const here = downloadsVolume(report);

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.downloads.diskHint')}>{t('settings.downloads.diskTitle')}</SectionTitle>

      {/* The row gets the draft, so its marks move with the spinners. */}
      {here && (
        <div className="glim-well p-0">
          <DiskVolumeRow v={here} cfg={cfg} hint={spaceHint(t, report)} />
        </div>
      )}

      {/* Per file and dispatch pass: free space minus what the pass promised
          must cover the remaining bytes plus this reserve. The hint says so. */}
      <Field label={t('settings.downloads.diskReserve')} hint={t('settings.downloads.diskReserveHint')}>
        <NumberInput
          value={toGiB(cfg.diskReserve)}
          min={0}
          max={MAX_GIB}
          step={STEP_GIB}
          onValue={(v) => patch({ diskReserve: fromGiB(v) })}
        />
      </Field>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {/* min is the stop floor. The server raises this box rather than
            lowering the stop floor, which protects a nearly full volume. */}
        <Field label={t('settings.downloads.diskLowSpace')} hint={t('settings.downloads.diskLowSpaceHint')}>
          <NumberInput
            value={toGiB(cfg.diskLowSpace)}
            min={lowFloorGiB}
            max={MAX_GIB}
            step={STEP_GIB}
            onValue={(v) => patch({ diskLowSpace: fromGiB(v) })}
          />
        </Field>

        {/* The watcher reads this one every fifteen seconds, and a transfer
            that cannot resume loses its bytes when it is held back. */}
        <Field label={t('settings.downloads.diskCriticalSpace')} hint={t('settings.downloads.diskCriticalSpaceHint')}>
          <NumberInput
            value={toGiB(cfg.diskCriticalSpace)}
            min={0}
            max={MAX_GIB}
            step={STEP_GIB}
            onValue={(v) => patch({ diskCriticalSpace: fromGiB(v) })}
          />
        </Field>
      </div>
    </Card>
  );
}
