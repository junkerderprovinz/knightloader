import { useEffect, useState } from 'react';
import { Field, FieldGroup, IconBadge, NumberInput, TextArea, TextInput, ToggleRow } from '../../../components/ui';
import type { IdleCommandSpec } from '../../../lib/api';
import {
  DEFAULT_PARALLEL,
  ENV_NAMES,
  MAX_PARALLEL,
  TIMEOUT,
  clampParallel,
  isStored,
  storedArgs,
  type EventProgramRow,
  type EventProgramStatus,
} from '../../../lib/eventprograms';
import type { Placeholder } from '../../../lib/eventtargets';
import { IconTrash } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { useTriggerLabel } from '../../../lib/triggers';
import { TargetEvents } from '../eventtargets/TargetEvents';
import { Placeholders } from '../eventtargets/TargetRow';
import { ProgramHealth } from './ProgramHealth';

/**
 * ProgramRow shows one event program, collapsed to its name and events,
 * expanded to the whole row. A stored program and its arguments come back
 * masked, so their boxes stay empty with a note until something is typed,
 * which then replaces the stored value, as on the idle card.
 *
 * The arguments are committed on blur: every change otherwise drops a blank
 * line as it is typed, and a new line could never be started.
 */
export function ProgramRow({
  row,
  index,
  last,
  open,
  triggers,
  placeholders,
  status,
  deployment,
  onToggle,
  onChange,
  onRemove,
}: {
  row: EventProgramRow;
  index: number;
  last: boolean;
  open: boolean;
  /** From GET /api/scripts/triggers. */
  triggers: string[];
  /** From GET /api/eventprograms/placeholders. */
  placeholders: Placeholder[];
  status?: EventProgramStatus;
  deployment: string;
  onToggle: () => void;
  onChange: (next: EventProgramRow) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const triggerLabel = useTriggerLabel();

  const command = row.command;
  const programStored = isStored(command.program);
  const argsStored = storedArgs(command.args);
  const shownArgs = argsStored ? '' : (command.args ?? []).join('\n');
  const [argsText, setArgsText] = useState(shownArgs);
  // The save answer replaces the draft, so follow it.
  useEffect(() => setArgsText(shownArgs), [shownArgs]);

  // Every write spreads, because the command is replaced as a whole.
  const setCommand = (fields: Partial<IdleCommandSpec>) => onChange({ ...row, command: { ...command, ...fields } });

  const commitArgs = () => {
    // Unchanged, so a masked list that was only looked at stays stored.
    if (argsText === shownArgs) return;
    // Blank lines are dropped, as CommandSpec.Sanitize does.
    setCommand({ args: argsText.split('\n').filter((a) => a.trim() !== '') });
  };

  const picked = row.triggers ?? [];
  const programName = programStored ? t('settings.downloads.idleCommandStoredShort') : command.program;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 items-center gap-3 text-start">
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">
              {row.name.trim() || <span className="text-carbon-textMuted">{t('settings.eventPrograms.name')}</span>}
            </span>
            <span className="block truncate text-[11px] text-carbon-textMuted">
              {picked.length > 0 ? picked.map(triggerLabel).join(', ') : t('settings.eventPrograms.eventsNone')}
            </span>
          </span>
          {/* Only the off state is marked. */}
          {!row.enabled && (
            <span className="hidden shrink-0 text-[11px] uppercase tracking-wider text-carbon-textMuted sm:block">
              {t('settings.modules.off')}
            </span>
          )}
        </button>
        <IconBadge
          // A lone glyph takes half its 32px badge.
          icon={<IconTrash width={16} height={16} />}
          hue={index}
          title={t('settings.eventPrograms.remove')}
          aria-label={t('settings.eventPrograms.remove')}
          // No blur, so the arguments are not committed in front of the removal.
          onMouseDown={(e) => e.preventDefault()}
          onClick={onRemove}
        />
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <ToggleRow
            label={t('settings.eventPrograms.enabled')}
            hint={t('settings.eventPrograms.enabledHint')}
            checked={row.enabled}
            onChange={(v) => onChange({ ...row, enabled: v })}
            hue={index}
          />

          <Field label={t('settings.eventPrograms.name')} hint={t('settings.eventPrograms.nameHint')}>
            <TextInput value={row.name} onChange={(e) => onChange({ ...row, name: e.target.value })} />
          </Field>

          <Field label={t('settings.eventPrograms.program')} hint={t('settings.eventPrograms.programHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={programStored ? '' : command.program}
              placeholder={programStored ? t('settings.downloads.idleCommandStored') : '/home/you/bin/after-download.sh'}
              onChange={(e) => setCommand({ program: e.target.value })}
            />
          </Field>

          <Field label={t('settings.eventPrograms.args')} hint={t('settings.eventPrograms.argsHint')}>
            <TextArea
              dir="ltr"
              spellCheck={false}
              rows={3}
              value={argsText}
              placeholder={argsStored ? t('settings.downloads.idleCommandStored') : '%%file%%'}
              onChange={(e) => setArgsText(e.target.value)}
              onBlur={commitArgs}
            />
          </Field>

          <Placeholders list={placeholders} picked={picked} hint={t('settings.eventPrograms.placeholdersHint')} />

          <FieldGroup label={t('settings.eventPrograms.env')} hint={t('settings.eventPrograms.envHint')}>
            <div className="flex flex-wrap gap-1.5">
              {ENV_NAMES.map((name) => (
                <code
                  key={name}
                  dir="ltr"
                  className="rounded-[var(--radius-pill)] bg-carbon-surface2 px-1.5 py-0.5 text-[11px] text-carbon-textSub"
                >
                  {name}
                </code>
              ))}
            </div>
          </FieldGroup>

          <TargetEvents
            triggers={triggers}
            picked={picked}
            hue={index}
            hint={t('settings.eventPrograms.eventsHint')}
            noneText={t('settings.eventPrograms.eventsNone')}
            burstHint={t('settings.eventPrograms.eventsBurst')}
            onChange={(next) => {
              const out = { ...row };
              if (next.length === 0) delete out.triggers;
              else out.triggers = next;
              onChange(out);
            }}
          />

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t('settings.eventPrograms.timeout')} hint={t('settings.eventPrograms.timeoutHint')}>
              <NumberInput
                dir="ltr"
                value={command.timeoutSeconds || TIMEOUT.fallback}
                min={TIMEOUT.lo}
                max={TIMEOUT.hi}
                step={1}
                onValue={(v) => setCommand({ timeoutSeconds: v })}
              />
            </Field>
            <Field label={t('settings.eventPrograms.parallel')} hint={t('settings.eventPrograms.parallelHint')}>
              <NumberInput
                dir="ltr"
                value={row.parallel > 0 ? row.parallel : DEFAULT_PARALLEL}
                min={1}
                max={MAX_PARALLEL}
                step={1}
                onValue={(v) => onChange({ ...row, parallel: clampParallel(v) })}
              />
            </Field>
          </div>

          <ProgramHealth
            status={status}
            program={programName}
            deployment={deployment}
            timeoutSeconds={command.timeoutSeconds || TIMEOUT.fallback}
          />
        </div>
      )}
    </li>
  );
}
