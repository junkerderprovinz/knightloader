import { FieldGroup, IconBadge, NumberInput, TextInput, ToggleRow, useTooltip } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { IconTrash } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import {
  DEFAULT_TIMEOUT_SECONDS,
  MAX_ATTEMPTS,
  MAX_TIMEOUT_SECONDS,
  addressOf,
  hostOf,
  type Channel,
  type ChannelOn,
  type EventTargetStatus,
  type Placeholder,
} from '../../../lib/eventtargets';
import { RowRefusal, SettingRow, Sheet } from '../controls';
import { ChannelFields } from './ChannelFields';
import { useKindName } from './kinds';
import { TargetEvents } from './TargetEvents';
import { TargetHealth } from './TargetHealth';
import { TargetProbe } from './TargetProbe';

/**
 * TargetRow shows one channel as its name, kind and host, and everything about
 * it in the window the row opens.
 */
export function TargetRow({
  row,
  index,
  last,
  open,
  triggers,
  placeholders,
  status,
  onToggle,
  onChange,
  onRemove,
}: {
  row: Channel;
  index: number;
  last: boolean;
  open: boolean;
  /** From GET /api/scripts/triggers. */
  triggers: string[];
  /** From GET /api/eventtargets/placeholders. */
  placeholders: Placeholder[];
  status?: EventTargetStatus;
  onToggle: () => void;
  onChange: (next: Channel) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const kindName = useKindName();
  // Only the host, since ntfy and Gotify can take their credential in the query.
  const host = hostOf(addressOf(row)) || addressOf(row);
  const ticked = row.events?.length ?? 0;
  const on: ChannelOn = row.on || 'failure';
  const title = row.name?.trim() || kindName(row.kind);

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button type="button" onClick={onToggle} aria-haspopup="dialog" className="flex min-w-0 items-center gap-3 text-start">
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">{title}</span>
            <span dir="ltr" className="block truncate text-meta text-carbon-textMuted">
              {host ? `${kindName(row.kind)} · ${host}` : kindName(row.kind)}
            </span>
          </span>
          {ticked > 0 && <TickedCount n={ticked} />}
          {/* Only the off state is marked. */}
          {!row.enabled && (
            <span className="hidden shrink-0 text-meta uppercase tracking-wider text-carbon-textMuted sm:block">
              {t('settings.modules.off')}
            </span>
          )}
        </button>
        <IconBadge
          icon={<IconTrash width={16} height={16} />}
          hue={index}
          title={t('settings.eventTargets.remove')}
          aria-label={t('settings.eventTargets.remove')}
          // No blur, so no commit happens in front of the removal.
          onMouseDown={(e) => e.preventDefault()}
          onClick={onRemove}
        />
      </div>
      <RowRefusal field={`notifyChannels.${index}`} />

      {open && (
        <Sheet title={title} hue={index} wide onClose={onToggle}>
          <ToggleRow
            label={t('settings.eventTargets.enabled')}
            hint={t('settings.eventTargets.enabledHint')}
            checked={row.enabled}
            onChange={(v) => onChange({ ...row, enabled: v })}
            hue={index}
          />
          <SettingRow control="long" label={t('settings.eventTargets.name')} hint={t('settings.eventTargets.nameHint')}>
            <TextInput value={row.name ?? ''} onChange={(e) => onChange({ ...row, name: e.target.value })} />
          </SettingRow>

          <ChannelFields row={row} onChange={onChange} />
          {host !== '' && <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.leavesTheBox', { host })}</p>}

          <SettingRow label={t('settings.eventTargets.on')} hint={t('settings.eventTargets.onHint')}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={t('settings.eventTargets.on')}
              active={on === 'never' ? 'failure' : on}
              onSelect={(id) => onChange({ ...row, on: id as ChannelOn })}
              items={[
                { id: 'failure', label: t('settings.eventTargets.on.failure') },
                { id: 'always', label: t('settings.eventTargets.on.always') },
              ]}
            />
          </SettingRow>

          <TargetEvents
            triggers={triggers}
            picked={row.events ?? []}
            hue={index}
            noneText={t('settings.eventTargets.eventsAll')}
            noneQuiet
            onChange={(next) => {
              const out = { ...row };
              if (next.length === 0) delete out.events;
              else out.events = next;
              onChange(out);
            }}
          />

          {row.kind === 'webhook' && <Placeholders list={placeholders} picked={row.events ?? []} />}

          <div className="flex flex-col gap-4">
            <SettingRow control="short" label={t('settings.eventTargets.attempts')} hint={t('settings.eventTargets.attemptsHint')}>
              <div className="flex items-center gap-2">
                <NumberInput
                  dir="ltr"
                  value={row.attempts ?? 0}
                  min={0}
                  max={MAX_ATTEMPTS}
                  step={1}
                  onValue={(v) => onChange({ ...row, attempts: clamp(v, MAX_ATTEMPTS) })}
                />
                <span className="shrink-0 text-meta text-carbon-textMuted">
                  {(row.attempts ?? 0) === 0 ? t('settings.eventTargets.attemptsDefault') : String(row.attempts)}
                </span>
              </div>
            </SettingRow>
            <SettingRow control="short" label={t('settings.eventTargets.timeout')} hint={t('settings.eventTargets.timeoutHint')}>
              <div className="flex items-center gap-2">
                <NumberInput
                  dir="ltr"
                  value={row.timeoutSeconds ?? 0}
                  min={0}
                  max={MAX_TIMEOUT_SECONDS}
                  step={1}
                  onValue={(v) => onChange({ ...row, timeoutSeconds: clamp(v, MAX_TIMEOUT_SECONDS) })}
                />
                <span className="glim-num shrink-0 text-meta text-carbon-textMuted">
                  {(row.timeoutSeconds ?? 0) === 0 ? DEFAULT_TIMEOUT_SECONDS : row.timeoutSeconds}
                </span>
              </div>
            </SettingRow>
          </div>

          <TargetHealth status={status} />
          <TargetProbe row={row} />
        </Sheet>
      )}
    </li>
  );
}

/** clamp keeps 0, which means the default, and the kit's ceiling. */
function clamp(v: number, max: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole <= 0) return 0;
  return Math.min(whole, max);
}

/** TickedCount shows how many events are ticked, with the house tooltip. */
function TickedCount({ n }: { n: number }) {
  const { t } = useT();
  const tip = useTooltip<HTMLSpanElement>(t('settings.eventTargets.events'));
  // The span sits inside the row's button, so it takes no role and no tab stop of its own.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <span {...tipHoverProps} className="glim-num hidden shrink-0 text-xs text-carbon-textSub sm:block">
        {n}
      </span>
      {tip.node}
    </>
  );
}

/** PlaceholderChip is its own component because the tooltip is a hook. */
function PlaceholderChip({ name, tip: tipText, unused }: { name: string; tip: string; unused: boolean }) {
  const tip = useTooltip<HTMLElement>(tipText);
  return (
    <>
      <code
        dir="ltr"
        {...tip.triggerProps}
        className={`rounded-[var(--radius-pill)] bg-carbon-surface2 px-1.5 py-0.5 text-meta ${
          unused ? 'text-carbon-textMuted line-through' : 'text-carbon-textSub'
        }`}
      >
        {name}
      </code>
      {tip.node}
    </>
  );
}

/**
 * Placeholders lists the names the server fills into a webhook's address,
 * headers and body, and marks the ones no ticked event carries. The event
 * programs show their own list with it.
 */
export function Placeholders({ list, picked, hint }: { list: Placeholder[]; picked: string[]; hint?: string }) {
  const { t } = useT();
  if (list.length === 0) return null;
  return (
    <FieldGroup label={t('settings.eventTargets.placeholders')} hint={hint ?? t('settings.eventTargets.placeholdersHint')}>
      <div className="flex flex-wrap gap-1.5">
        {list.map((p) => {
          const unused =
            picked.length > 0 && p.triggers !== undefined && p.triggers.length > 0 && !p.triggers.some((tr) => picked.includes(tr));
          const name = `%%${p.name}%%`;
          return (
            <PlaceholderChip
              key={p.name}
              name={name}
              unused={unused}
              tip={unused ? t('settings.eventTargets.placeholderUnused', { name }) : p.scope}
            />
          );
        })}
      </div>
    </FieldGroup>
  );
}
