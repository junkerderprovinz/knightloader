// The donation addresses for the crypto window, the web UI's list
// (web/src/lib/donate.ts). check-donate.mjs holds the two to each other, since
// an address that drifts on one surface only fails nobody's test and loses a
// stranger's money.
//
// Grouped by coin and then by chain, and every chain a donor can pick carries
// its own address, so a coin can never be sent over a chain where the address
// does not exist. A chain without an address is simply absent.

/** One address, and the chain it lives on. */
export interface CryptoNetwork {
  id: string;
  /** The chain as a donor's wallet names it. */
  name: string;
  address: string;
  /** A short line shown with the address, for what a donor has to know. */
  noteKey?: 'settings.cryptoNoTag';
}

/** One coin, and every chain it can be sent on here. */
export interface CryptoCoin {
  /** Stable id, and the key the coin's mark is drawn by. */
  id: 'btc' | 'eth' | 'usdt' | 'usdc' | 'bnb' | 'sol' | 'sui' | 'xrp';
  /** The ticker, on the tile under the mark. */
  symbol: string;
  /** The full name, for the accessible label. */
  name: string;
  /** Never empty, and every entry carries an address. */
  networks: CryptoNetwork[];
}

// The five wallets, each written once and shared by the networks below.
const BTC = 'bc1q078lt57t4n5zq5md3knz3ythum0w78zmjw5eda';
/** One address for every EVM chain: the same key controls it on all of them. */
const EVM = '0xFF6726C5bd76C8FD6b6bE7Ea5CEd4621fde5e841';
const SOL = 'GrTyhSbZVArdaZAr3TqWDrkEGahomLtNZJ41qPLm3dHd';
const SUI = '0xa76677f71d107c9a957c2d1814b68027cbd4ad9dbf28d911a97fad5d2fc3a414';
const XRP = 'rwMK2nXqChT4JYWVypMypnpctDcM9jgWmG';

const ETHEREUM: CryptoNetwork = { id: 'ethereum', name: 'Ethereum', address: EVM };
const BASE: CryptoNetwork = { id: 'base', name: 'Base', address: EVM };
const OPTIMISM: CryptoNetwork = { id: 'optimism', name: 'Optimism', address: EVM };
const BSC: CryptoNetwork = { id: 'bsc', name: 'BNB Smart Chain', address: EVM };
const SOLANA: CryptoNetwork = { id: 'solana', name: 'Solana', address: SOL };

export const CRYPTO_COINS: CryptoCoin[] = [
  { id: 'btc', symbol: 'BTC', name: 'Bitcoin', networks: [{ id: 'bitcoin', name: 'Bitcoin', address: BTC }] },
  // Native ETH only. On BNB Smart Chain, ETH is a bridged token.
  { id: 'eth', symbol: 'ETH', name: 'Ethereum', networks: [ETHEREUM, BASE, OPTIMISM] },
  { id: 'usdt', symbol: 'USDT', name: 'Tether', networks: [ETHEREUM, BSC, SOLANA] },
  { id: 'usdc', symbol: 'USDC', name: 'USD Coin', networks: [ETHEREUM, BASE, SOLANA] },
  { id: 'bnb', symbol: 'BNB', name: 'BNB', networks: [BSC] },
  { id: 'sol', symbol: 'SOL', name: 'Solana', networks: [SOLANA] },
  { id: 'sui', symbol: 'SUI', name: 'Sui', networks: [{ id: 'sui', name: 'Sui', address: SUI }] },
  {
    id: 'xrp',
    symbol: 'XRP',
    name: 'XRP',
    networks: [
      // Exchanges often require a destination tag; this self-custody account
      // does not (RequireDest is off), and the window says so beside it.
      { id: 'xrpl', name: 'XRP Ledger', address: XRP, noteKey: 'settings.cryptoNoTag' },
    ],
  },
];

/**
 * Which wallet each chain must resolve to, written out separately so the check
 * compares the list with something other than itself.
 */
export const ADDRESS_BY_CHAIN: Record<string, string> = {
  bitcoin: BTC,
  ethereum: EVM,
  base: EVM,
  optimism: EVM,
  bsc: EVM,
  solana: SOL,
  sui: SUI,
  xrpl: XRP,
};
