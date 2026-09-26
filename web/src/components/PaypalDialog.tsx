import { useState } from 'react';

import { Button, FieldGroup, Modal, TextInput } from './ui';
import { Tabs } from './Tabs';
import { IconClose } from '../lib/icons';
import { PAYPAL, PAYPAL_AMOUNTS } from '../lib/donate';
import { parseAmount, type GiveFrequency } from '../lib/paypal';
import { usePaypalButtons } from '../lib/usePaypalButtons';
import { useShake } from '../lib/useShake';
import { useT } from '../lib/i18n';

const DEFAULT_AMOUNT = '25';

/**
 * PaypalDialog asks how often and how much in the app's own controls, then
 * shows PayPal's two buttons: the PayPal one opens PayPal's login popup, and
 * the card one opens a card form inside this window for somebody without a
 * PayPal account. PayPal draws its buttons itself and allows no more than a
 * colour, a radius and a height, so the window keeps them apart from the
 * controls above them.
 */
export function PaypalDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const [frequency, setFrequency] = useState<GiveFrequency>('once');
  const [preset, setPreset] = useState<string | null>(DEFAULT_AMOUNT);
  const [typed, setTyped] = useState('');
  const [status, setStatus] = useState<'idle' | 'done' | 'failed'>('idle');
  const [refused, setRefused] = useState(0);
  const field = useShake<HTMLDivElement>(refused);
  const typedAmount = parseAmount(typed);

  const buttons = usePaypalButtons({
    config: PAYPAL,
    frequency,
    // Half typed, the buttons refuse rather than charge a preset the donor
    // is typing over.
    amount: typed === '' ? preset : typedAmount,
    description: 'KnightLoader',
    onDone: () => setStatus('done'),
    onError: () => setStatus('failed'),
  });

  // A valid typed amount takes the selection from the presets, so the window
  // never shows two amounts at once; clearing the field gives it back.
  function type(text: string) {
    setTyped(text);
    if (parseAmount(text)) setPreset(null);
    else if (text === '') setPreset((p) => p ?? DEFAULT_AMOUNT);
  }

  // Leaving the field shows the amount as it will be charged, hands a preset's
  // amount back to its preset, and shakes text that is no amount.
  function leave() {
    if (typed === '') return;
    if (!typedAmount) {
      setRefused((n) => n + 1);
      return;
    }
    if (PAYPAL_AMOUNTS.includes(typedAmount)) {
      setPreset(typedAmount);
      setTyped('');
    } else setTyped(typedAmount);
  }

  const frequencyLabel = t('settings.about.paypalFrequency');
  const amountLabel = t('settings.about.paypalAmount');
  return (
    <Modal
      title="PayPal"
      height="capped"
      onClose={onClose}
      footer={
        <Button
          kind="primary"
          labelled
          icon={<IconClose width={16} height={16} />}
          title={t('common.close')}
          onClick={onClose}
        />
      }
    >
      <p className="text-sm text-carbon-text">{t('settings.about.donateAppeal')}</p>
      <p className="text-sm text-carbon-textSub">{t('settings.about.paypalIntro')}</p>

      <FieldGroup label={frequencyLabel}>
        <Tabs
          variant="well"
          size="sm"
          label={frequencyLabel}
          active={frequency}
          onSelect={(id) => {
            setFrequency(id as GiveFrequency);
            setStatus('idle');
          }}
          items={[
            { id: 'once', label: t('settings.about.paypalOnce') },
            { id: 'month', label: t('settings.about.paypalMonthly') },
            { id: 'year', label: t('settings.about.paypalYearly') },
          ]}
        />
      </FieldGroup>

      <FieldGroup label={amountLabel}>
        {/* The presets keep the window's width, and the free amount stands at
            the end of the row under them. */}
        <div className="flex flex-col items-end gap-2">
          <Tabs
            variant="well"
            size="sm"
            label={amountLabel}
            active={preset}
            onSelect={(id) => {
              setPreset(id);
              setTyped('');
            }}
            items={PAYPAL_AMOUNTS.map((a) => ({ id: a, label: `${a} €` }))}
          />
          <div ref={field} className="w-36">
            <TextInput
              inputMode="decimal"
              value={typed}
              placeholder={t('settings.about.paypalOtherAmount')}
              aria-label={t('settings.about.paypalOtherAmount')}
              aria-invalid={typed !== '' && !typedAmount}
              onChange={(e) => type(e.target.value)}
              onBlur={leave}
              // The height pinned, or the border would add to a field that
              // otherwise measures --btn-h like every other.
              className={`h-[var(--btn-h)] border ${typedAmount ? 'border-accent' : 'border-transparent'}`}
            />
          </div>
        </div>
      </FieldGroup>

      {/* PayPal's frames are drawn light, and inside a dark color-scheme the
          browser paints them an opaque white strip, so their box declares the
          light scheme. The margin adds to the window's gap to set PayPal's
          controls apart from the app's. */}
      <div className="mt-2">
        <div ref={buttons.container} className="[color-scheme:light]" />
        {!buttons.ready && status !== 'failed' && (
          <p className="py-2 text-center text-xs text-carbon-textMuted">{t('settings.about.paypalLoading')}</p>
        )}
      </div>

      {status === 'done' && <p className="text-sm text-statusOk">{t('settings.about.paypalThanks')}</p>}
      {status === 'failed' && <p className="text-sm text-statusFail">{t('settings.about.paypalFailed')}</p>}
    </Modal>
  );
}
