import { IconBadge } from './ui';
import { IconCopy, IconLink } from '../lib/icons';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useCnl } from '../lib/useCnl';
import { useWatchSwitch } from './WatchElsewhere';
import { WATCH_SUPPORTED } from '../lib/clipboardWatch';
import { isDesktop } from '../lib/desktop';
import { moduleDetail } from '../pages/settings/tx';

/**
 * LinkIntakeButtons are the add-links form's buttons for Click'n'Load and the
 * clipboard watch, sharing their state with the settings card. Each holds its
 * state: filled while on, and a press turns it the other way.
 *
 * The clipboard button stays visible but disabled where the browser cannot read
 * the clipboard. Click'n'Load disappears when the server has no such module,
 * since there is nothing to enable.
 */
export function LinkIntakeButtons({ hue }: { hue: number }) {
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
        <IconBadge
          labelled
          active={row.enabled}
          hue={hue}
          icon={<IconLink width={16} height={16} />}
          title={t('settings.module.cnl')}
          aria-label={t('settings.module.cnl')}
          // What it takes, then the live reading, such as the address it
          // listens on.
          hint={[t('settings.linkIntake.cnlHint'), moduleDetail(t, row) ?? ''].filter(Boolean).join(' ')}
          onClick={() => void onCnl(!row.enabled)}
          disabled={busy}
        />
      )}
      <IconBadge
        labelled
        active={watch}
        data-new="intake-glyphs"
        hue={hue + 1}
        icon={<IconCopy width={16} height={16} />}
        title={t('intake.clipboardWatch')}
        aria-label={t('intake.clipboardWatch')}
        hint={
          !WATCH_SUPPORTED
            ? t('intake.clipboardWatchUnavailable')
            : isDesktop()
              ? t('intake.clipboardWatchHintDesktop')
              : t('intake.clipboardWatchHint')
        }
        onClick={() => flip(!watch)}
        disabled={!WATCH_SUPPORTED}
      />
      {dialog}
    </>
  );
}
