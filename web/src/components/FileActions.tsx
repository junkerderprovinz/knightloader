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
 * or video, or for a torrent of several files, the server found one among
 * them that is (torrentMedia). A torrent's name is its folder's, which says
 * nothing about its files.
 */
export function playsAsMedia(t: Task): boolean {
  if ((t.torrentFileCount ?? 0) > 1) return t.torrentMedia !== undefined;
  return playableAs(t.name) !== null;
}

/**
 * hasSomethingToPlay reports whether a task's file plays now. A running
 * download is streamed, and a finished one is on disk. A stopped HTTP
 * download's file already has its full size with holes where nothing has
 * arrived, so the server refuses it.
 */
export function hasSomethingToPlay(t: Task): boolean {
  return t.status === 'running' || t.status === 'done';
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
  // they get no entry, and neither does a file with nothing to play.
  const play: MenuItem[] =
    local && hasSomethingToPlay(task) && playsAsMedia(task)
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
