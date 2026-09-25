import { useEffect, useRef, useState } from 'react';
import { donationHandlers, loadPaypal, type GiveFrequency, type PaypalConfig } from './paypal';

/**
 * usePaypalButtons renders PayPal's two buttons into the returned container,
 * and again whenever the frequency changes, since a subscription needs the
 * other SDK namespace. The amount is read at click time, so picking another
 * amount reloads nothing. `ready` turns true once PayPal has drawn them.
 */
export function usePaypalButtons({
  config,
  frequency,
  amount,
  description,
  onDone,
  onError,
}: {
  config: PaypalConfig;
  frequency: GiveFrequency;
  /** The amount to charge, or null while the typed amount is not valid. */
  amount: string | null;
  description: string;
  onDone: () => void;
  onError: () => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const [ready, setReady] = useState(false);
  const latest = useRef({ amount, onDone, onError });
  latest.current = { amount, onDone, onError };

  useEffect(() => {
    const box = container.current;
    if (!box) return;
    let buttons: { close(): Promise<void> } | null = null;
    let cancelled = false;
    setReady(false);

    loadPaypal(config, frequency !== 'once').then(
      (paypal) => {
        if (cancelled) return;
        const instance = paypal.Buttons({
          // Blue is one of the five colours PayPal allows, and reads on both themes.
          style: { layout: 'vertical', color: 'blue', shape: 'rect', borderRadius: 10, label: 'donate', height: 40 },
          // A half-typed amount keeps the buttons from opening PayPal at all.
          onClick: (_: unknown, actions: { resolve(): void; reject(): void }) =>
            latest.current.amount ? actions.resolve() : actions.reject(),
          ...donationHandlers(
            config,
            frequency,
            () => latest.current.amount ?? '0',
            description,
            () => latest.current.onDone(),
          ),
          onError: () => latest.current.onError(),
        });
        buttons = instance;
        // The old instance closes asynchronously, so its frames could still
        // stand beside the new ones.
        box.replaceChildren();
        instance.render(box).then(
          () => {
            if (!cancelled) setReady(true);
          },
          () => latest.current.onError(),
        );
      },
      () => {
        if (!cancelled) latest.current.onError();
      },
    );

    return () => {
      cancelled = true;
      buttons?.close().catch(() => {});
    };
  }, [config, frequency, description]);

  return { container, ready };
}
