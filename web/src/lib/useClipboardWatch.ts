import { useCallback } from 'react';
import { useUIState } from './uistate';
import { WATCH_FIELD, WATCH_SUPPORTED } from './clipboardWatch';

/**
 * The clipboard watch's on/off state, shared by the three places that touch
 * it: the Linkeingang card in the settings, the collector's own button, and
 * GlobalIntake, which is what actually runs the poller.
 *
 * A remembered UI field rather than a server setting, because the thing it
 * switches only exists in one browser tab. A watch stored on the server would
 * read "on" in a second browser that has no clipboard permission at all, and
 * "on" on a phone that cannot run it - it is a property of the client, so it
 * is remembered by the client (see uistate.ts).
 *
 * Reported as off wherever the browser cannot read the clipboard, whatever is
 * stored: an instance moved from https:// to a plain LAN address would
 * otherwise show a switch still standing at "on" over a watch that has
 * silently not run since.
 */
export function useClipboardWatch(): [boolean, (on: boolean) => void] {
  const [stored, setStored] = useUIState<boolean>(WATCH_FIELD, false);
  const set = useCallback(
    (on: boolean) => setStored(WATCH_SUPPORTED ? on : false),
    [setStored],
  );
  return [WATCH_SUPPORTED && stored, set];
}
