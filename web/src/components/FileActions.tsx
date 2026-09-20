// Context menu entries for a task's file. Opening natively and revealing in the
// file manager work only in the desktop build; in a browser they stay in the
// menu, disabled with the reason on the row.
import { type Task, taskFileURL } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { isDesktop, openNatively, revealInFolder } from '../lib/desktop';
import { type MenuGroup } from './ContextMenu';
import { IconApp, IconExternalLink, IconFolder } from '../lib/icons';

/**
 * reachable reports whether a task has a file on this machine: an unresolved
 * link still has its URL as its name, and a JD sidecar task's file lives on the
 * sidecar's disk. It mirrors internal/app's filesAreLocal; the server still
 * checks.
 */
export function reachable(t: Task): boolean {
  return t.resolver !== 'jd' && t.name !== '' && t.name !== t.url;
}

export function useFileMenu({ chosen, base, local }: { chosen: Task[]; base: string; local: boolean }): MenuGroup[] {
  const { t } = useT();
  const { toast } = useToast();

  if (chosen.length !== 1 || !reachable(chosen[0])) return [];
  const task = chosen[0];
  const desktopActionsAvailable = local && isDesktop();
  const reason = desktopActionsAvailable ? undefined : t('file.desktopOnly');
  const fail = (e: unknown) => toast(String(e instanceof Error ? e.message : e), 'fail');

  return [
    {
      id: 'file',
      items: [
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
