import { useEffect, useMemo, useState, type CSSProperties } from 'react';

import { Button, Modal, useTooltip } from './ui';
import { CoinMark } from './donateMarks';
import { QRCode } from './QRCode';
import { hueVars, rainbowAt } from '../lib/appearance';
import { qrMatrix } from '../lib/qrmatrix';
import { CRYPTO_COINS, type CryptoCoin, type CryptoNetwork } from '../lib/donate';
import { useT } from '../lib/i18n';
import { useNavLabels } from '../lib/navLabels';

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
//
// IT DOES ASK FOR THE CORNER X, and it is the one window in the app that has
// to. Modal draws that square only for a caller that passes a label, because
// seventeen of these windows carry a Cancel button in their footer and an X
// above it offers the same answer twice. This one has no footer at all: nothing
// in it is a decision, the address is simply on screen. Without the X the only
// ways out were Escape and a click on the scrim, and neither is visible.
// ---------------------------------------------------------------------------

export function CryptoDonateDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const [coin, setCoin] = useState<CryptoCoin>(CRYPTO_COINS[0]!);
  const [network, setNetwork] = useState<CryptoNetwork>(CRYPTO_COINS[0]!.networks[0]!);
  const [copied, setCopied] = useState(false);
  const matrix = useMemo(() => qrMatrix(network.address), [network.address]);

  // The label engine, under this repo's own name for it, and `hover` is drawn
  // AS hover here like everywhere else.
  //
  // It used to resolve to the same thing as `both` on this one surface, with
  // the argument written out: a grid of eight coins is something somebody
  // SEARCHES, and hiding the tickers until asked would turn "find USDT" into
  // hovering every tile in turn. GlimStone heard that argument, said it was a
  // good one, and rejected it anyway - a control that quietly resolves a mode
  // to something else IS the complaint, however well it argues, because from
  // outside a documented exemption and a control that simply ignores the
  // setting look identical. So: at rest a reactive tile looks exactly like
  // glyph mode, the ticker comes back under the pointer, and the SELECTED coin
  // keeps its word, so the one answer on screen is never the one nobody can
  // read. Nothing resizes while that happens - the tile is a square sized by
  // the grid, and the ticker collapses inside it.
  const labels = useNavLabels();
  const showMark = labels !== 'text';
  const showTicker = labels !== 'glyph';
  const tickerOnHover = labels === 'hover';

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
    <Modal title={t('settings.about.cryptoTitle')} onClose={onClose} closeLabel={t('common.close')}>
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
          {/* A chain name is DATA and has no symbol, so the label engine has
              nothing to hide here and these stay words in every mode. The
              colour engine still applies: each chain owns a position, so the
              chosen one fills in its own hue. */}
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
              style={hueVars(rainbowAt(i)) as CSSProperties}
              // The pill TOKEN and never rounded-full: the two are identical at
              // the default round setting, which is exactly why the difference
              // survives a build-and-glance - it only shows once somebody
              // switches the shape to square, where a hard-wired 9999px stays a
              // stadium in a window of right angles.
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
          read.

          Each tile owns a palette position, so rainbow mode makes eight coins
          scannable by colour the way it makes any other list scannable.
          `.glim-hue-icon` tints the mark itself while the ticker sits beside
          it, and is dropped in glyph mode: there the mark IS the tile's whole
          content, and the house rule for an icon-only badge is that only the
          fill carries colour. */}
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

/**
 * The ticker, present in the markup but collapsed to nothing until the pointer
 * or the keyboard arrives. Collapsed rather than left out, and that IS the
 * mechanism: a label that is there but has no height can grow back in place, so
 * the tile keeps exactly the geometry it has in every other label mode and
 * nothing around it is ever re-measured. The same classes Tabs.tsx hides a rail
 * label with, so the two surfaces answer the reactive setting identically.
 */
const HIDDEN_TICKER =
  'max-h-0 leading-4 opacity-0 transition-all duration-200 group-hover:max-h-4 group-hover:opacity-100 ' +
  'group-focus-visible:max-h-4 group-focus-visible:opacity-100';

/**
 * One coin, as a square tile.
 *
 * SQUARE, AND THE MARK IS HALF THE TILE. Both numbers come from the sibling
 * surface in this app that already settled them - BrowserTools' download tiles,
 * a 112px box around a 56px mark - rather than from whatever the mark happened
 * to measure before the tile became a square. `aspect-square` and not a fixed
 * height, so the tile stays square in all three label modes: a picker that
 * changes height when somebody switches how controls are labelled reads as a
 * different grid. The mark is sized against the tile's own height rather than
 * in pixels, so half stays half at whatever width the four columns work out to.
 *
 * Its own component because the tooltip is a hook and a hook cannot be called
 * inside the map above. That tooltip is the house bubble and never a native
 * `title=`: one control, one tooltip mechanism, and the operating system's own
 * balloon draws in the OS font, at the pointer instead of at the trigger, and
 * is untouched by every rule the house bubble follows.
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
  // role and tabIndex stripped back out: this is a real <button> carrying
  // role="option" in a listbox, and triggerProps' own "note" role would tell a
  // screen reader the tile is a description rather than the control it is.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        role="option"
        aria-selected={selected}
        aria-label={`${coin.name} (${coin.symbol})`}
        onClick={onPick}
        style={hueVars(rainbowAt(hue)) as CSSProperties}
        {...tipHoverProps}
        className={`group flex aspect-square flex-col items-center justify-center gap-1 rounded-[var(--radius-control)]
          px-2 transition-colors ${showTicker ? 'glim-hue glim-hue-icon' : 'glim-hue'} ${
            selected
              ? 'glim-active bg-accent text-accentContrast'
              : 'bg-carbon-surface2 text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text'
          }`}
      >
        {showMark && (
          // Half the tile's own side, read off the square rather than typed as
          // a number: the svg's width/height attributes are what CSS overrides
          // here, and `w-auto` keeps the viewBox's square proportion.
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
