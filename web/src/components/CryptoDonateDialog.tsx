import { useEffect, useMemo, useState } from 'react';

import { Button, Modal } from './ui';
import { CoinMark } from './donateMarks';
import { QRCode } from './QRCode';
import { qrMatrix } from '../lib/qrmatrix';
import { CRYPTO_COINS, type CryptoCoin, type CryptoNetwork } from '../lib/donate';
import { useT } from '../lib/i18n';

// ---------------------------------------------------------------------------
// The crypto window: the second way to give (GlimStone 1.8.3).
//
// The coffee button takes a card. This takes what somebody already holds in a
// wallet: no account, no name at either end, and nothing leaving the machine.
//
// THE RULE THIS WINDOW EXISTS FOR: every network a donor can pick carries its
// OWN address. There is no line anywhere naming a chain without a wallet
// behind it, so a chain we cannot receive on is unofferable rather than merely
// discouraged. See lib/donate.ts for the near miss that produced the rule: one
// 0x address offered under "Tether" beside the networks BNB, Tron, Solana and
// Ethereum would have destroyed a donation sent over Tron, and nobody would
// ever have reported it.
//
// Coin first, chain second, and both are real choices. A donor thinks "I have
// USDT", not "I have Ethereum". The chain switches directly under the address
// it changes, because a picker one box away from its own effect makes somebody
// look twice to see whether the address moved.
//
// The code sits at the top and the picker underneath: a dialog usually asks
// before it answers, and this one is the other way round because the code is
// what the window was opened for.
//
// Built on this app's own Modal rather than on the design language's window,
// because it has to look like the other dialogs standing beside it here. The
// Modal already owns Escape, the backdrop click and the corner close, so this
// file adds no second way to shut it.
// ---------------------------------------------------------------------------

export function CryptoDonateDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const [coin, setCoin] = useState<CryptoCoin>(CRYPTO_COINS[0]!);
  const [network, setNetwork] = useState<CryptoNetwork>(CRYPTO_COINS[0]!.networks[0]!);
  const [copied, setCopied] = useState(false);
  const matrix = useMemo(() => qrMatrix(network.address), [network.address]);

  // The label flips for a moment and goes back, the same answer every other
  // copy control in this app gives.
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(id);
  }, [copied]);

  // Picking a coin always lands on a network of THAT coin. Keeping the previous
  // chain when it happens to also carry the new coin would be a convenience
  // with one bad case: a chain somebody last looked at staying selected under a
  // coin they never checked it against.
  function pickCoin(next: CryptoCoin) {
    setCoin(next);
    setNetwork(next.networks[0]!);
    setCopied(false);
  }

  return (
    <Modal title={t('settings.about.cryptoTitle')} onClose={onClose}>
      <p className="text-sm text-carbon-textSub">{t('settings.about.cryptoIntro')}</p>

      {/* The answer, first. */}
      <div className="flex flex-col items-center gap-3 rounded-[var(--radius-card)] bg-carbon-surface2 p-4">
        <QRCode matrix={matrix} label={network.address} size={168} />
        {/* Whole, in one piece, in a mono face, and never shortened: an address
            is read back by eye before somebody sends to it, so an ellipsis in
            the middle turns the one string that has to be exact into a string
            nobody can check. */}
        <p dir="ltr" className="glim-num w-full break-all text-center font-mono text-xs text-carbon-text">
          {network.address}
        </p>
        {/* The chain, switched here, under the address it changes. Shown even
            when a coin has only one, because this is also the line that SAYS
            which network the address belongs to, and that fact may not come and
            go with the tile. */}
        <div
          className="flex flex-wrap justify-center gap-2"
          role="listbox"
          aria-label={t('settings.about.cryptoNetworks')}
        >
          {coin.networks.map((n) => (
            <button
              key={n.id}
              type="button"
              role="option"
              aria-selected={n.id === network.id}
              onClick={() => {
                setNetwork(n);
                setCopied(false);
              }}
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                n.id === network.id
                  ? 'bg-accent text-accentContrast'
                  : 'bg-carbon-surface3 text-carbon-textSub hover:bg-carbon-hoverRaised hover:text-carbon-text'
              }`}
            >
              {n.name}
            </button>
          ))}
        </div>
        {/* Warn-coloured, and it is not a warning: it is the line a donor would
            otherwise go hunting for. Exchanges train people to look for a
            destination tag or a memo, so the chain that wants neither has to
            say so where the address is. */}
        {network.noteKey && (
          <p className="text-center text-xs text-statusWarn">{t(network.noteKey)}</p>
        )}
        <Button
          kind="primary"
          onClick={() => {
            void navigator.clipboard?.writeText(network.address).then(() => setCopied(true));
          }}
        >
          {copied ? t('common.copied') : t('common.copy')}
        </Button>
      </div>

      {/* The picker, under the answer it changes. Tiles rather than a list,
          because a coin is recognised by its mark faster than its name is
          read. */}
      <div
        className="grid grid-cols-4 gap-2"
        role="listbox"
        aria-label={t('settings.about.cryptoTitle')}
      >
        {CRYPTO_COINS.map((c) => (
          <button
            key={c.id}
            type="button"
            role="option"
            aria-selected={c.id === coin.id}
            aria-label={`${c.name} (${c.symbol})`}
            onClick={() => pickCoin(c)}
            className={`flex flex-col items-center gap-1 rounded-[var(--radius-control)] px-2 py-3 transition-colors ${
              c.id === coin.id
                ? 'bg-accent text-accentContrast'
                : 'bg-carbon-surface2 text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text'
            }`}
          >
            <CoinMark coin={c.id} size={22} />
            <span className="text-xs font-medium">{c.symbol}</span>
          </button>
        ))}
      </div>
    </Modal>
  );
}
