import { IconBadge } from './ui';
import { IconBrowser, IconClipboard } from '../lib/icons';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useCnl } from '../lib/useCnl';
import { useClipboardWatch } from '../lib/useClipboardWatch';
import { WATCH_SUPPORTED } from '../lib/clipboardWatch';

/**
 * The collector's own two switches for the two ways a link arrives here
 * without being typed: Click'n'Load, and the clipboard watch (jdp, 2026-09-07:
 * "Da könnten wir auch zwei schaltflächen für die beiden optionen im
 * Linksammler einfügen").
 *
 * The same two switches as the Linkeingang card in the settings, reading the
 * same state - not a second copy of it. Click'n'Load goes through the module
 * registry, so a switch here and a switch there are the same PUT; the clipboard
 * watch is a remembered client field, so both read the same one.
 *
 * Each badge is absent, not disabled, where the thing it switches does not
 * exist: no Click'n'Load module on this instance (a desktop build), or a
 * browser that cannot read the clipboard at all (any plain-HTTP address - see
 * clipboardWatch.ts). A row of dead badges over a paste box would explain a
 * browser restriction to somebody who only wanted to add a link.
 */
export function LinkIntakeBadges() {
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
        <IconBadge
          icon={<IconBrowser width={16} height={16} />}
          hue={3}
          active={row.enabled}
          // The live detail, not the label: "listening on 127.0.0.1:9666" is
          // what somebody hovering a switch on this page wants to know, and it
          // is the one thing the badge itself cannot show.
          title={`${t('settings.module.cnl')}${row.detail ? ` — ${row.detail}` : ''}`}
          aria-label={t('settings.module.cnl')}
          onClick={() => void onCnl()}
          disabled={busy}
        />
      )}
      {WATCH_SUPPORTED && (
        <IconBadge
          icon={<IconClipboard width={16} height={16} />}
          hue={4}
          active={watch}
          title={t('intake.clipboardWatch')}
          aria-label={t('intake.clipboardWatch')}
          onClick={() => setWatch(!watch)}
        />
      )}
    </>
  );
}
