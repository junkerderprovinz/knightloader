import { useEffect, useMemo, useState } from 'react';
import {
  Image,
  Modal,
  PixelRatio,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
  useWindowDimensions,
} from 'react-native';
import * as Clipboard from 'expo-clipboard';
import qrcode from 'qrcode-generator';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { contrastOn } from '../theme/appearance';
import { TYPE, inkFor } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { CRYPTO_COINS, type CryptoCoin, type CryptoNetwork } from '../donate';
import { GlimButton, NotchCard } from './glim';
import { Cross, Paste } from './IconBadge';
import { NoArrival } from './Moving';
import { Text } from './Text';

/**
 * Each coin's mark for its tile, the paths the web UI draws
 * (web/src/components/donateMarks.tsx) as white bitmaps tinted like every other
 * glyph: Simple Icons, CC0 1.0, for Bitcoin, Ethereum, Tether, Binance, Solana
 * and Sui; cryptocurrency-icons, MIT, Copyright (c) 2018 Christopher Downer,
 * for USD Coin and XRP.
 */
const COIN_MARKS: Record<CryptoCoin['id'], number> = {
  btc: require('../../assets/coin-btc.png'),
  eth: require('../../assets/coin-eth.png'),
  usdt: require('../../assets/coin-usdt.png'),
  usdc: require('../../assets/coin-usdc.png'),
  bnb: require('../../assets/coin-bnb.png'),
  sol: require('../../assets/coin-sol.png'),
  sui: require('../../assets/coin-sui.png'),
  xrp: require('../../assets/coin-xrp.png'),
};

/**
 * The crypto window: pick a coin, then its chain, and get the address as a
 * code, as text and through a copy button. Nothing leaves the phone and no
 * account is needed at either end. Every chain on offer carries its own address
 * (../donate.ts), so a coin cannot be sent where nobody receives it.
 *
 * The code comes first, since it is what the window is opened for; the chain
 * sits directly under the address it changes, and the coins follow as tiles.
 * The way out is the button in the bottom row, with no corner X.
 */
export function CryptoDonate({ visible, onClose }: { visible: boolean; onClose: () => void }) {
  const { t } = useT();
  const { c, dark, corners, accent, hueAt, rainbow } = useAppearance();
  const { motion } = useMotion();
  const { height } = useWindowDimensions();
  const [coin, setCoin] = useState<CryptoCoin>(CRYPTO_COINS[0]!);
  const [network, setNetwork] = useState<CryptoNetwork>(CRYPTO_COINS[0]!.networks[0]!);
  const [copied, setCopied] = useState(false);
  const rows = useMemo(() => qrRows(network.address), [network.address]);

  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(id);
  }, [copied]);

  /** The fill a chosen tile or chip takes: its own position under the rainbow,
   *  the accent otherwise, and the ink measured on it. */
  const chosen = (i: number) => {
    const fill = hueAt(i) ?? accent;
    return { fill, ink: contrastOn(fill) };
  };
  // At rest a tile's mark takes its position as ink, as the web's glim-hue-icon
  // does; reactive rests neutral.
  const restingMark = (i: number) => {
    const hue = rainbow.reactive ? undefined : hueAt(i);
    return hue ? (dark ? hue : inkFor(hue)) : c.textSub;
  };
  const coinIndex = CRYPTO_COINS.indexOf(coin);

  return (
    <Modal visible={visible} transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={onClose}>
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={onClose}>
          {/* Swallows the press so a tap inside the window closes nothing. */}
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title={t('settings.cryptoTitle')} info={t('settings.cryptoIntro')} style={styles.flush}>
              {/* Only the body scrolls, so the title and the bottom row stay in
                  place on a short screen (rule 15). */}
              <ScrollView style={{ maxHeight: height * 0.66 }} contentContainerStyle={styles.body}>
                <Text style={[styles.appeal, { color: c.text }]}>{t('settings.donateAppeal')}</Text>
                <View style={[styles.code, { backgroundColor: c.surface2, ...corners.card }]}>
                  {/* Black on white in both themes: an inverted code is outside
                      the standard, and the scanners that refuse it are the wallet
                      apps a donor holds. The plate takes the card radius, which is
                      safe because the outer four modules are the quiet zone. */}
                  <View style={[styles.plate, { ...corners.card }]}>
                    <QrCode rows={rows} size={168} label={network.address} />
                  </View>
                  {/* The whole address, wrapping rather than shortened, since it
                      is checked by eye before anybody sends to it. */}
                  <Text selectable style={[styles.address, { color: c.text }]}>
                    {network.address}
                  </Text>

                  {/* Shown for a coin with one chain as well: it also says which
                      network the address belongs to. */}
                  <View style={styles.chips} accessibilityLabel={t('settings.cryptoNetworks')}>
                    {coin.networks.map((n, i) => {
                      const on = n.id === network.id;
                      const { fill, ink } = chosen(i);
                      return (
                        <Pressable
                          key={n.id}
                          onPress={() => {
                            setNetwork(n);
                            setCopied(false);
                          }}
                          accessibilityRole="button"
                          accessibilityState={{ selected: on }}
                          style={({ pressed }) => [
                            styles.chip,
                            { ...corners.pill, backgroundColor: on ? fill : pressed ? c.hoverRaised : c.surface3 },
                          ]}
                        >
                          <Text style={[styles.chipText, { color: on ? ink : c.textSub }]}>{n.name}</Text>
                        </Pressable>
                      );
                    })}
                  </View>

                  {/* What a donor on this chain has to know, where they would
                      look for it. */}
                  {network.noteKey && (
                    <Text style={[styles.note, { color: c.statusWarnText }]}>{t(network.noteKey)}</Text>
                  )}

                  {/* The coin's own position, so under the rainbow it matches the
                      tile the address came from. */}
                  <GlimButton
                    hue={coinIndex}
                    label={copied ? t('settings.cryptoCopied') : t('settings.cryptoCopy')}
                    icon={(ink) => <Paste color={ink} />}
                    onPress={() => {
                      void Clipboard.setStringAsync(network.address)
                        .then(() => setCopied(true))
                        // The address stays on screen whole, to be selected by hand.
                        .catch(() => undefined);
                    }}
                  />
                </View>

                {/* Tiles, since a coin's mark is recognised faster than its name
                    is read. Square whatever they show, with the mark at their
                    heart. */}
                <View style={styles.tiles}>
                  {CRYPTO_COINS.map((k, i) => {
                    const on = k.id === coin.id;
                    const { fill, ink } = chosen(i);
                    return (
                      <Pressable
                        key={k.id}
                        onPress={() => {
                          // Always the new coin's first chain, never one carried
                          // over from the last coin without being checked.
                          setCoin(k);
                          setNetwork(k.networks[0]!);
                          setCopied(false);
                        }}
                        accessibilityRole="button"
                        accessibilityState={{ selected: on }}
                        accessibilityLabel={`${k.name} (${k.symbol})`}
                        style={({ pressed }) => [
                          styles.tile,
                          {
                            ...corners.control,
                            // A press stands in for the web's hover.
                            backgroundColor: on ? fill : pressed ? c.tileHover : c.surface2,
                          },
                        ]}
                      >
                        {({ pressed }) => {
                          // A filled tile paints its mark in its fill's ink, never
                          // in a colour nobody can predict the contrast of. A
                          // pressed one takes the tile ink, since a rainbow hue
                          // measures under 3:1 on the dark theme's grey.
                          const markInk = on ? ink : pressed ? c.tileHoverInk : restingMark(i);
                          const wordInk = on ? ink : pressed ? markInk : c.textSub;
                          return (
                            <>
                              <Image source={COIN_MARKS[k.id]} style={styles.mark} tintColor={markInk} resizeMode="contain" />
                              <Text style={[styles.ticker, { color: wordInk }]}>{k.symbol}</Text>
                            </>
                          );
                        }}
                      </Pressable>
                    );
                  })}
                </View>
              </ScrollView>

              <View style={styles.actions}>
                <GlimButton tone="quiet" label={t('settings.cryptoClose')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
              </View>
            </NotchCard>
          </Pressable>
        </Pressable>
      </NoArrival>
    </Modal>
  );
}

/**
 * The address as a module grid, qrcode-generator with the web UI's settings:
 * the smallest version that fits, at level M, which suits a screen.
 */
function qrRows(value: string): boolean[][] {
  const qr = qrcode(0, 'M');
  qr.addData(value);
  qr.make();
  const n = qr.getModuleCount();
  return Array.from({ length: n }, (_, y) => Array.from({ length: n }, (_, x) => qr.isDark(y, x)));
}

/**
 * The grid drawn with Views: one per run of dark modules in a row, which keeps
 * a 37-module code to a few hundred views. Each module is a whole number of
 * device pixels, or the rows would meet on fractions and show hairline seams.
 */
function QrCode({ rows, size, label }: { rows: boolean[][]; size: number; label: string }) {
  const n = rows.length;
  const m = PixelRatio.roundToNearestPixel(Math.floor(size / n));
  const runs: { x: number; y: number; w: number }[] = [];
  rows.forEach((row, y) => {
    for (let x = 0; x < n; ) {
      if (!row[x]) {
        x += 1;
        continue;
      }
      let w = 1;
      while (x + w < n && row[x + w]) w += 1;
      runs.push({ x, y, w });
      x += w;
    }
  });
  return (
    <View style={{ width: m * n, height: m * n }} accessibilityRole="image" accessibilityLabel={label}>
      {runs.map((r) => (
        <View
          key={`${r.y}-${r.x}`}
          style={{ position: 'absolute', left: r.x * m, top: r.y * m, width: r.w * m, height: m, backgroundColor: '#000000' }}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  // No backgroundColor here: it is `c.scrim`, applied at render time, so it
  // follows the theme.
  scrim: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 16 },
  window: { width: '100%', maxWidth: 480 },
  flush: { marginTop: 0 },
  body: { gap: 16, paddingTop: 4 },
  appeal: { fontSize: TYPE.body, lineHeight: 20 },
  code: { alignItems: 'center', gap: 12, padding: 16 },
  plate: { padding: 12, backgroundColor: '#ffffff' },
  address: {
    fontSize: TYPE.dense,
    textAlign: 'center',
    fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace',
  },
  chips: { flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'center', gap: 8 },
  chip: { paddingHorizontal: 12, paddingVertical: 5 },
  chipText: { fontSize: TYPE.dense, fontWeight: '500' },
  note: { fontSize: TYPE.dense, textAlign: 'center' },
  // Four across: 23% each, with what is left spread between them.
  tiles: { flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'space-between', rowGap: 8 },
  tile: { width: '23%', aspectRatio: 1, alignItems: 'center', justifyContent: 'center', gap: 4 },
  mark: { width: 26, height: 26 },
  ticker: { fontSize: TYPE.dense, fontWeight: '500' },
  actions: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: 16 },
});
