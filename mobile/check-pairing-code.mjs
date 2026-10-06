// The app reads the QR code beside the phrase the way the server writes it.
//
// relay.PairingCode puts the instance's own relay on a second line, and
// src/api/pairingCode.ts reads it back. Every row of the Go test's table,
// TestPairingCode in internal/relay/key_test.go, is scanned here: a code
// without a second line pairs on the project relay, a wss:// relay is taken
// over, and anything else is refused rather than dialled, since the app
// permits no cleartext traffic. The connect screen then keeps searching the
// relay a scanned code named until the words are typed or pasted.
//
// Run by hand and by CI, from mobile/: `node check-pairing-code.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const { readPairingCode } = await import(pathToFileURL(join(here, 'src', 'api', 'pairingCode.ts')).href);
const goTest = readFileSync(join(here, '..', 'internal', 'relay', 'key_test.go'), 'utf8');

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const phrase = goTest.match(/^func TestPairingCode\(t \*testing\.T\) \{\n\tconst phrase = "([^"]+)"/m)?.[1];
const table = goTest.match(/^func TestPairingCode\([\s\S]*?^\}$/m);
if (!phrase || !table) {
  console.error('TestPairingCode was not found; the patterns in this check no longer match internal/relay/key_test.go');
  process.exit(1);
}

const rows = [...table[0].matchAll(/\{"([^"]*)", [^,]+, phrase(?: \+ "\\n([^"]*)")?\}/g)];
if (rows.length === 0) problems.push('TestPairingCode has no rows this check can read');
for (const [, name, relay] of rows) {
  const code = relay === undefined ? phrase : `${phrase}\n${relay}`;
  const want =
    relay === undefined
      ? { words: phrase }
      : relay.startsWith('wss://')
        ? { words: phrase, relay }
        : { words: phrase, refused: relay };
  expect(name, readPairingCode(code), want);
}

const relay = 'wss://relay.example.com/relay/connect';
expect('words typed on several lines are still words', readPairingCode(phrase.replaceAll(' ', '\n')), {
  words: phrase.replaceAll(' ', '\n'),
});
expect('a relay on the same line', readPairingCode(`${phrase} ${relay}`), { words: phrase, relay });
expect('a trailing line break', readPairingCode(`${phrase}\n${relay}\n`), { words: phrase, relay });
expect('the scheme in capitals', readPairingCode(`${phrase}\nWSS://relay.example.com`), {
  words: phrase,
  relay: 'wss://relay.example.com',
});
for (const address of [
  'ws://relay.example.com/relay/connect',
  'http://relay.example.com',
  'https://relay.example.com',
  'wss://',
  'wss://user@relay.example.com',
  'ftp://relay.example.com',
]) {
  expect(`${address} is refused`, readPairingCode(`${phrase}\n${address}`), { words: phrase, refused: address });
}

// The connect screen keeps the relay a scanned code names. When the instance
// has not answered by the time the spinner stops, Connect is what somebody
// presses next, and it has to search the same relay again rather than the
// project relay, where that instance never shows up.
{
  const DEFAULT = 'wss://project.example/relay/connect';
  const dialled = [];
  let clipboard = '';
  const split = (p) => p.split(/[\s,]+/).filter(Boolean);
  const { default: RelayConnectScreen } = compile(join(here, 'src', 'screens', 'RelayConnectScreen.tsx'), {
    'react-native': { Animated: { View: 'Animated.View' }, StyleSheet: { create: (s) => s }, View: 'View' },
    'expo-clipboard': { getStringAsync: async () => clipboard },
    '../components/QRScanner': { __esModule: true, default: 'QRScanner' },
    '../api/relayClient': {
      closeRelayClient() {},
      relayClientFor: ({ url }) => {
        dialled.push(url);
        return { subscribe: () => () => {}, siblings: () => [] };
      },
    },
    '../api/seedphrase': {
      DEFAULT_RELAY_URL: DEFAULT,
      PhraseError: class extends Error {},
      WORD_COUNT: 12,
      keyFromPhrase: () => 'key',
      frameKeyFromPhrase: () => new Uint8Array(32),
    },
    '../api/phraseWords': {
      splitPhrase: split,
      checkPhrase: (p) => ({ words: split(p), unknown: [], complete: split(p).length === 12 }),
    },
    '../api/pairingCode': { readPairingCode },
    '../api/deviceName': { deviceName: () => 'Phone' },
    '../api/sha256': { toHex: () => '' },
    '../storage/relayIdentity': { relayIdentity: async () => 'phone' },
    '../storage/connections': { addConnection: async () => {}, listConnections: async () => [], setActiveConnectionId: async () => {} },
    '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, accent: '', corners: {} }) },
    '../theme/MotionContext': { useShake: () => ({ style: {}, shake() {} }) },
    '../theme/tokens': { NUM: {}, TYPE: {} },
    '../i18n/I18nContext': { useT: () => ({ t: (key) => key }) },
    '../components/glim': { GlimButton: 'GlimButton' },
    '../components/IconBadge': {
      __esModule: true,
      default: 'IconBadge',
      Back: 'Back',
      Connect: 'Connect',
      Paste: 'Paste',
      Scan: 'Scan',
      boxForInk: (n) => n,
    },
    '../components/InfoTip': { InfoTip: 'InfoTip' },
    '../components/Moving': { MovingScroll: 'MovingScroll' },
    '../components/Text': { Text: 'Text', TextInput: 'TextInput' },
  });

  const screen = mount(() => RelayConnectScreen({ onConnected() {}, onBack() {} }));
  const find = (shown, test) => shown.elements.find((e) => test(e.type, e.props))?.props;
  const scan = async (code) => {
    find(screen.shown(), (type) => type === 'QRScanner').onScanned(code);
    return screen.settle();
  };
  const press = async (label) => {
    find(screen.shown(), (type, props) => type === 'GlimButton' && props.label === label).onPress();
    return screen.settle();
  };
  const last = () => dialled.at(-1);

  await screen.settle();
  await scan(`${phrase}\n${relay}`);
  expect('a scanned code searches the relay it names', last(), relay);
  await press('relay.joinButton');
  expect('Connect after a scan searches the relay the code named', last(), relay);
  await scan(phrase);
  await press('relay.joinButton');
  expect('Connect after a code without a relay searches the project relay', last(), DEFAULT);
  await scan(`${phrase}\n${relay}`);
  find(screen.shown(), (type) => type === 'TextInput').onChangeText(phrase);
  await screen.settle();
  await press('relay.joinButton');
  expect('typed words search the project relay', last(), DEFAULT);
  await scan(`${phrase}\n${relay}`);
  clipboard = phrase;
  await press('relay.pasteButton');
  await press('relay.joinButton');
  expect('pasted words search the project relay', last(), DEFAULT);
  screen.unmount();
}

if (problems.length) {
  console.error('The app reads the pairing code differently from how the server writes it:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Pairing codes: ${rows.length} server cases and the app's own read as written, and Connect keeps the scanned relay.`);
