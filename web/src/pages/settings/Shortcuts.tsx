import { useEffect, useMemo, useState } from 'react';
import { Button, Card, EmptyState, Modal, SectionTitle } from '../../components/ui';
import { IconKeyboard } from '../../lib/icons';
import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { allCommands } from '../../lib/commands/allCommands';
import { formatShortcut } from '../../lib/commands/shortcuts';
import { effectiveShortcut, findConflict, useShortcutOverrides, type ShortcutOverrides } from '../../lib/commands/overrides';
import type { Command } from '../../lib/commands/types';

/**
 * The Shortcuts settings tab: every command that ships with a default
 * keyboard shortcut, grouped the same way the eventual command palette
 * groups them (`Command.group`), each showing the binding actually in
 * effect and a way to change or reset it.
 *
 * DELIBERATELY UNFILTERED. allCommands() (lib/commands/allCommands.ts) is
 * every command this build declares, across every surface, not
 * useCommands(surface, ctx) filtered down to "reachable from here right
 * now" - a shortcut bound to a Collector-only command has to stay visible
 * and rebindable while looking at this page from Settings, or it could
 * never be found again once the Collector tab that showed it is closed.
 *
 * PERSISTENCE. Rebindings live in one uistate field
 * (lib/commands/overrides.ts's SHORTCUT_OVERRIDES_FIELD,
 * "commands.shortcutOverrides") through the existing useUIState hook - the
 * same server-persisted, debounced, cross-browser store every other
 * remembered layout in this app already uses (uistate.ts's own doc
 * comment). The global keyboard dispatcher built in parallel this same wave
 * reads the identical field with overrides.ts's non-hook
 * readShortcutOverrides(), so a rebind here takes effect everywhere without
 * a second storage path to keep in sync.
 *
 * `group` IS NOT RELIABLY A TranslationKey. types.ts's own doc comment on
 * Command.group asks every command file to set it to one
 * ("commands.group.navigation"), and global.ts/queue.ts/language.ts/
 * settings.ts do - but downloads.ts and collector.ts (landed the same wave,
 * a different lane) set it to a bare English word ("Downloads",
 * "Collector") instead. groupLabel() below is tx.ts's own label() pattern
 * copied rather than reinvented: try it as a real key, fall back to the raw
 * string when it is not one - the same reason a page id with no
 * settings.nav.<id> entry still renders instead of crashing.
 */
function groupLabel(t: (key: TranslationKey) => string, group: string): string {
  return group in en ? t(group as TranslationKey) : group;
}

/** Groups a flat command list by `.group`, preserving first-seen order. */
function groupCommands(commands: Command[]): [string, Command[]][] {
  const order: string[] = [];
  const byGroup = new Map<string, Command[]>();
  for (const c of commands) {
    if (!byGroup.has(c.group)) {
      byGroup.set(c.group, []);
      order.push(c.group);
    }
    byGroup.get(c.group)!.push(c);
  }
  return order.map((g) => [g, byGroup.get(g)!]);
}

export function Shortcuts() {
  const { t } = useT();
  const [overrides, setOverrides] = useShortcutOverrides();
  const [captureFor, setCaptureFor] = useState<Command | null>(null);
  const [confirmResetAll, setConfirmResetAll] = useState(false);

  // Static per render - every command file's own array is a module-level
  // constant, so there is nothing here that changes between renders and
  // needs a dependency list.
  const commands = useMemo(() => allCommands().filter((c) => !!c.defaultShortcut), []);
  const groups = useMemo(() => groupCommands(commands), [commands]);
  const hasOverrides = Object.keys(overrides).length > 0;

  function saveOverride(id: string, combo: string) {
    setOverrides({ ...overrides, [id]: combo });
    setCaptureFor(null);
  }

  function resetOne(id: string) {
    if (!(id in overrides)) return;
    const next = { ...overrides };
    delete next[id];
    setOverrides(next);
  }

  function resetAll() {
    setOverrides({});
    setConfirmResetAll(false);
  }

  return (
    <div className="flex flex-col gap-6">
      <Card className="flex flex-wrap items-center justify-between gap-3">
        <SectionTitle hue={0}>{t('settings.nav.shortcuts')}</SectionTitle>
        <p className="max-w-2xl text-sm text-carbon-textSub">{t('settings.shortcuts.subtitle')}</p>
        <Button kind="ghost" disabled={!hasOverrides} onClick={() => setConfirmResetAll(true)}>
          {t('settings.shortcuts.resetAll')}
        </Button>
      </Card>

      {groups.length === 0 && (
        <EmptyState icon={<IconKeyboard width={28} height={28} />} title={t('settings.shortcuts.empty')} />
      )}

      {groups.map(([group, cmds], i) => (
        <div key={group} className="flex flex-col gap-3">
          <Card className="flex flex-col divide-y divide-carbon-border/60 p-0">
            <div className="p-5 pb-0">
              <SectionTitle hue={i + 1}>{groupLabel(t, group)}</SectionTitle>
            </div>
            {cmds.map((cmd) => (
              <ShortcutRow
                key={cmd.id}
                cmd={cmd}
                overrides={overrides}
                onChange={() => setCaptureFor(cmd)}
                onReset={() => resetOne(cmd.id)}
              />
            ))}
          </Card>
        </div>
      ))}

      {captureFor && (
        <CaptureModal
          cmd={captureFor}
          overrides={overrides}
          allCmds={commands}
          onSave={saveOverride}
          onCancel={() => setCaptureFor(null)}
        />
      )}

      {confirmResetAll && (
        <Modal
          title={t('settings.shortcuts.resetAllConfirmTitle')}
          onClose={() => setConfirmResetAll(false)}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setConfirmResetAll(false)}>
                {t('common.cancel')}
              </Button>
              <Button kind="danger" onClick={resetAll}>
                {t('settings.shortcuts.resetAllConfirm')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('settings.shortcuts.resetAllConfirmBody')}</p>
        </Modal>
      )}
    </div>
  );
}

function ShortcutRow({
  cmd,
  overrides,
  onChange,
  onReset,
}: {
  cmd: Command;
  overrides: ShortcutOverrides;
  onChange: () => void;
  onReset: () => void;
}) {
  const { t } = useT();
  const Icon = cmd.icon;
  const bound = effectiveShortcut(cmd, overrides);
  const isOverridden = cmd.id in overrides;

  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      {Icon && <Icon className="h-4 w-4 shrink-0 text-carbon-textMuted" />}
      <span className="min-w-0 flex-1 truncate text-sm text-carbon-text">{t(cmd.labelKey)}</span>
      <kbd className="glim-num shrink-0 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-[11px] font-medium text-carbon-textSub">
        {bound ? formatShortcut(bound) : ''}
      </kbd>
      <Button kind="secondary" className="shrink-0 px-2.5 py-1 text-xs" onClick={onChange}>
        {t('settings.shortcuts.change')}
      </Button>
      {isOverridden && (
        <Button kind="ghost" className="shrink-0 px-2.5 py-1 text-xs" onClick={onReset}>
          {t('settings.shortcuts.reset')}
        </Button>
      )}
    </div>
  );
}

// A combo is not complete while only one of these is held - the capture
// listener below waits for a real key on top of them before it has
// anything worth recording. Mirrors shortcuts.ts's own modifier handling,
// kept local rather than imported: this is the one place in the app that
// builds a combo FROM a live keydown rather than parsing an already-typed
// one, so it belongs beside the UI that captures it, in the exact string
// shape parseShortcut/formatShortcut (lib/commands/shortcuts.ts) already
// read and render.
const MODIFIER_ONLY_KEYS = new Set(['Control', 'Meta', 'Alt', 'Shift']);

function comboFromKeydown(e: KeyboardEvent): string | null {
  if (MODIFIER_ONLY_KEYS.has(e.key)) return null;
  const parts: string[] = [];
  if (e.ctrlKey || e.metaKey) parts.push('mod');
  if (e.altKey) parts.push('alt');
  if (e.shiftKey) parts.push('shift');
  const key = e.key.toLowerCase();
  parts.push(key === ' ' ? 'space' : key);
  return parts.join('+');
}

/**
 * The rebind flow: click "Change", then press a key combination. Escape
 * cancels rather than being recorded as a binding - Modal's own Escape
 * handling (ui.tsx) already means "close this" everywhere else in the app,
 * and a command bound to the same key a person expects to close the dialog
 * with would be unreachable the moment it mattered.
 *
 * The listener is attached in the capture phase on window and stops
 * propagation, so nothing else - a page's own keydown handler, the
 * browser's default action for the key just pressed - sees the keystroke
 * while this dialog is open. It detaches whenever this component unmounts,
 * whether that is a successful save (the parent drops captureFor) or Cancel.
 */
function CaptureModal({
  cmd,
  overrides,
  allCmds,
  onSave,
  onCancel,
}: {
  cmd: Command;
  overrides: ShortcutOverrides;
  allCmds: Command[];
  onSave: (id: string, combo: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT();
  const [error, setError] = useState('');

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === 'Escape') {
        onCancel();
        return;
      }
      const combo = comboFromKeydown(e);
      if (!combo) return; // only modifiers held so far - keep listening
      const conflict = findConflict(allCmds, overrides, combo, cmd.id);
      if (conflict) {
        setError(t('settings.shortcuts.conflict', { combo: formatShortcut(combo), command: t(conflict.labelKey) }));
        return;
      }
      onSave(cmd.id, combo);
    }
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [cmd, overrides, allCmds, onSave, onCancel, t]);

  return (
    <Modal
      title={t('settings.shortcuts.captureTitle', { name: t(cmd.labelKey) })}
      onClose={onCancel}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
        </>
      }
    >
      <p className="text-sm text-carbon-textSub">{t('settings.shortcuts.captureHint')}</p>
      {error && <p className="text-sm text-statusFail">{error}</p>}
    </Modal>
  );
}
