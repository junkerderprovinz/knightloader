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
 * Keeps the browser tab honest while the queue owes work: running/total and
 * percent complete drawn as a ring onto the favicon, speed and percent in the
 * title (build-plan.md section 8's Wave 9 note, 9C) - and restores both to
 * exactly what they were the instant nothing is owed any more, which is the
 * one thing this feature is not allowed to get wrong: a finished run leaving
 * a stale ring in the tab strip forever.
 *
 * Reads useTasks('') - the same live task stream Sidebar and the shell strip
 * already subscribe to - rather than opening any channel of its own.
 *
 * Mounted beside the router (app/router.tsx), not inside Layout where
 * CaptchaModal and GlobalIntake live: a captcha prompt or a paste target only
 * means something once a page is actually showing, but a tab's title and
 * icon are chrome, not page content, and have no page or instance scope to
 * agree with.
 */
export function TabIndicator() {
  const tasks = useTasks('');
  const activity = useMemo(() => measureActivity(tasks), [tasks]);

  const baseTitle = useRef<string | null>(null);
  const iconSnap = useRef<IconSnapshot | null>(null);
  // What the ring last actually drew, so a task update that leaves running
  // and percent unchanged (any other field on any task - name, position,
  // matchedRules...) does not repaint a canvas and re-decode a favicon for
  // nothing. '' never matches a real reading, which is what forces a redraw
  // the first time activity becomes non-idle after being idle.
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

  // The safety net for anything that is not the ordinary idle path above:
  // logging out unmounts everything AuthGate gated (Sidebar.tsx's own logout
  // button), and a dev hot-reload can drop this component without the app
  // ever going idle first. Either way the tab must not keep a stale ring
  // past this component actually being gone.
  useEffect(() => {
    return () => {
      if (baseTitle.current !== null) document.title = baseTitle.current;
      if (iconSnap.current !== null) restoreIcon(iconSnap.current);
    };
  }, []);

  return null;
}
