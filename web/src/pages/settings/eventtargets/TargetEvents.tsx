import { FieldGroup, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { FIRES_PER_LINK, REPLAYS_AFTER_RESTART, useTriggerLabel } from '../../../lib/triggers';

/**
 * Which events reach this target: one switch per trigger, never a checkbox.
 *
 * THE LIST COMES FROM THE SERVER and the labels come from lib/triggers.ts, which
 * the script editor reads too. Neither is written down here. A list on this side
 * would offer events the registry cannot fire and miss ones it can, and a second
 * label map would be the drift that already happened once: the script editor's
 * own map named four of the eleven, so seven events were drawn as their raw
 * dotted ids in a menu of sentences.
 *
 * NOTHING IS TICKED TO START WITH, and an empty list means this target never
 * sends. That is the server's reading too (notify.Target.Triggers), and it is
 * the only safe default: a target that arrived subscribed to everything would
 * send two hundred messages the first time somebody pasted a container.
 *
 * TWO WARNINGS ARE DRAWN HERE AND NOWHERE ELSE, because this is the only place
 * on the page where the choice that causes them is made.
 *
 * The first is the restart replay. Three of the eleven fire again after every
 * restart, container pull and upgrade, because what they remember lives in
 * memory rather than on disk - see REPLAYS_AFTER_RESTART for which three and
 * why. This is the single biggest way this feature turns into noise at three in
 * the morning for somebody who has been running it for a year, and there is
 * deliberately no silent suppression window anywhere: a grace period that
 * swallowed a real captcha arriving eight seconds after boot would be worse than
 * the noise. So it is said, per event, and the choice stays with the operator.
 *
 * The second is link.added, which fires once per LINK rather than once per
 * paste. Two hundred links is two hundred outbound requests, which is how
 * somebody gets rate limited by a public ntfy instance - and what the queue
 * cannot hold is dropped and counted rather than piling up, which the status
 * block below reports.
 */
export function TargetEvents({
  triggers,
  picked,
  hue,
  onChange,
}: {
  /** From GET /api/scripts/triggers, in the order the registry returns them:
   *  the original four first, then the seven the event bus added. Rendered in
   *  that order rather than sorted, so "a download finishes" stays at the top of
   *  the list where the script editor also puts it. */
  triggers: string[];
  picked: string[];
  hue: number;
  onChange: (next: string[]) => void;
}) {
  const { t } = useT();
  const triggerLabel = useTriggerLabel();

  const toggle = (id: string, on: boolean) =>
    // Filtered out of the ORIGINAL order rather than pushed onto the end, so
    // ticking, unticking and ticking again does not reshuffle the list the row
    // stores and make an unrelated save look like a change.
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

        {/* The state of this row, not an explanation of the field: the
            explanation is behind the (i) on the caption above. */}
        {picked.length === 0 && <p className="text-xs text-statusWarn">{t('settings.eventTargets.eventsNone')}</p>}
        {picked.includes(FIRES_PER_LINK) && (
          <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.eventsBurst')}</p>
        )}
        {/* Drawn only once something that replays is actually ticked. Shown
            always it would be a paragraph everybody learns to scroll past, and
            the one person it is written for would scroll past it too. */}
        {anyReplays && <p className="text-xs text-carbon-textMuted">{t('settings.eventTargets.eventsReplay')}</p>}
      </div>
    </FieldGroup>
  );
}
