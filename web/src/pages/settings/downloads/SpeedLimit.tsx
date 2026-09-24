import { Field, NumberInput } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { isLeet } from '../../../lib/leet';

/**
 * SpeedLimitField is the global download limit as the Downloads settings page
 * draws it. The shell bar's quick settings show this same field, so both
 * places edit the value in the same unit. `value` and `onValue` are bytes per
 * second, 0 for no limit.
 */
export function SpeedLimitField({ value, onValue }: { value: number; onValue: (bytes: number) => void }) {
  const { t } = useT();
  return (
    <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
      <span className="flex items-center gap-2">
        <span className="min-w-0 flex-1">
          <NumberInput
            value={Math.round(value / 1024)}
            min={0}
            step={256}
            onValue={(v) => onValue(Math.max(0, v) * 1024)}
          />
        </span>
        {/* The 1337 easter egg (docs/easter-eggs.md). The field shows KiB, so
            isLeet compares against 1337 KiB. The word stands beside the number
            rather than under it, where it would read as a validation message. */}
        {isLeet(value) && (
          <span className="shrink-0 text-[11px] leading-none text-carbon-textMuted">{t('settings.motion.storm')}</span>
        )}
      </span>
    </Field>
  );
}
