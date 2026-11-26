// The downloads screen says why it is not connected, and reads what it could
// not read before once it is.
//
// A phone that waits for approval gets every call refused with memberPending.
// The screen shows that refusal under its "connecting" line rather than
// "connecting" alone, and when the list comes through after the approval it
// reads the saved views, the card switches and the skipped links again, and
// tells the app, which reads the instance's look again. A transport failure
// leaves the line as it is.
//
// Run by hand and by CI, from mobile/: `node check-downloads-connect.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const { ApiError, errorText } = compile(join(here, 'src', 'api', 'refusal.ts'), {});
const reads = { views: 0, cards: 0, skipped: 0 };
let poll = null;
const nothing = async () => {};

const { default: DownloadsScreen } = compile(join(here, 'src', 'screens', 'DownloadsScreen.tsx'), {
  'react-native': { ActivityIndicator: 'ActivityIndicator', StyleSheet: { create: (s) => s }, TouchableOpacity: 'TouchableOpacity', View: 'View' },
  '../api/client': {
    ApiError,
    errorText,
    fetchListCards: async () => {
      reads.cards++;
      return { finished: true, torrents: true };
    },
    fetchQueue: async () => ({ halted: false, running: 0 }),
    fetchSpeedHistory: async () => ({ down: [], up: [] }),
    pollTasks: (_conn, _base, onSnapshot, onError) => {
      poll = { onSnapshot, onError };
      return Object.assign(() => {}, { refresh: nothing });
    },
    setQueueHalted: nothing,
    stopAll: nothing,
    fetchTorrentFiles: nothing,
    reorderTasks: nothing,
    selectTorrentFiles: nothing,
    setSeeding: nothing,
    setTasksEnabled: nothing,
    startTasks: nothing,
  },
  '../api/taskState': { splitByCard: (q) => ({ downloads: q, finished: [], torrents: [] }), unpackingByTask: () => ({}) },
  '../api/types': { needsAddingAgain: () => false },
  '../api/play': { playTask: nothing },
  '../components/SaveCard': { useSaveToPhone: () => ({ card: null, save() {}, saveFile() {} }) },
  '../components/PackageList': { __esModule: true, default: 'PackageList' },
  '../components/TaskActions': { useTaskActions: () => ({ retryFailed: null, remove() {}, open() {}, layer: null }) },
  '../components/TaskEdit': { TaskEdit: 'TaskEdit' },
  '../components/glim': { GlimButton: 'GlimButton', WellSelector: 'WellSelector' },
  '../components/CaptchaWatch': { useCaptchas: () => ({ list: [] }) },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, accent: '', accentInk: '', accentContrast: '', corners: {} }) },
  '../theme/tokens': { TYPE: {} },
  '../i18n/I18nContext': { useT: () => ({ t: (key) => key }) },
  '../components/IconBadge': { __esModule: true, default: 'IconBadge', Back: 'Back', SpeedLimit: 'SpeedLimit', Trash: 'Trash' },
  '../components/InfoTip': { Tip: 'Tip' },
  '../components/SpeedGraph': { __esModule: true, default: 'SpeedGraph' },
  '../components/speedPlot': { uploadOf: () => 0 },
  '../components/Text': { Text: 'Text' },
  '../components/Moving': { Arrive: 'Arrive' },
  '../components/ListNarrowing': { ListNarrowing: 'ListNarrowing' },
  '../components/NotAdded': { HeldLinks: 'HeldLinks', SkippedLinks: 'SkippedLinks' },
  '../components/QueueLimits': { QueueLimits: 'QueueLimits' },
  '../api/narrowing': { COLLECTOR_FILTERS: [], DOWNLOAD_FILTERS: [], NOTHING_NARROWED: {}, isNarrowed: () => false, narrow: (l) => l },
  '../api/notAdded': { heldOf: () => [] },
  '../api/listActions': {
    clearHeld: nothing,
    clearSkipped: nothing,
    fetchSkipped: async () => {
      reads.skipped++;
      return [];
    },
    fetchViews: async () => {
      reads.views++;
      return { downloads: [], collector: [] };
    },
    putViews: nothing,
    restoreHeld: nothing,
  },
  '../api/savedViews': { sanitiseViews: (v) => v ?? [] },
  '../components/SavedViews': { SavedViews: 'SavedViews' },
  '../theme/MotionContext': { usePress: () => ({ scale: 1, onPressIn() {}, onPressOut() {} }) },
});

const conn = { kind: 'relay', id: 'c1', name: 'Bottich', relayUrl: 'wss://r', relayKey: 'k', relayFrameKey: 'f', instanceId: 'i1' };
let reached = 0;
const screen = mount(() =>
  DownloadsScreen({
    conn,
    onAddPress() {},
    onSwitchConnection() {},
    onOpenCaptchas() {},
    onReached: () => reached++,
  }),
);
const words = () => screen.shown().words;
await screen.settle();

poll.onError(new ApiError('this device waits for approval on an instance of the group', 403, 'memberPending'));
await screen.settle();
expect('a pending phone is told so under the connecting line', words().includes('error.memberPending'), true);
expect('and still reads as connecting', words().includes('downloads.connecting'), true);
const before = { ...reads };

poll.onSnapshot([]);
await screen.settle();
expect('the approved phone is connected', words().includes('downloads.connecting'), false);
expect('and the refusal is gone', words().includes('error.memberPending'), false);
expect('the saved views are read again', reads.views > before.views, true);
expect('the card switches are read again', reads.cards > before.cards, true);
expect('the skipped links are read again', reads.skipped > before.skipped, true);
expect('and the app hears of it once', reached, 1);

const settled = { ...reads };
poll.onSnapshot([]);
await screen.settle();
expect('a second list reads nothing again', reads, settled);
expect('and the app hears nothing more', reached, 1);

poll.onError(new Error('relay: not connected'));
await screen.settle();
expect('a dropped connection says connecting', words().includes('downloads.connecting'), true);
expect('with no refusal under it', words().some((w) => w.startsWith('error.')), false);
poll.onSnapshot([]);
await screen.settle();
expect('coming back reads them again', reads.views > settled.views, true);
expect('and tells the app again', reached, 2);
screen.unmount();

if (problems.length) {
  console.error('The downloads screen hides why it is not connected or misses what came after:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log('Downloads connecting: a refusal is named, and views, cards, skipped links and the look are read again once connected.');
