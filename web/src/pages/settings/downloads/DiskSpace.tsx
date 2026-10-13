import { DiskVolumeRow } from '../../../components/DiskSpaceTile';
import { Card, NumberInput, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { downloadsVolume, spaceHint, useDiskSpace } from '../../../lib/useDiskSpace';
import { useDraft } from '../context';
import { SettingRow } from '../controls';

// The three disk floors decide whether a download may start and whether a
// running one goes back to the queue. Each is off at 0 and stays editable even
// where the platform cannot report free space, since the settings travel with
// the instance.

// GB in the boxes, bytes on the wire, decimal like every byte count in the app.
const GB = 1e9;

// Rounded for display only, so opening the page does not rewrite the draft.
const toGB = (b: number) => Math.round((b / GB) * 100) / 100;
// clampDiskBytes reads a negative as 0, so none is sent.
const fromGB = (v: number) => Math.max(0, Math.round(v * GB));

// The server's maxDiskBytes, a petabyte, in GB.
const MAX_GB = 1e6;

// Half a GB, also the reserve's shipped value.
const STEP_GB = 0.5;

export function DiskSpaceCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // sanitizeDiskSpace raises the start floor to the stop floor, or the watcher
  // and the dispatcher would stop and restart a transfer every fifteen seconds.
  const lowFloorGB = toGB(cfg.diskCriticalSpace);

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
      <SettingRow
        control="short"
        label={t('settings.downloads.diskReserve')}
        hint={t('settings.downloads.diskReserveHint')}
      >
        <NumberInput
          value={toGB(cfg.diskReserve)}
          min={0}
          max={MAX_GB}
          step={STEP_GB}
          onValue={(v) => patch({ diskReserve: fromGB(v) })}
        />
      </SettingRow>

      <div className="flex flex-col gap-4">
        {/* min is the stop floor. The server raises this box rather than
            lowering the stop floor, which protects a nearly full volume. */}
        <SettingRow
          control="short"
          label={t('settings.downloads.diskLowSpace')}
          hint={t('settings.downloads.diskLowSpaceHint')}
        >
          <NumberInput
            value={toGB(cfg.diskLowSpace)}
            min={lowFloorGB}
            max={MAX_GB}
            step={STEP_GB}
            onValue={(v) => patch({ diskLowSpace: fromGB(v) })}
          />
        </SettingRow>

        {/* The watcher reads this one every fifteen seconds, and a transfer
            that cannot resume loses its bytes when it is held back. */}
        <SettingRow
          control="short"
          label={t('settings.downloads.diskCriticalSpace')}
          hint={t('settings.downloads.diskCriticalSpaceHint')}
        >
          <NumberInput
            value={toGB(cfg.diskCriticalSpace)}
            min={0}
            max={MAX_GB}
            step={STEP_GB}
            onValue={(v) => patch({ diskCriticalSpace: fromGB(v) })}
          />
        </SettingRow>
      </div>
    </Card>
  );
}
