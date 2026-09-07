import { Button } from './ui';
import { IconBrowser, IconClipboard } from '../lib/icons';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useCnl } from '../lib/useCnl';
import { useClipboardWatch } from '../lib/useClipboardWatch';
import { WATCH_SUPPORTED } from '../lib/clipboardWatch';

/**
 * The collector's own two switches for the two ways a link arrives without
 * being typed: Click'n'Load, and the clipboard watch.
 *
 * Labelled buttons, not square badges (jdp, 2026-09-07: "CnL und Zwischenablage
 * beobachten sollen da ein button sein den man aus und einschalten kann", and
 * asked where: "Im Linksammler, als beschriftete Buttons"). A glyph alone could
 * not say WHICH of the two intakes it was, and these are modes rather than
 * actions, so their own state has to be readable without hovering: `primary`
 * when on, `secondary` when off, which is the same on/off pairing the transport
 * buttons in the head card already use.
 *
 * They read the same state as the Linkeingang card in the settings, not a copy
 * of it. Click'n'Load goes through the module registry, so a switch here and a
 * switch there are the same PUT; the clipboard watch is a remembered client
 * field, so both read the same one.
 *
 * The clipboard button stays VISIBLE where the browser cannot read the
 * clipboard, disabled, with the reason in its tooltip (jdp, same round: "Button
 * ausgegraut mit Erklärung im Hover"). It used to hide itself there, which kept
 * the row tidy at the cost of never letting anybody find out the feature
 * exists. Click'n'Load is different and still disappears when the server has no
 * such module at all - that is a build without the listener, not a browser
 * restriction, and there is nothing for a disabled button to promise.
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
          // The live detail, not the label: "listening on 127.0.0.1:9666" is
          // what somebody hovering this wants to know, and it is the one thing
          // the button itself cannot show.
          title={`${t('settings.module.cnl')}${row.detail ? ` — ${row.detail}` : ''}`}
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
        title={WATCH_SUPPORTED ? t('intake.clipboardWatchHint') : t('intake.clipboardWatchUnavailable')}
        onClick={() => setWatch(!watch)}
        disabled={!WATCH_SUPPORTED}
      >
        {t('intake.clipboardWatch')}
      </Button>
    </>
  );
}
