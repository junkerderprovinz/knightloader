// Context menu entries for a task's file. Opening natively and revealing in the
// file manager work only in the desktop build; in a browser they stay in the
// menu, disabled with the reason on the row.
import { type Task, taskFileURL } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { isDesktop, openNatively, revealInFolder } from '../lib/desktop';
import { type MenuGroup, type MenuItem } from './ContextMenu';
import { IconApp, IconExternalLink, IconFolder, IconPlayFile } from '../lib/icons';
import { playableAs } from './taskdetail/playable';

/**
 * reachable reports whether a task has a file on this machine: an unresolved
 * link still has its URL as its name, and a JD sidecar task's file lives on the
 * sidecar's disk. It mirrors internal/app's filesAreLocal; the server still
 * checks.
 */
export function reachable(t: Task): boolean {
  return t.resolver !== 'jd' && t.name !== '' && t.name !== t.url;
}

/**
 * playsAsMedia reports whether Play is offered for a task: its file is audio
 * or video, or it is a torrent of several files, of which the server plays the
 * largest selected one that is.
 */
export function playsAsMedia(t: Task): boolean {
  return playableAs(t.name) !== null || (t.torrentFileCount ?? 0) > 1;
}

export function useFileMenu({ chosen, base, local }: { chosen: Task[]; base: string; local: boolean }): MenuGroup[] {
  const { t } = useT();
  const { toast } = useToast();

  if (chosen.length !== 1 || !reachable(chosen[0])) return [];
  const task = chosen[0];
  const desktopActionsAvailable = local && isDesktop();
  const reason = desktopActionsAvailable ? undefined : t('file.desktopOnly');
  const fail = (e: unknown) => toast(String(e instanceof Error ? e.message : e), 'fail');

  // The file opens in a tab of its own, where the browser's player streams it.
  // While the download runs, the server fetches the part being played first.
  // A peer's files pass through a proxy that neither streams nor seeks, so
  // they get no entry, and neither does a file with nothing to play yet.
  const started = task.status === 'running' || task.status === 'done' || task.loaded > 0;
  const play: MenuItem[] =
    local && started && playsAsMedia(task)
      ? [
          {
            id: 'play',
            label: t('file.play'),
            icon: <IconPlayFile width={14} height={14} />,
            onSelect: () => window.open(taskFileURL(task.id, base), '_blank', 'noopener'),
          },
        ]
      : [];

  return [
    {
      id: 'file',
      items: [
        ...play,
        {
          id: 'open',
          label: t('file.open'),
          icon: <IconExternalLink width={14} height={14} />,
          onSelect: () => window.open(taskFileURL(task.id, base), '_blank', 'noopener'),
        },
        {
          id: 'openNatively',
          label: t('file.openNatively'),
          icon: <IconApp width={14} height={14} />,
          disabled: !desktopActionsAvailable,
          detail: reason,
          onSelect: () => void openNatively(task.id).catch(fail),
        },
        {
          id: 'revealInFolder',
          label: t('file.revealInFolder'),
          icon: <IconFolder width={14} height={14} />,
          disabled: !desktopActionsAvailable,
          detail: reason,
          onSelect: () => void revealInFolder(task.id).catch(fail),
        },
      ],
    },
  ];
}
