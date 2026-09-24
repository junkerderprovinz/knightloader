import { useEffect, useMemo, useRef } from 'react';
import { useTasks } from '../lib/useTasks';
import { applyIcon, captureIcon, measureActivity, renderRingFavicon, restoreIcon, type IconSnapshot } from '../lib/tabIndicator';

/**
 * TabIndicator draws queue progress as a ring on the favicon while work is
 * owed and restores the icon once it is not. It is mounted beside the router,
 * since the tab is chrome rather than page content.
 */
export function TabIndicator() {
  const tasks = useTasks('');
  const activity = useMemo(() => measureActivity(tasks), [tasks]);

  const iconSnap = useRef<IconSnapshot | null>(null);
  // What the ring last drew, so unrelated task updates skip the redraw. The
  // empty string forces one when activity resumes.
  const ringKey = useRef('');

  useEffect(() => {
    if (iconSnap.current === null) iconSnap.current = captureIcon();
    const snap = iconSnap.current;

    if (activity.total === 0) {
      if (ringKey.current !== '') {
        restoreIcon(snap);
        ringKey.current = '';
      }
      return;
    }

    const key = `${activity.running}|${activity.percent}`;
    if (key !== ringKey.current) {
      ringKey.current = key;
      applyIcon(renderRingFavicon(activity));
    }
  }, [activity]);

  // Logging out or a hot reload can unmount this without the queue going idle.
  useEffect(() => {
    return () => {
      if (iconSnap.current !== null) restoreIcon(iconSnap.current);
    };
  }, []);

  return null;
}
