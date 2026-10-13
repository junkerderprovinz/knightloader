import { UnitNumberInput } from '../../../components/ui';
import { RATE_UNITS } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { SettingRow } from '../controls';

/**
 * UploadLimitField is the torrent upload limit as the Torrents settings page
 * draws it. The shell bar's quick settings show this same field. `value` and
 * `onValue` are bytes per second, 0 for no limit, like the download limit.
 */
export function UploadLimitField({ value, onValue }: { value: number; onValue: (bytes: number) => void }) {
  const { t } = useT();
  return (
    <SettingRow
      control="short"
      label={t('settings.torrents.uploadLimit')}
      hint={t('settings.torrents.uploadLimitHint')}
    >
      <UnitNumberInput value={value} units={RATE_UNITS} onValue={onValue} />
    </SettingRow>
  );
}
