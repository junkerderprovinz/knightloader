import { useState } from 'react';
import { addLinks } from '../lib/api';
import { message } from '../lib/intake';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { Button } from './ui';
import { IconClipboard } from '../lib/icons';

const CLIPBOARD_READABLE = typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

/**
 * PasteFromClipboardButton stages the clipboard's links in one click. It is
 * hidden outside a secure context, where navigator.clipboard does not exist;
 * Ctrl+V and drop still work there through GlobalIntake.
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
      // Usually a dismissed permission prompt rather than a network error.
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
