// Which confirmation dialogs somebody has told this instance to stop showing.
//
// JDownloader lets you silence a confirm from inside the confirm itself, and
// jdp asked for the same (2026-09-06: "bei solchen fenstern soll es eine option
// geben sie nicht mehr anzeigen. wie in JD. in den einstellungen muss es aber
// einen bereich geben diese optoin wieder rückgängig zu machen"). The second
// half is the part that makes the first half safe: a dialog you can silence
// and never bring back is a dialog you silence once and regret quietly.
//
// One array field in the shared UI-state document (lib/uistate.ts), not a field
// per dialog: a field per dialog is how this file would have to be edited for
// the fourth confirmation, and the fifth, forever.
import { useCallback } from 'react';
import { useUIState } from './uistate';
import type { TranslationKey } from './i18n';

const FIELD = 'dialogs.muted';

// A stable empty default - useUIState leaves its fallback out of the effect's
// dependencies, and a fresh [] per render would resubscribe forever.
const NONE: string[] = [];

/**
 * Every confirmation this build lets somebody silence, with the label the
 * settings list names it by.
 *
 * Deliberately NOT every dialog in the app. A confirmation is mutable when the
 * action behind it is reversible or merely tedious to repeat; the ones that
 * throw work away for good keep asking. That is why "remove with files" is in
 * this list and a password reset is not: the first deletes what the list says
 * it will delete, in a dialog that has already told you the byte count.
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
