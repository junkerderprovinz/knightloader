import { Button } from './ui';
import { IconBrowser, IconClipboard } from '../lib/icons';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useCnl } from '../lib/useCnl';
import { useClipboardWatch } from '../lib/useClipboardWatch';
import { WATCH_SUPPORTED } from '../lib/clipboardWatch';
import { moduleDetail } from '../pages/settings/tx';

/**
 * LinkIntakeButtons are the collector's switches for Click'n'Load and the
 * clipboard watch, sharing their state with the settings card. They are modes,
 * so `primary` means on and `secondary` off.
 *
 * The clipboard button stays visible but disabled where the browser cannot read
 * the clipboard. Click'n'Load disappears when the server has no such module,
 * since there is nothing to enable.
 */
export function LinkIntakeButtons() {
  const { t } = useT();
  const { toast } = useToast();
  const { row, busy, set } = useCnl();
  const [watch, setWatch] = useClipboardWatch();

  const cnlSwitchable = !!row && row.verdict === 'shipped' && row.switch !== 'none';

  async function onCnl() {
    try {
      await set(!row?.enabled);
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
        <Button
          kind={row.enabled ? 'primary' : 'secondary'}
          className="px-2.5 text-xs"
          icon={<IconBrowser width={14} height={14} />}
          // The live reading, such as the address it listens on; the button
          // already says its own name.
          title={moduleDetail(t, row)}
          onClick={() => void onCnl()}
          disabled={busy}
        >
          {t('settings.module.cnl')}
        </Button>
      )}
      <Button
        kind={watch ? 'primary' : 'secondary'}
        className="px-2.5 text-xs"
        icon={<IconClipboard width={14} height={14} />}
        hint={WATCH_SUPPORTED ? t('intake.clipboardWatchHint') : t('intake.clipboardWatchUnavailable')}
        onClick={() => setWatch(!watch)}
        disabled={!WATCH_SUPPORTED}
      >
        {t('intake.clipboardWatch')}
      </Button>
    </>
  );
}
