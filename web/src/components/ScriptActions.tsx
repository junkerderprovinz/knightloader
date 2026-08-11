// The manual-invocation half of Wave 11B: census row "Toolbar / Main Menu /
// Contextmenu / Traymenu Button Pressed" - "turning scripts into manual
// commands on... both table context menus". The census's own blocker note on
// that row names PackageActions.tsx by name as one of the places JD's own
// placement maps onto in this app ("the UI menus are hard-coded React
// (web/src/components/Sidebar.tsx, PackageActions.tsx)"), which is why this
// hook is wired in beside PackageActions in Downloads.tsx/Collector.tsx
// rather than a new, easy-to-miss location - see this wave's own report for
// the full placement reasoning (both table context menus, not a third menu
// system or a new toolbar).
//
// Shaped exactly like Archives.tsx's useArchiveMenu and FileActions.tsx's
// useFileMenu: chosen tasks in, a MenuGroup[] out, empty when there is
// nothing to offer - Panel in ContextMenu.tsx already drops an empty group,
// so an empty return here is the correct "nothing to show", not a special
// case the caller has to know about.
//
// Gated to exactly one chosen task, matching useFileMenu's own gate and for
// a harder reason than that one: internal/script's execution model
// (sandbox.go's execCtx/taskGlobal) closes every task-scoped closure over
// ONE taskID chosen in Go before a script sees a line of code, and
// runScript (lib/scripts.ts) is typed to match - there is no "run against
// several tasks" call this hook could make even if it offered one.
import { useCallback, useEffect, useState } from 'react';
import type { Task } from '../lib/api';
import { fetchScripts, runScript, type Script } from '../lib/scripts';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import type { MenuGroup } from './ContextMenu';
import { IconCode } from '../lib/icons';

/**
 * Same PENDING/useCx shape as Scripts.tsx and Schedule.tsx - see either's own
 * doc comment. Kept as its own small table rather than importing Scripts.tsx's
 * (which is not exported) because this file must not depend on the settings
 * page: it is reached from Downloads.tsx and Collector.tsx, neither of which
 * should have to pull in a whole settings sub-page to render a context menu.
 */
const PENDING = {
  'task.runScript': 'Run script',
  'task.runScriptUnnamed': 'Untitled script',
  'task.runScriptDone': 'Ran “{name}”',
  'task.runScriptFailed': '“{name}” failed: {error}',
} as const;

type PendingKey = keyof typeof PENDING;
type Cx = (key: PendingKey, vars?: Record<string, string | number>) => string;

function useCx(): Cx {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

/**
 * useScriptMenu manages its own fetch of the script list rather than taking
 * it as a prop. The menu only opens on a right-click, the list rarely
 * changes and is cheap to ask for, and every OTHER menu-contributing hook in
 * this tree (useArchiveMenu, useFileMenu) is handed data the PAGE already
 * had to fetch for some other reason (extraction jobs, the task list
 * itself) - scripts are not otherwise needed anywhere on Downloads.tsx or
 * Collector.tsx, so fetching them here keeps that fetch out of two page
 * components that would otherwise carry it for no reason of their own.
 */
function useScripts(): Script[] {
  const [scripts, setScripts] = useState<Script[]>([]);
  useEffect(() => {
    let alive = true;
    fetchScripts()
      .then((list) => alive && setScripts(list))
      .catch(() => {
        // A genuine network failure: the menu simply offers nothing,
        // exactly like an account-less Downloads page offers no archive
        // actions rather than showing a broken control.
      });
    return () => {
      alive = false;
    };
  }, []);
  return scripts;
}

/**
 * `base` names the instance the selection belongs to, matching every other
 * selection-scoped action in this file's sibling hooks (queueMove,
 * setPriority in PackageActions.tsx). Accepted but not yet threaded through
 * to runScript - whether running a script against a FEDERATED peer's own
 * script store is even meaningful, or whether scripts are inherently
 * local-instance-only like FileActions' reveal/open, is a real open question
 * for whoever lands the backend (11A) or the federation surface, not one to
 * settle silently here by picking a direction. Kept in the signature so the
 * call sites in Downloads.tsx/Collector.tsx do not need a second edit the day
 * that question is answered - see this wave's own report.
 */
export function useScriptMenu({ chosen, base: _base }: { chosen: Task[]; base: string }): MenuGroup[] {
  const cx = useCx();
  const { toast } = useToast();
  const scripts = useScripts();

  if (chosen.length !== 1) return [];
  const task = chosen[0];
  const runnable = scripts.filter((s) => s.enabled && s.trigger === 'manual');
  if (runnable.length === 0) return [];

  const nameOf = (s: Script) => s.name || cx('task.runScriptUnnamed');

  return [
    {
      id: 'scripts',
      items: [
        {
          id: 'run-script',
          label: cx('task.runScript'),
          icon: <IconCode width={14} height={14} />,
          submenu: [
            {
              id: 'available',
              items: runnable.map((s) => ({
                id: s.id,
                label: nameOf(s),
                onSelect: () => {
                  void runScript(s.id, task.id).then(
                    (result) => {
                      toast(
                        result.ok
                          ? cx('task.runScriptDone', { name: nameOf(s) })
                          : cx('task.runScriptFailed', { name: nameOf(s), error: result.error ?? '' }),
                        result.ok ? 'ok' : 'fail',
                        result.ok ? 'action-done' : 'action-failed',
                      );
                    },
                    (e: unknown) => {
                      toast(
                        cx('task.runScriptFailed', { name: nameOf(s), error: e instanceof Error ? e.message : String(e) }),
                        'fail',
                        'action-failed',
                      );
                    },
                  );
                },
              })),
            },
          ],
        },
      ],
    },
  ];
}
