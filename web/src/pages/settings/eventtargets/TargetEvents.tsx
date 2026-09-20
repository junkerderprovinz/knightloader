import { FieldGroup, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { FIRES_PER_LINK, REPLAYS_AFTER_RESTART, useTriggerLabel } from '../../../lib/triggers';

/**
 * TargetEvents picks the events that reach this target, one switch per trigger.
 * The list comes from the server and the labels from lib/triggers.ts, and an
 * empty list means the target never sends. It warns about the events that fire
 * again after every restart and about link.added, which fires once per link.
 */
export function TargetEvents({
  triggers,
  picked,
  hue,
  onChange,
}: {
  /** From GET /api/scripts/triggers, kept in the registry's order like the script editor. */
  triggers: string[];
  picked: string[];
  hue: number;
  onChange: (next: string[]) => void;
}) {
  const { t } = useT();
  const triggerLabel = useTriggerLabel();

  const toggle = (id: string, on: boolean) =>
    // Kept in the registry's order, so switching back and forth stores the same list.
    onChange(on ? triggers.filter((tr) => picked.includes(tr) || tr === id) : picked.filter((tr) => tr !== id));

  const anyReplays = picked.some((tr) => REPLAYS_AFTER_RESTART.has(tr));

  return (
    <FieldGroup label={t('settings.eventTargets.events')} hint={t('settings.eventTargets.eventsHint')}>
      <div className="flex flex-col gap-2">
        {triggers.map((tr) => (
          <ToggleRow
            key={tr}
            label={triggerLabel(tr)}
            checked={picked.includes(tr)}
            onChange={(v) => toggle(tr, v)}
            hue={hue}
          />
        ))}

        {picked.length === 0 && <p className="text-xs text-statusWarn">{t('settings.eventTargets.eventsNone')}</p>}
        {picked.includes(FIRES_PER_LINK) && (
          <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.eventsBurst')}</p>
        )}
        {anyReplays && <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.eventsReplay')}</p>}
      </div>
    </FieldGroup>
  );
}
