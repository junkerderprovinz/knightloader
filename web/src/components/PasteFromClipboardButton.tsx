import { useState } from 'react';
import { addLinks } from '../lib/api';
import { message } from '../lib/intake';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { Button } from './ui';
import { IconClipboard } from '../lib/icons';

// Read once at module scope, not per render: the answer cannot change
// without a page reload, and re-reading it on every render would be
// pointless work for a value that is really a build-time fact about this
// deployment (secure context or not).
const CLIPBOARD_READABLE = typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

/**
 * The one-shot "paste from clipboard" button JD's own Add Links dialog
 * offers beside its paste box.
 *
 * Feature-detected and hidden entirely rather than merely disabled: this
 * app's ordinary deployment is a bare http://192.168.x.x address, not a
 * secure context, where navigator.clipboard is undefined outright - a
 * disabled button sitting there forever would explain a browser
 * restriction nobody asked about, on a page that already takes an ordinary
 * Ctrl+V and a drop (see GlobalIntake, which reads event.clipboardData and
 * needs no permission at all). Only this explicit, one-shot button ever
 * touches the permission-gated Clipboard API - a document-level listener
 * silently reading the clipboard on its own would be a very different, and
 * much more alarming, feature.
 */
export function PasteFromClipboardButton({
  pkg = '',
  className = '',
}: {
  pkg?: string;
  className?: string;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);

  if (!CLIPBOARD_READABLE) return null;

  async function paste() {
    setBusy(true);
    try {
      const text = (await navigator.clipboard.readText()).trim();
      if (!text) {
        toast(t('collector.toastNone'), 'fail');
        return;
      }
      const created = await addLinks(text, pkg);
      toast(
        created.length ? t('collector.toastStaged', { n: created.length }) : t('collector.toastNone'),
        created.length ? 'ok' : 'fail',
      );
    } catch (e) {
      // The common case here is not a network error but the permission
      // prompt itself being dismissed - message(e) carries whichever the
      // browser actually gave, rather than this guessing which one it was.
      toast(t('list.failed', { error: message(e) }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Button
      kind="ghost"
      className={`px-2.5 text-xs ${className}`}
      icon={<IconClipboard width={14} height={14} />}
      onClick={() => void paste()}
      disabled={busy}
    >
      {t('intake.pasteButton')}
    </Button>
  );
}
