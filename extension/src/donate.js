// The donation addresses for the crypto window, the web UI's list
// (web/src/lib/donate.ts) with the types stripped. check-donate.mjs holds the
// two to each other, since an address that drifts on one surface only fails
// nobody's test and loses a stranger's money.
//
// Grouped by coin and then by chain, and every chain a donor can pick carries
// its own address, so a coin can never be sent over a chain where the address
// does not exist. A chain without an address is simply absent.

// The five wallets, each written once and shared by the networks below.
const DONATE_BTC = 'bc1q078lt57t4n5zq5md3knz3ythum0w78zmjw5eda';
/** One address for every EVM chain: the same key controls it on all of them. */
const DONATE_EVM = '0xFF6726C5bd76C8FD6b6bE7Ea5CEd4621fde5e841';
const DONATE_SOL = 'GrTyhSbZVArdaZAr3TqWDrkEGahomLtNZJ41qPLm3dHd';
const DONATE_SUI = '0xa76677f71d107c9a957c2d1814b68027cbd4ad9dbf28d911a97fad5d2fc3a414';
const DONATE_XRP = 'rwMK2nXqChT4JYWVypMypnpctDcM9jgWmG';

const DONATE_ETHEREUM = { id: 'ethereum', name: 'Ethereum', address: DONATE_EVM };
const DONATE_BASE = { id: 'base', name: 'Base', address: DONATE_EVM };
const DONATE_OPTIMISM = { id: 'optimism', name: 'Optimism', address: DONATE_EVM };
const DONATE_BSC = { id: 'bsc', name: 'BNB Smart Chain', address: DONATE_EVM };
const DONATE_SOLANA = { id: 'solana', name: 'Solana', address: DONATE_SOL };

// Each coin carries its own colour and the ink that holds on it, white wherever
// white reaches 2:1, which its tile lights up in under the pointer.
const CRYPTO_COINS = [
  { id: 'btc', symbol: 'BTC', name: 'Bitcoin', tile: { color: '#f7931a', ink: '#ffffff' }, networks: [{ id: 'bitcoin', name: 'Bitcoin', address: DONATE_BTC }] },
  // Native ETH only. On BNB Smart Chain, ETH is a bridged token.
  { id: 'eth', symbol: 'ETH', name: 'Ethereum', tile: { color: '#627eea', ink: '#ffffff' }, networks: [DONATE_ETHEREUM, DONATE_BASE, DONATE_OPTIMISM] },
  { id: 'usdt', symbol: 'USDT', name: 'Tether', tile: { color: '#26a17b', ink: '#ffffff' }, networks: [DONATE_ETHEREUM, DONATE_BSC, DONATE_SOLANA] },
  { id: 'usdc', symbol: 'USDC', name: 'USD Coin', tile: { color: '#2775ca', ink: '#ffffff' }, networks: [DONATE_ETHEREUM, DONATE_BASE, DONATE_SOLANA] },
  { id: 'bnb', symbol: 'BNB', name: 'BNB', tile: { color: '#f0b90b', ink: '#161616' }, networks: [DONATE_BSC] },
  { id: 'sol', symbol: 'SOL', name: 'Solana', tile: { color: '#9945ff', ink: '#ffffff' }, networks: [DONATE_SOLANA] },
  { id: 'sui', symbol: 'SUI', name: 'Sui', tile: { color: '#4da2ff', ink: '#ffffff' }, networks: [{ id: 'sui', name: 'Sui', address: DONATE_SUI }] },
  {
    id: 'xrp',
    symbol: 'XRP',
    name: 'XRP',
    tile: { color: '#23292f', ink: '#ffffff' },
    networks: [
      // Exchanges often require a destination tag; this self-custody account
      // does not (RequireDest is off), and the window says so beside it.
      { id: 'xrpl', name: 'XRP Ledger', address: DONATE_XRP, noteKey: 'options.cryptoNoTag' },
    ],
  },
];

/**
 * Which wallet each chain must resolve to, written out separately so the check
 * compares the list with something other than itself.
 */
const ADDRESS_BY_CHAIN = {
  bitcoin: DONATE_BTC,
  ethereum: DONATE_EVM,
  base: DONATE_EVM,
  optimism: DONATE_EVM,
  bsc: DONATE_EVM,
  solana: DONATE_SOL,
  sui: DONATE_SUI,
  xrpl: DONATE_XRP,
};

// Each coin's mark for its tile, the web UI's (web/src/components/donateMarks.tsx):
// Simple Icons, CC0 1.0, for Bitcoin, Ethereum, Tether, Binance, Solana and
// Sui; cryptocurrency-icons, MIT, Copyright (c) 2018 Christopher Downer, for
// USD Coin and XRP. Drawn in the tile's ink, like every other glyph, with the
// symbol a hole in the disc.
const COIN_MARKS = {
  btc: {
    box: '0 0 24 24',
    d: 'M23.638 14.904c-1.602 6.43-8.113 10.34-14.542 8.736C2.67 22.05-1.244 15.525.362 9.105 1.962 2.67 8.475-1.243 14.9.358c6.43 1.605 10.342 8.115 8.738 14.548v-.002zm-6.35-4.613c.24-1.59-.974-2.45-2.64-3.03l.54-2.153-1.315-.33-.525 2.107c-.345-.087-.705-.167-1.064-.25l.526-2.127-1.32-.33-.54 2.165c-.285-.067-.565-.132-.84-.2l-1.815-.45-.35 1.407s.975.225.955.236c.535.136.63.486.615.766l-1.477 5.92c-.075.166-.24.406-.614.314.015.02-.96-.24-.96-.24l-.66 1.51 1.71.426.93.242-.54 2.19 1.32.327.54-2.17c.36.1.705.19 1.05.273l-.51 2.154 1.32.33.545-2.19c2.24.427 3.93.257 4.64-1.774.57-1.637-.03-2.58-1.217-3.196.854-.193 1.5-.76 1.68-1.93h.01zm-3.01 4.22c-.404 1.64-3.157.75-4.05.53l.72-2.9c.896.23 3.757.67 3.33 2.37zm.41-4.24c-.37 1.49-2.662.735-3.405.55l.654-2.64c.744.18 3.137.524 2.75 2.084v.006z',
  },
  eth: {
    box: '0 0 24 24',
    d: 'M11.944 17.97L4.58 13.62 11.943 24l7.37-10.38-7.372 4.35h.003zM12.056 0L4.69 12.223l7.365 4.354 7.365-4.35L12.056 0z',
  },
  usdt: {
    box: '0 0 24 24',
    d: 'M18.7538 10.5176c0 .6251-2.2379 1.1483-5.2381 1.2812l.0028.0007c-.0848.0064-.5233.0325-1.5012.0325-.7778 0-1.33-.0233-1.5237-.0325-3.0059-.1322-5.2495-.6555-5.2495-1.2819s2.2436-1.149 5.2495-1.2834v2.0442c.1965.0142.7594.0474 1.5372.0474.9334 0 1.4008-.0389 1.4849-.0466V9.2356c2.9994.1337 5.2381.657 5.2381 1.282zm5.19.5466L12.1248 22.389a.1803.1803 0 0 1-.2496 0L.0562 11.0635a.1781.1781 0 0 1-.0382-.2079l4.3762-9.1921a.1767.1767 0 0 1 .1626-.1026h14.8878a.1768.1768 0 0 1 .1612.1032l4.3762 9.1922a.1782.1782 0 0 1-.0382.2079zm-4.478-.4038c0-.8068-2.5515-1.4799-5.9473-1.6369V7.195h4.186V4.4055H6.3076V7.195h4.1852v1.8286c-3.4018.1562-5.9601.83-5.9601 1.6376 0 .8075 2.5583 1.4806 5.9601 1.6376v5.8618h3.025v-5.8639c3.394-.1563 5.948-.8295 5.948-1.6363z',
  },
  usdc: {
    box: '0 0 32 32',
    d: 'M16 0c8.837 0 16 7.163 16 16s-7.163 16-16 16S0 24.837 0 16 7.163 0 16 0zm3.352 5.56c-.244-.12-.488 0-.548.243-.061.061-.061.122-.061.243v.85l.01.104a.86.86 0 00.355.503c4.754 1.7 7.192 6.98 5.424 11.653-.914 2.55-2.925 4.491-5.424 5.402-.244.121-.365.303-.365.607v.85l.005.088a.45.45 0 00.36.397c.061 0 .183 0 .244-.06a10.895 10.895 0 007.13-13.717c-1.096-3.46-3.778-6.07-7.13-7.162zm-6.46-.06c-.061 0-.183 0-.244.06a10.895 10.895 0 00-7.13 13.717c1.096 3.4 3.717 6.01 7.13 7.102.244.121.488 0 .548-.243.061-.06.061-.122.061-.243v-.85l-.01-.08c-.042-.169-.199-.362-.355-.466-4.754-1.7-7.192-6.98-5.424-11.653.914-2.55 2.925-4.491 5.424-5.402.244-.121.365-.303.365-.607v-.85l-.005-.088a.45.45 0 00-.36-.397zm3.535 3.156h-.915l-.088.008c-.2.04-.346.212-.4.478v1.396l-.207.032c-1.708.304-2.778 1.483-2.778 2.942 0 2.002 1.218 2.791 3.778 3.095 1.707.303 2.255.668 2.255 1.639 0 .97-.853 1.638-2.011 1.638-1.585 0-2.133-.667-2.316-1.578-.06-.242-.244-.364-.427-.364h-1.036l-.079.007a.413.413 0 00-.347.418v.06l.033.18c.29 1.424 1.266 2.443 3.197 2.734v1.457l.008.088c.04.198.213.344.48.397h.914l.088-.008c.2-.04.346-.212.4-.477V21.34l.207-.04c1.713-.362 2.84-1.601 2.84-3.177 0-2.124-1.28-2.852-3.84-3.156-1.829-.243-2.194-.728-2.194-1.578 0-.85.61-1.396 1.828-1.396 1.097 0 1.707.364 2.011 1.275a.458.458 0 00.427.303h.975l.079-.006a.413.413 0 00.348-.419v-.06l-.037-.173a3.04 3.04 0 00-2.706-2.316V9.142l-.008-.088c-.04-.199-.213-.345-.48-.398z',
  },
  bnb: {
    box: '0 0 24 24',
    d: 'M16.624 13.9202l2.7175 2.7154-7.353 7.353-7.353-7.352 2.7175-2.7164 4.6355 4.6595 4.6356-4.6595zm4.6366-4.6366L24 12l-2.7154 2.7164L18.5682 12l2.6924-2.7164zm-9.272.001l2.7163 2.6914-2.7164 2.7174v-.001L9.2721 12l2.7164-2.7154zm-9.2722-.001L5.4088 12l-2.6914 2.6924L0 12l2.7164-2.7164zM11.9885.0115l7.353 7.329-2.7174 2.7154-4.6356-4.6356-4.6355 4.6595-2.7174-2.7154 7.353-7.353z',
  },
  sol: {
    box: '0 0 24 24',
    d: 'm23.8764 18.0313-3.962 4.1393a.9201.9201 0 0 1-.306.2106.9407.9407 0 0 1-.367.0742H.4599a.4689.4689 0 0 1-.2522-.0733.4513.4513 0 0 1-.1696-.1962.4375.4375 0 0 1-.0314-.2545.4438.4438 0 0 1 .117-.2298l3.9649-4.1393a.92.92 0 0 1 .3052-.2102.9407.9407 0 0 1 .3658-.0746H23.54a.4692.4692 0 0 1 .2523.0734.4531.4531 0 0 1 .1697.196.438.438 0 0 1 .0313.2547.4442.4442 0 0 1-.1169.2297zm-3.962-8.3355a.9202.9202 0 0 0-.306-.2106.941.941 0 0 0-.367-.0742H.4599a.4687.4687 0 0 0-.2522.0734.4513.4513 0 0 0-.1696.1961.4376.4376 0 0 0-.0314.2546.444.444 0 0 0 .117.2297l3.9649 4.1394a.9204.9204 0 0 0 .3052.2102c.1154.049.24.0744.3658.0746H23.54a.469.469 0 0 0 .2523-.0734.453.453 0 0 0 .1697-.1961.4382.4382 0 0 0 .0313-.2546.4444.4444 0 0 0-.1169-.2297zM.46 6.7225h18.7815a.9411.9411 0 0 0 .367-.0742.9202.9202 0 0 0 .306-.2106l3.962-4.1394a.4442.4442 0 0 0 .117-.2297.4378.4378 0 0 0-.0314-.2546.453.453 0 0 0-.1697-.196.469.469 0 0 0-.2523-.0734H4.7596a.941.941 0 0 0-.3658.0745.9203.9203 0 0 0-.3052.2102L.1246 5.9687a.4438.4438 0 0 0-.1169.2295.4375.4375 0 0 0 .0312.2544.4512.4512 0 0 0 .1692.196.4689.4689 0 0 0 .2518.0739z',
  },
  sui: {
    box: '0 0 24 24',
    d: 'M17.636 10.009a7.16 7.16 0 0 1 1.565 4.474 7.2 7.2 0 0 1-1.608 4.53l-.087.106-.023-.135a7 7 0 0 0-.07-.349c-.502-2.21-2.142-4.106-4.84-5.642-1.823-1.034-2.866-2.278-3.14-3.693-.177-.915-.046-1.834.209-2.62.254-.787.631-1.446.953-1.843l1.05-1.284a.46.46 0 0 1 .713 0l5.28 6.456zm1.66-1.283L12.26.123a.336.336 0 0 0-.52 0L4.704 8.726l-.023.029a9.33 9.33 0 0 0-2.07 5.872C2.612 19.803 6.816 24 12 24s9.388-4.197 9.388-9.373a9.32 9.32 0 0 0-2.07-5.871zM6.389 9.981l.63-.77.018.142q.023.17.055.34c.408 2.136 1.862 3.917 4.294 5.297 2.114 1.203 3.345 2.586 3.7 4.103a5.3 5.3 0 0 1 .109 1.801l-.004.034-.03.014A7.2 7.2 0 0 1 12 21.67c-3.976 0-7.2-3.218-7.2-7.188 0-1.705.594-3.27 1.587-4.503z',
  },
  xrp: {
    // The current XRP mark; Ripple's former wave logo reads as brackets at 22px.
    box: '0 0 32 32',
    d: 'M16 32C7.163 32 0 24.837 0 16S7.163 0 16 0s16 7.163 16 16-7.163 16-16 16zm7.07-24l-4.574 4.523a3.556 3.556 0 01-4.996 0L8.93 8H6.035l6.02 5.957a5.621 5.621 0 007.89 0L25.961 8h-2.89zM8.895 24.563L13.504 20a3.556 3.556 0 014.996 0l4.605 4.563H26l-6.055-5.993a5.621 5.621 0 00-7.89 0L6 24.562h2.895z',
  },
};

/** Bitcoin's letterform with the disc cut away, for the button that opens the
 *  window: at 16px the disc would read as an orange dot rather than a letter. */
const BTC_LETTER = {
  box: '3.961 4.178 15.2 15.2',
  d: COIN_MARKS.btc.d.slice(COIN_MARKS.btc.d.indexOf('m-6.35-4.613')).replace('m-6.35-4.613', 'M17.288 10.291'),
};
