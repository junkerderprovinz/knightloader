import { useCallback } from 'react';
import { useUIState } from './uistate';
import { WATCH_FIELD, WATCH_SUPPORTED } from './clipboardWatch';

/**
 * useClipboardWatch is the clipboard watch's on/off state, shared by the
 * settings card, the collector's button and GlobalIntake, which runs the
 * poller. A UI state field rather than a server setting, since the watch runs
 * in one browser. It reads as off wherever the browser cannot read the
 * clipboard, whatever is stored.
 */
export function useClipboardWatch(): [boolean, (on: boolean) => void] {
  const [stored, setStored] = useUIState<boolean>(WATCH_FIELD, false);
  const set = useCallback(
    (on: boolean) => setStored(WATCH_SUPPORTED ? on : false),
    [setStored],
  );
  return [WATCH_SUPPORTED && stored, set];
}
