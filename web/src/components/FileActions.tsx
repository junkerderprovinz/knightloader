// Context menu entries for a task's file and its link. Opening natively and
// opening the folder work only in the desktop build, which is the only place
// they are offered: a browser has no file manager to hand them to.
import { type Task, taskFileURL } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { isDesktop, openFolder, openNatively, openURL } from '../lib/desktop';
import { type MenuGroup, type MenuItem } from './ContextMenu';
import { IconApp, IconBrowser, IconCopy, IconExternalLink, IconFolderOpen, IconPlayFile } from '../lib/icons';
import { playableTask } from './taskdetail/playable';

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
  return playableTask(t) !== null;
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

/** webLink is a link a browser can open, which a magnet or an uploaded file is not. */
const webLink = (url: string): boolean => /^https?:\/\//i.test(url);

/**
 * useFileMenu is what the menu offers for the files and links of a selection,
 * the way JDownloader's right-click does: the file itself for one link that
 * has one, the folder it downloads into, and the links. files is false in the
 * collector, where nothing has been downloaded yet.
 */
export function useFileMenu({
  chosen,
  base,
  local,
  files = true,
}: {
  chosen: Task[];
  base: string;
  local: boolean;
  files?: boolean;
}): MenuGroup[] {
  const { t } = useT();
  const { toast } = useToast();
  if (chosen.length === 0) return [];
  const desktop = local && isDesktop();
  const fail = (e: unknown) => toast(String(e instanceof Error ? e.message : e), 'fail');

  const links: MenuItem[] = [
    {
      id: 'copyLinks',
      label: t('menu.copyLinks'),
      icon: <IconCopy width={14} height={14} />,
      onSelect: () =>
        void navigator.clipboard
          .writeText(chosen.map((x) => x.url).join('\n'))
          .then(() => toast(t('menu.linksCopied', { n: chosen.length }), 'ok'))
          .catch(fail),
    },
  ];
  if (chosen.length === 1 && webLink(chosen[0].url)) {
    const url = chosen[0].url;
    links.push({
      id: 'openInBrowser',
      label: t('menu.openInBrowser'),
      icon: <IconBrowser width={14} height={14} />,
      onSelect: () => (desktop ? void openURL(url).catch(fail) : void window.open(url, '_blank', 'noopener')),
    });
  }
  const folder: MenuItem[] = desktop
    ? [
        {
          id: 'openFolder',
          label: t('menu.openFolder'),
          icon: <IconFolderOpen width={14} height={14} />,
          onSelect: () => void openFolder(chosen[0].id).catch(fail),
        },
      ]
    : [];
  const linkGroups: MenuGroup[] = [{ id: 'links', items: [...folder, ...links] }];

  if (!files || chosen.length !== 1 || !reachable(chosen[0])) return linkGroups;
  const task = chosen[0];

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
        ...(desktop
          ? [
              {
                id: 'openNatively',
                label: t('file.openNatively'),
                icon: <IconApp width={14} height={14} />,
                onSelect: () => void openNatively(task.id).catch(fail),
              },
            ]
          : []),
      ],
    },
    ...linkGroups,
  ];
}
