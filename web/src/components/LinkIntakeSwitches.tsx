import { ToggleRow } from './ui';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useCnl } from '../lib/useCnl';
import { useWatchSwitch } from './WatchElsewhere';
import { WATCH_SUPPORTED } from '../lib/clipboardWatch';
import { isDesktop } from '../lib/desktop';
import { moduleDetail } from '../pages/settings/tx';

/**
 * LinkIntakeSwitches are the add-links options' switches for Click'n'Load and
 * the clipboard watch, sharing their state with the settings card.
 *
 * The clipboard row stays visible but disabled where the browser cannot read
 * the clipboard. Click'n'Load disappears when the server has no such module,
 * since there is nothing to enable.
 */
export function LinkIntakeSwitches() {
  const { t } = useT();
  const { toast } = useToast();
  const { row, busy, set } = useCnl();
  const { watch, flip, dialog } = useWatchSwitch();

  const cnlSwitchable = !!row && row.verdict === 'shipped' && row.switch !== 'none';

  async function onCnl(on: boolean) {
    try {
      await set(on);
    } catch (e) {
      toast(
        t('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }),
        'fail',
      );
    }
  }

  return (
    <>
      {cnlSwitchable && (
        <ToggleRow
          label={t('settings.module.cnl')}
          // What it takes, then the live reading, such as the address it
          // listens on.
          hint={[t('settings.linkIntake.cnlHint'), moduleDetail(t, row) ?? '']}
          checked={row.enabled}
          onChange={(on) => void onCnl(on)}
          disabled={busy}
        />
      )}
      <ToggleRow
        label={t('intake.clipboardWatch')}
        hint={
          !WATCH_SUPPORTED
            ? t('intake.clipboardWatchUnavailable')
            : isDesktop()
              ? t('intake.clipboardWatchHintDesktop')
              : t('intake.clipboardWatchHint')
        }
        checked={watch}
        onChange={flip}
        disabled={!WATCH_SUPPORTED}
      />
      {dialog}
    </>
  );
}
