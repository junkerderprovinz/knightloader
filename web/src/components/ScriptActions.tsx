// Runs manual scripts from both list context menus. Like useFileMenu it offers
// entries for exactly one task, because a script runs against a single task id.
import { useEffect, useState } from 'react';
import type { Task } from '../lib/api';
import { fetchScripts, runScript, type Script } from '../lib/scripts';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import type { MenuGroup } from './ContextMenu';
import { IconCode } from '../lib/icons';

// The pages have no other use for the script list, so the hook fetches it.
function useScripts(): Script[] {
  const [scripts, setScripts] = useState<Script[]>([]);
  useEffect(() => {
    let alive = true;
    fetchScripts()
      .then((list) => alive && setScripts(list))
      .catch(() => {
        // The menu then offers no scripts.
      });
    return () => {
      alive = false;
    };
  }, []);
  return scripts;
}

/**
 * useScriptMenu offers the enabled manual scripts for one chosen task. `base`
 * is not passed on, since runScript only targets the local instance.
 */
export function useScriptMenu({ chosen, base: _base }: { chosen: Task[]; base: string }): MenuGroup[] {
  const { t } = useT();
  const { toast } = useToast();
  const scripts = useScripts();

  if (chosen.length !== 1) return [];
  const task = chosen[0];
  const runnable = scripts.filter((s) => s.enabled && s.trigger === 'manual');
  if (runnable.length === 0) return [];

  const nameOf = (s: Script) => s.name || t('task.runScriptUnnamed');

  return [
    {
      id: 'scripts',
      items: [
        {
          id: 'run-script',
          label: t('task.runScript'),
          icon: <IconCode width={14} height={14} />,
          submenu: [
            {
              id: 'available',
              items: runnable.map((s) => ({
                id: s.id,
                label: nameOf(s),
                icon: <IconCode />,
                onSelect: () => {
                  void runScript(s.id, task.id).then(
                    (result) => {
                      toast(
                        result.ok
                          ? t('task.runScriptDone', { name: nameOf(s) })
                          : t('task.runScriptFailed', { name: nameOf(s), error: result.error ?? '' }),
                        result.ok ? 'ok' : 'fail',
                        result.ok ? 'action-done' : 'action-failed',
                      );
                    },
                    (e: unknown) => {
                      toast(
                        t('task.runScriptFailed', { name: nameOf(s), error: e instanceof Error ? e.message : String(e) }),
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
