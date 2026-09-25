import { useEffect, useMemo, useState, type CSSProperties } from 'react';

import { Button, Modal, useTooltip } from './ui';
import { CoinMark } from './donateMarks';
import { QRCode } from './QRCode';
import { hueVars } from '../lib/appearance';
import { qrMatrix } from '../lib/qrmatrix';
import { IconClose } from '../lib/icons';
import { CRYPTO_COINS, type CryptoCoin, type CryptoNetwork } from '../lib/donate';
import { useT } from '../lib/i18n';
import { useNavLabels } from '../lib/navLabels';

/**
 * CryptoDonateDialog shows a QR code and address for the chosen coin and
 * network. Every network offered carries its own address (see lib/donate.ts),
 * so a donor can never send over a chain nobody receives on.
 */
export function CryptoDonateDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const [coin, setCoin] = useState<CryptoCoin>(CRYPTO_COINS[0]!);
  const [network, setNetwork] = useState<CryptoNetwork>(CRYPTO_COINS[0]!.networks[0]!);
  const [copied, setCopied] = useState(false);
  const matrix = useMemo(() => qrMatrix(network.address), [network.address]);

  // In `hover` mode the ticker shows under the pointer, and the selected coin
  // always keeps its word.
  const labels = useNavLabels();
  const showMark = labels !== 'text';
  const showTicker = labels !== 'glyph';
  const tickerOnHover = labels === 'hover';

  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(id);
  }, [copied]);

  // Always the new coin's first network, never a chain carried over unchecked.
  function pickCoin(next: CryptoCoin) {
    setCoin(next);
    setNetwork(next.networks[0]!);
    setCopied(false);
  }

  return (
    <Modal
      title={t('settings.about.cryptoTitle')}
      hint={t('settings.about.cryptoIntro')}
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
      <div className="flex flex-col items-center gap-3 rounded-[var(--radius-card)] bg-carbon-surface2 p-4">
        <QRCode matrix={matrix} label={network.address} size={168} />
        {/* Never shortened: an address is checked by eye before sending. */}
        <p dir="ltr" className="glim-num w-full break-all text-center font-mono text-xs text-carbon-text">
          {network.address}
        </p>
        {/* Shown even for a single network, since it names the address's chain.
            Chain names have no symbol, so they stay words in every label mode. */}
        <div
          className="flex flex-wrap justify-center gap-2"
          role="listbox"
          aria-label={t('settings.about.cryptoNetworks')}
        >
          {coin.networks.map((n, i) => (
            <button
              key={n.id}
              type="button"
              role="option"
              aria-selected={n.id === network.id}
              onClick={() => {
                setNetwork(n);
                setCopied(false);
              }}
              style={hueVars(i) as CSSProperties}
              // The pill token, not rounded-full, so the square shape setting applies.
              className={`glim-hue rounded-[var(--radius-pill)] px-3 py-1 text-xs font-medium transition-colors ${
                n.id === network.id
                  ? 'bg-accent text-accentContrast'
                  : 'bg-carbon-surface3 text-carbon-textSub hover:bg-carbon-hoverRaised hover:text-carbon-text'
              }`}
            >
              {n.name}
            </button>
          ))}
        </div>
        {/* Such as "no memo needed", which exchanges train people to look for. */}
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

      {/* Tiles, since a coin's mark is recognised faster than its name. */}
      <div
        className="grid grid-cols-4 gap-2"
        role="listbox"
        aria-label={t('settings.about.cryptoTitle')}
      >
        {CRYPTO_COINS.map((c, i) => (
          <CoinTile
            key={c.id}
            coin={c}
            hue={i}
            selected={c.id === coin.id}
            showMark={showMark}
            showTicker={showTicker}
            tickerOnHover={tickerOnHover}
            onPick={() => pickCoin(c)}
          />
        ))}
      </div>
    </Modal>
  );
}

// Collapsed rather than omitted, so the ticker grows back in place without
// re-measuring the tile. Tabs.tsx hides rail labels with the same classes.
const HIDDEN_TICKER =
  'leading-[1.4] max-h-0 opacity-0 transition-all duration-200 group-hover:max-h-[1.4em] ' +
  'group-hover:opacity-100 group-focus-visible:max-h-[1.4em] group-focus-visible:opacity-100';

/**
 * CoinTile is one coin as a square tile with the mark at half its height, like
 * BrowserTools' tiles. It is a component of its own because useTooltip is a hook.
 */
function CoinTile({
  coin,
  hue,
  selected,
  showMark,
  showTicker,
  tickerOnHover,
  onPick,
}: {
  coin: CryptoCoin;
  hue: number;
  selected: boolean;
  showMark: boolean;
  showTicker: boolean;
  tickerOnHover: boolean;
  onPick: () => void;
}) {
  const tip = useTooltip<HTMLButtonElement>(coin.name);
  // The button is an option in a listbox, so triggerProps' "note" role must go.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        role="option"
        aria-selected={selected}
        aria-label={`${coin.name} (${coin.symbol})`}
        onClick={onPick}
        style={hueVars(hue) as CSSProperties}
        {...tipHoverProps}
        className={`kl-coin-tile group flex aspect-square flex-col items-center justify-center gap-1 rounded-[var(--radius-control)]
          px-2 transition-colors ${showTicker ? 'glim-hue glim-hue-icon' : 'glim-hue'} ${
            selected
              ? 'glim-active bg-accent text-accentContrast'
              : 'bg-carbon-surface2 text-carbon-textSub hover:bg-carbon-tileHover hover:text-carbon-tileHoverInk'
          }`}
      >
        {showMark && (
          // CSS overrides the svg's size attributes to half the tile.
          <span className="flex h-1/2 shrink-0 items-center justify-center [&>svg]:h-full [&>svg]:w-auto">
            <CoinMark coin={coin.id} size={22} />
          </span>
        )}
        {showTicker && (
          <span
            className={`text-xs font-medium ${tickerOnHover && !selected ? HIDDEN_TICKER : ''}`}
          >
            {coin.symbol}
          </span>
        )}
      </button>
      {tip.node}
    </>
  );
}
