// Reaching a task's own file: view it, and on the desktop build only, reveal
// it in the OS file manager or hand it to whatever application the OS opens
// that kind of file with (package 20). A browser has no such capability at
// all - see lib/desktop.ts for how the two builds are told apart - and the
// two desktop-only entries stay in the menu either way, disabled with the
// reason written on the row, rather than vanishing as if the feature had
// never been built.
//
// The menu entries are a GROUP handed to the existing context menu, the same
// pattern Archives.tsx already uses for its own verbs.
import { type Task, taskFileURL } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { isDesktop, openNatively, revealInFolder } from '../lib/desktop';
import { type MenuGroup } from './ContextMenu';
import { IconApp, IconExternalLink, IconFolder } from '../lib/icons';

/**
 * reachable is whether a task has bytes worth reaching at all: a link still
 * sitting in the collector has never resolved a real name (Name still equals
 * URL), and a task fetched through the JD sidecar lives on that process's own
 * filesystem, not this one - the frontend mirrors internal/app's
 * filesAreLocal here only to keep the menu from offering what the route
 * would refuse right back; the server's own check is the one that matters.
 */
function reachable(t: Task): boolean {
  return t.resolver !== 'jd' && t.name !== '' && t.name !== t.url;
}

export function useFileMenu({ chosen, base, local }: { chosen: Task[]; base: string; local: boolean }): MenuGroup[] {
  const { t } = useT();
  const { toast } = useToast();

  if (chosen.length !== 1 || !reachable(chosen[0])) return [];
  const task = chosen[0];
  const desktopActionsAvailable = local && isDesktop();
  const reason = desktopActionsAvailable ? undefined : t('file.desktopOnly');
  // No explicit kind: an untyped 'fail' call already lands on 'action-failed'
  // (see toast.tsx's KIND_BY_TONE), which is exactly the generic "an action
  // the user asked for did not work" bucket this belongs in.
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
