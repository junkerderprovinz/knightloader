import { useEffect, useMemo, useRef } from 'react';
import { useTasks } from '../lib/useTasks';
import {
  applyIcon,
  captureIcon,
  formatTabTitle,
  measureActivity,
  renderRingFavicon,
  restoreIcon,
  type IconSnapshot,
} from '../lib/tabIndicator';

/**
 * TabIndicator draws queue progress as a ring on the favicon and puts speed and
 * percent in the title while work is owed, and restores both once it is not.
 * It is mounted beside the router, since the tab is chrome rather than page
 * content.
 */
export function TabIndicator() {
  const tasks = useTasks('');
  const activity = useMemo(() => measureActivity(tasks), [tasks]);

  const baseTitle = useRef<string | null>(null);
  const iconSnap = useRef<IconSnapshot | null>(null);
  // What the ring last drew, so unrelated task updates skip the redraw. The
  // empty string forces one when activity resumes.
  const ringKey = useRef('');

  useEffect(() => {
    if (baseTitle.current === null) baseTitle.current = document.title;
    if (iconSnap.current === null) iconSnap.current = captureIcon();
    const base = baseTitle.current;
    const snap = iconSnap.current;

    if (activity.total === 0) {
      if (document.title !== base) document.title = base;
      if (ringKey.current !== '') {
        restoreIcon(snap);
        ringKey.current = '';
      }
      return;
    }

    document.title = formatTabTitle(activity, base);

    const key = `${activity.running}|${activity.percent}`;
    if (key !== ringKey.current) {
      ringKey.current = key;
      applyIcon(renderRingFavicon(activity));
    }
  }, [activity]);

  // Logging out or a hot reload can unmount this without the queue going idle.
  useEffect(() => {
    return () => {
      if (baseTitle.current !== null) document.title = baseTitle.current;
      if (iconSnap.current !== null) restoreIcon(iconSnap.current);
    };
  }, []);

  return null;
}
