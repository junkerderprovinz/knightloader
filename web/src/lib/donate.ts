// The donation addresses, grouped by coin and then by chain.
//
// Every chain a donor can pick carries its own address, so a coin can never
// be sent to a chain where the address does not exist (an EVM address is not
// a Tron or Solana address). Chains without an address, such as Tron, are
// simply absent.
//
// check-donate-addresses.mjs checks each format: the Bitcoin bech32 checksum,
// the XRP base58check, the Solana key length, and the EVM and Sui hex. The XRP
// account exists on the ledger, so a donation to it cannot bounce.

/** One address, and the chain it lives on. */
export interface CryptoNetwork {
  /** Stable id, for the copy toast and for tests. */
  id: string;
  /** The chain as a donor's wallet names it. */
  name: string;
  address: string;
  /** A short line shown with the address, for what a donor has to know. */
  noteKey?: 'settings.about.cryptoNoTag';
}

/** One coin, and every chain it can be sent on here. */
export interface CryptoCoin {
  /** Stable id, and the key components/donateMarks.tsx draws the mark by. */
  id: string;
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

const ETHEREUM = { id: 'ethereum', name: 'Ethereum', address: EVM };
const BASE = { id: 'base', name: 'Base', address: EVM };
const OPTIMISM = { id: 'optimism', name: 'Optimism', address: EVM };
const BSC = { id: 'bsc', name: 'BNB Smart Chain', address: EVM };
const SOLANA = { id: 'solana', name: 'Solana', address: SOL };

export const CRYPTO_COINS: CryptoCoin[] = [
  {
    id: 'btc',
    symbol: 'BTC',
    name: 'Bitcoin',
    networks: [{ id: 'bitcoin', name: 'Bitcoin', address: BTC }],
  },
  {
    id: 'eth',
    symbol: 'ETH',
    name: 'Ethereum',
    // Native ETH only. On BNB Smart Chain, ETH is a bridged token.
    networks: [ETHEREUM, BASE, OPTIMISM],
  },
  {
    id: 'usdt',
    symbol: 'USDT',
    name: 'Tether',
    networks: [ETHEREUM, BSC, SOLANA],
  },
  {
    id: 'usdc',
    symbol: 'USDC',
    name: 'USD Coin',
    networks: [ETHEREUM, BASE, SOLANA],
  },
  {
    id: 'bnb',
    symbol: 'BNB',
    name: 'BNB',
    networks: [BSC],
  },
  {
    id: 'sol',
    symbol: 'SOL',
    name: 'Solana',
    networks: [SOLANA],
  },
  {
    id: 'sui',
    symbol: 'SUI',
    name: 'Sui',
    networks: [{ id: 'sui', name: 'Sui', address: SUI }],
  },
  {
    id: 'xrp',
    symbol: 'XRP',
    name: 'XRP',
    networks: [
      {
        id: 'xrpl',
        name: 'XRP Ledger',
        address: XRP,
        // Exchanges often require a destination tag; this self-custody account
        // does not (RequireDest is off).
        noteKey: 'settings.about.cryptoNoTag',
      },
    ],
  },
];

/**
 * Which wallet each chain must resolve to, written out separately so the test
 * checks the list against something other than itself.
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
