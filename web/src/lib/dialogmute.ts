// Which confirmation dialogs the user has silenced, as in JDownloader. The
// settings page lists them so they can be brought back. One array field in the
// shared UI state, not a field per dialog.
import { useCallback } from 'react';
import { useUIState } from './uistate';
import type { TranslationKey } from './i18n';

const FIELD = 'dialogs.muted';

// A stable empty default; a fresh [] per render would resubscribe forever.
const NONE: string[] = [];

/**
 * The confirmations that can be silenced, with their settings labels. Only
 * those whose action is reversible or already spelled out in the dialog;
 * confirmations that throw work away for good keep asking.
 */
export const MUTABLE_DIALOGS: { id: DialogId; label: TranslationKey }[] = [
  { id: 'remove', label: 'remove.title' },
  { id: 'cleanup', label: 'cleanup.menu' },
  { id: 'hardStop', label: 'queue.hardStopConfirmTitle' },
];

export type DialogId = 'remove' | 'cleanup' | 'hardStop';

export function useDialogMute() {
  const [muted, setMuted] = useUIState<string[]>(FIELD, NONE);

  const isMuted = useCallback((id: DialogId) => muted.includes(id), [muted]);
  const setMuted1 = useCallback(
    (id: DialogId, on: boolean) => {
      const has = muted.includes(id);
      if (on === has) return;
      setMuted(on ? [...muted, id] : muted.filter((x) => x !== id));
    },
    [muted, setMuted],
  );
  const restoreAll = useCallback(() => setMuted([]), [setMuted]);

  return { muted, isMuted, setMuted: setMuted1, restoreAll };
}
