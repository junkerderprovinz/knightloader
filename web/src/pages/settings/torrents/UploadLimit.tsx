import { UnitNumberInput } from '../../../components/ui';
import { RATE_UNITS } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { SettingRow } from '../controls';

const KIB = 1024;

/**
 * UploadLimitField is the torrent upload limit as the Torrents settings page
 * draws it. The shell bar's quick settings show this same field. `value` and
 * `onValue` are KiB/s, as the setting stores them, 0 for no limit; the field
 * shows them like every other speed.
 */
export function UploadLimitField({ value, onValue }: { value: number; onValue: (kibs: number) => void }) {
  const { t } = useT();
  return (
    <SettingRow
      control="short"
      label={t('settings.torrents.uploadLimit')}
      hint={t('settings.torrents.uploadLimitHint')}
    >
      <UnitNumberInput
        value={value * KIB}
        units={RATE_UNITS}
        snap={(bytes) => Math.round(bytes / KIB) * KIB}
        onValue={(bytes) => onValue(bytes / KIB)}
      />
    </SettingRow>
  );
}
