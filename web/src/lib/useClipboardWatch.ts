import { useCallback } from 'react';
import { useUIState } from './uistate';
import { TARGET_FIELD, WATCH_FIELD, WATCH_SUPPORTED } from './clipboardWatch';

/**
 * useClipboardWatch is the clipboard watch's on/off state, shared by the
 * settings card, the collector's button and GlobalIntake, which runs the
 * poller. A UI state field rather than a server setting, since the watch runs
 * in one browser, or in the desktop app that reads it. It reads as off
 * wherever the page cannot watch the clipboard, whatever is stored.
 */
export function useClipboardWatch(): [boolean, (on: boolean) => void] {
  const [stored, setStored] = useUIState<boolean>(WATCH_FIELD, false);
  const set = useCallback(
    (on: boolean) => setStored(WATCH_SUPPORTED ? on : false),
    [setStored],
  );
  return [WATCH_SUPPORTED && stored, set];
}

/** useClipboardWatchTarget is where copied links go: '' for this instance,
 *  otherwise a peer's name as /api/instances lists it. */
export function useClipboardWatchTarget(): [string, (target: string) => void] {
  return useUIState<string>(TARGET_FIELD, '');
}
