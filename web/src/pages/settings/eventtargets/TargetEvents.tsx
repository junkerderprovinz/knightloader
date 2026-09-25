import { FieldGroup, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { FIRES_PER_LINK, REPLAYS_AFTER_RESTART, useTriggerLabel } from '../../../lib/triggers';

/**
 * TargetEvents picks the events that reach this target, one switch per trigger.
 * The list comes from the server and the labels from lib/triggers.ts, and an
 * empty list means the target never sends. The events that fire again after
 * every restart, and link.added, which fires once per link, say so in their
 * own (i). The event programs use it too, with their own wording for the (i)
 * and for nothing ticked.
 */
export function TargetEvents({
  triggers,
  picked,
  hue,
  onChange,
  hint,
  noneText,
  burstHint,
}: {
  /** From GET /api/scripts/triggers, kept in the registry's order like the script editor. */
  triggers: string[];
  picked: string[];
  hue: number;
  onChange: (next: string[]) => void;
  hint?: string;
  noneText?: string;
  burstHint?: string;
}) {
  const { t } = useT();
  const triggerLabel = useTriggerLabel();

  const toggle = (id: string, on: boolean) =>
    // Kept in the registry's order, so switching back and forth stores the same list.
    onChange(on ? triggers.filter((tr) => picked.includes(tr) || tr === id) : picked.filter((tr) => tr !== id));

  const rowHint = (tr: string) =>
    tr === FIRES_PER_LINK
      ? (burstHint ?? t('settings.eventTargets.eventsBurst'))
      : REPLAYS_AFTER_RESTART.has(tr)
        ? t('settings.eventTargets.replaysHint')
        : undefined;

  return (
    <FieldGroup label={t('settings.eventTargets.events')} hint={hint ?? t('settings.eventTargets.eventsHint')}>
      <div className="flex flex-col gap-2">
        {triggers.map((tr) => (
          <ToggleRow
            key={tr}
            label={triggerLabel(tr)}
            hint={rowHint(tr)}
            checked={picked.includes(tr)}
            onChange={(v) => toggle(tr, v)}
            hue={hue}
          />
        ))}

        {picked.length === 0 && (
          <p className="text-xs text-statusWarn">{noneText ?? t('settings.eventTargets.eventsNone')}</p>
        )}
      </div>
    </FieldGroup>
  );
}
