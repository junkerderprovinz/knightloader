import { Field, NumberInput } from '../../../components/ui';
import { useT } from '../../../lib/i18n';

// The three counts that decide how much runs at once, as the Downloads settings
// page draws them. The shell bar's quick settings show these same fields, so
// both places offer the same bounds and the same explanation.

interface CountProps {
  value: number;
  onValue: (n: number) => void;
}

export function MaxConcurrentField({ value, onValue }: CountProps) {
  const { t } = useT();
  return (
    <Field label={t('settings.maxConcurrent')} hint={t('settings.maxConcurrentHint')}>
      <NumberInput value={value} min={1} max={64} onValue={onValue} />
    </Field>
  );
}

export function MaxPerHostField({ value, onValue }: CountProps) {
  const { t } = useT();
  return (
    <Field label={t('settings.maxPerHost')} hint={t('settings.maxPerHostHint')}>
      <NumberInput value={value} min={1} max={64} onValue={onValue} />
    </Field>
  );
}

/** The max is the engine's own bound. */
export function ChunksField({ value, onValue }: CountProps) {
  const { t } = useT();
  return (
    <Field label={t('settings.chunks')} hint={t('settings.chunksHint')}>
      <NumberInput value={value} min={0} max={16} onValue={onValue} />
    </Field>
  );
}
