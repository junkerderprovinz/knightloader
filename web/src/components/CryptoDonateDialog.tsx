import { useEffect, useMemo, useState } from 'react';

import { Button, Modal } from './ui';
import { QRCode } from './QRCode';
import { qrMatrix } from '../lib/qrmatrix';
import { CRYPTO_CHAINS, type CryptoChain } from '../lib/donate';
import { useT } from '../lib/i18n';

// ---------------------------------------------------------------------------
// The crypto window: the second way to give (GlimStone 1.8.2).
//
// The coffee button takes a card. This takes what somebody already holds in a
// wallet: no account, no name at either end, and nothing leaving the machine.
// Two buttons under one sentence, because they reach two different people.
//
// THE RULE THIS WINDOW EXISTS FOR: the list is grouped BY CHAIN and never by
// coin. See lib/donate.ts for the whole reasoning and the near miss that
// produced it. Short version: one 0x address offered under "Tether" beside the
// networks BNB, Tron, Solana and Ethereum would have destroyed a donation sent
// over Tron, and nobody would ever have reported it.
//
// The address is offered three ways at once and a donor uses whichever they
// have: as text for a desktop wallet, as a QR for a phone, and as a copy
// button for whoever trusts neither their eyes nor their camera. The text form
// is whole and never shortened - an address is read back by eye before
// somebody sends to it, and an ellipsis in the middle turns the one string
// that has to be exact into a string nobody can check.
//
// Built on this app's own Modal rather than on the design language's window,
// because it has to look like the other dialogs standing beside it here. The
// Modal already owns Escape, the backdrop click and the corner close, so this
// file adds no second way to shut it.
// ---------------------------------------------------------------------------

export function CryptoDonateDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const [picked, setPicked] = useState<CryptoChain>(CRYPTO_CHAINS[0]!);
  const [copied, setCopied] = useState(false);
  const matrix = useMemo(() => qrMatrix(picked.address), [picked.address]);

  // The label flips for a moment and goes back, the same answer every other
  // copy control in this app gives.
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(id);
  }, [copied]);

  return (
    <Modal title={t('settings.about.cryptoTitle')} onClose={onClose}>
      <p className="text-sm text-carbon-textSub">{t('settings.about.cryptoIntro')}</p>

      {/* A row is a button because it does something, and the picked one is
          FILLED with the accent, the way every other chosen thing here is
          marked. A list rather than a dropdown: five chains is something
          somebody reads, and a closed picker would hide the very choice this
          window exists to put in front of them. */}
      <div className="flex flex-col gap-1" role="listbox" aria-label={t('settings.about.cryptoTitle')}>
        {CRYPTO_CHAINS.map((chain) => (
          <button
            key={chain.id}
            type="button"
            role="option"
            aria-selected={chain.id === picked.id}
            onClick={() => {
              setPicked(chain);
              setCopied(false);
            }}
            className={`flex items-baseline gap-2 rounded-[var(--radius-control)] px-3 py-2 text-start transition-colors ${
              chain.id === picked.id
                ? 'bg-accent text-accentContrast'
                : 'text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text'
            }`}
          >
            <span className="font-medium">{chain.name}</span>
            <span className="text-xs opacity-80">{chain.coins}</span>
          </button>
        ))}
      </div>

      <div className="flex flex-col items-center gap-3 rounded-[var(--radius-card)] bg-carbon-surface2 p-4">
        <QRCode matrix={matrix} label={picked.address} size={168} />
        <p dir="ltr" className="glim-num w-full break-all text-center font-mono text-xs text-carbon-text">
          {picked.address}
        </p>
        {picked.networks && (
          <p className="text-center text-xs text-carbon-textMuted">
            {t('settings.about.cryptoNetworks')}: {picked.networks}
          </p>
        )}
        {/* Warn-coloured, and it is not a warning: it is the line a donor would
            otherwise go hunting for. Exchanges train people to look for a
            destination tag or a memo, so the chain that wants neither has to
            say so where the address is. */}
        {picked.noteKey && <p className="text-center text-xs text-statusWarn">{t(picked.noteKey)}</p>}
        <Button
          kind="primary"
          onClick={() => {
            void navigator.clipboard?.writeText(picked.address).then(() => setCopied(true));
          }}
        >
          {copied ? t('common.copied') : t('common.copy')}
        </Button>
      </div>
    </Modal>
  );
}
