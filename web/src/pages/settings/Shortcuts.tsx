import { useEffect, useMemo, useState } from 'react';
import { Button, Card, EmptyState, Modal, SectionTitle } from '../../components/ui';
import { IconKeyboard } from '../../lib/icons';
import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { allCommands } from '../../lib/commands/allCommands';
import { formatShortcut } from '../../lib/commands/shortcuts';
import { effectiveShortcut, findConflict, useShortcutOverrides, type ShortcutOverrides } from '../../lib/commands/overrides';
import type { Command } from '../../lib/commands/types';
import { ListKeysCard } from './shortcuts/ListKeys';

/**
 * The Shortcuts tab lists every command with a default shortcut, grouped by
 * `Command.group`, with the binding in effect and a way to change or reset it.
 * It uses allCommands() rather than a surface filter, so a command from a
 * closed tab stays rebindable. Rebindings live in the uistate field that the
 * keyboard dispatcher also reads (lib/commands/overrides.ts).
 *
 * groupLabel treats `group` as a catalogue key when it is one and falls back to
 * the raw string, since some command files set a plain word.
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

  // The command arrays are module-level constants.
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
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-1">
          <SectionTitle hint={t('settings.shortcuts.subtitle')}>{t('settings.nav.shortcuts')}</SectionTitle>
        </div>
        <Button kind="secondary" className="shrink-0" disabled={!hasOverrides} onClick={() => setConfirmResetAll(true)}>
          {t('settings.shortcuts.resetAll')}
        </Button>
      </Card>

      {groups.length === 0 && (
        <EmptyState icon={<IconKeyboard width={28} height={28} />} title={t('settings.shortcuts.empty')} />
      )}

      {groups.map(([group, cmds], i) => (
        <div key={group} className="flex flex-col gap-3">
          {/* The title sits outside the divide-y flow, so no divider lands
              under the badge. */}
          <Card hue={i + 1} padding="none" className="flex flex-col">
            <div className="p-5 pb-0">
              <SectionTitle>{groupLabel(t, group)}</SectionTitle>
            </div>
            <div className="flex flex-col divide-y divide-carbon-border/60">
              {cmds.map((cmd) => (
                <ShortcutRow
                  key={cmd.id}
                  cmd={cmd}
                  overrides={overrides}
                  onChange={() => setCaptureFor(cmd)}
                  onReset={() => resetOne(cmd.id)}
                  hue={i + 1}
                />
              ))}
            </div>
          </Card>
        </div>
      ))}

      {/* Not a command group: these keys cannot be rebound. Its hue carries on
          from the last group. */}
      <ListKeysCard hue={groups.length + 1} />

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
              <Button kind="ghost" onClick={resetAll}>
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
  hue,
}: {
  cmd: Command;
  overrides: ShortcutOverrides;
  onChange: () => void;
  onReset: () => void;
  /** The group's hue, shared by every Change button in it. */
  hue: number;
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
        {bound ? formatShortcut(bound, t) : ''}
      </kbd>
      {/* Reset, then Change, which moves the row on. JSX order, so the pair
          mirrors in right-to-left languages. */}
      {isOverridden && (
        <Button kind="ghost" className="shrink-0 px-2.5 py-1 text-xs" onClick={onReset}>
          {t('settings.shortcuts.reset')}
        </Button>
      )}
      <Button kind="secondary" hue={hue} className="shrink-0 px-2.5 py-1 text-xs" onClick={onChange}>
        {t('settings.shortcuts.change')}
      </Button>
    </div>
  );
}

// A combo is incomplete while only modifiers are held. Kept here rather than
// in lib/commands/shortcuts.ts because this is the one place that builds a
// combo from a live keydown, in the shape parseShortcut reads.
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
 * CaptureModal records the next key combination for a command. Escape cancels
 * instead of being recorded, since it closes dialogs everywhere else. The
 * listener runs in the capture phase on window and stops propagation, so
 * nothing else sees the keystroke while the dialog is open.
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
        setError(t('settings.shortcuts.conflict', { combo: formatShortcut(combo, t), command: t(conflict.labelKey) }));
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
