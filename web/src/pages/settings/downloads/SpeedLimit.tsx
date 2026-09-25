import { Field, UnitNumberInput } from '../../../components/ui';
import { RATE_UNITS } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { isLeet } from '../../../lib/leet';

/**
 * SpeedLimitField is the global download limit as the Downloads settings page
 * draws it. The shell bar's quick settings show this same field, so both
 * places read and edit the value alike. `value` and `onValue` are bytes per
 * second, 0 for no limit.
 */
export function SpeedLimitField({ value, onValue }: { value: number; onValue: (bytes: number) => void }) {
  const { t } = useT();
  return (
    <Field label={t('settings.globalSpeedLimit')} hint={t('settings.speedHint')}>
      <span className="flex items-center gap-2">
        <span className="min-w-0 flex-1">
          <UnitNumberInput value={value} units={RATE_UNITS} onValue={onValue} />
        </span>
        {/* The 1337 easter egg (docs/easter-eggs.md), which compares the stored
            bytes, whichever unit the field shows them in. The word stands
            beside the number rather than under it, where it would read as a
            validation message. */}
        {isLeet(value) && (
          <span className="shrink-0 text-[11px] leading-none text-carbon-textMuted">{t('settings.motion.storm')}</span>
        )}
      </span>
    </Field>
  );
}
