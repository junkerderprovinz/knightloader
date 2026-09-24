import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { fetchQueue, liveTasks, setQueueHalted, stopAll, type LiveTasks } from '../api/client';
import type { Instance, QueueState, ServerConnection, Task } from '../api/types';
import PackageList from '../components/PackageList';
import { WellSelector } from '../components/glim';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import IconBadge, { Back, Trash } from '../components/IconBadge';
import SpeedGraph from '../components/SpeedGraph';
import { fmtBytes } from '../api/stats';
import { deleteTasks, reorderTasks, startTasks } from '../api/client';

// peer, when set, means this screen is showing a federation peer of conn rather
// than conn's own queue, so base becomes the proxy prefix (/api/instances/{name},
// see internal/api/routes_federation.go).
//
// Whether the task list then streams or polls is liveTasks' decision: a
// federation peer and a relay connection both forward plain REST calls with no
// socket to attach to, and only api/client.ts knows which is in play.
export default function DownloadsScreen({
  conn,
  peer,
  onAddPress,
  onSwitchConnection,
  onBackToOwn,
  onRemoveConnection,
}: {
  conn: ServerConnection;
  peer?: Instance;
  onAddPress: () => void;
  onSwitchConnection: () => void;
  onBackToOwn?: () => void;
  /** Undefined for a federation peer: a peer is not a saved connection, so
   *  there is nothing here to remove. */
  onRemoveConnection?: () => void;
}) {
  const { t } = useT();
  const { c, accent, accentInk, accentContrast, radii } = useAppearance();
  const base = peer ? `/api/instances/${encodeURIComponent(peer.name)}` : '/api';
  const [tasks, setTasks] = useState<Task[]>([]);
  const [connected, setConnected] = useState(false);
  const [queue, setQueue] = useState<QueueState | null>(null);
  const [queueBusy, setQueueBusy] = useState(false);
  /** What the last start or stop did, when it did not work. Shown rather than
   *  swallowed, the same line the overview carries: a control that reports
   *  nothing cannot be told apart from one that does nothing. */
  const [startError, setStartError] = useState('');
  // Which half of the instance is on screen. The two are one task list with one
  // status telling them apart, since "collected" means staged and not started,
  // so this is a filter over what already streams rather than a second request.
  const [tab, setTab] = useState<'downloads' | 'collector'>('downloads');
  // Summed from the task list this screen already streams, so there is no
  // second request and no second truth about the same number.
  const speed = tasks.reduce((n, t) => n + (t.speed || 0), 0);
  const collected = tasks.filter((x) => x.status === 'collected' && !x.variantOff);
  const queued = tasks.filter((x) => x.status !== 'collected');

  /** The live handle, kept so an action that just changed something on the
   *  server can ask for the truth immediately instead of waiting out the
   *  polling cycle. */
  const live = useRef<LiveTasks | null>(null);

  useEffect(() => {
    setConnected(false);
    setTasks([]);
    const onSnapshot = (snapshot: Task[]) => {
      setConnected(true);
      setTasks(snapshot);
    };
    const onError = () => setConnected(false);
    const handle = liveTasks(conn, base, onSnapshot, onError);
    live.current = handle;
    return () => {
      live.current = null;
      handle();
    };
  }, [conn, base, peer]);

  useEffect(() => {
    let alive = true;
    setQueue(null);
    const load = () => fetchQueue(conn, base).then((q) => alive && setQueue(q)).catch(() => {});
    load();
    const iv = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, [conn, base]);

  // try/finally with no catch clears the spinner and sends the rejection
  // nowhere, so a queue call the instance refused looks like a button that was
  // never wired up.
  //
  // Stopping calls /api/queue/stop rather than /api/queue with halted:true.
  // Halting stops the dispatcher and lets whatever is already downloading run
  // to the end, which is the right default for the server and the wrong verb
  // for this button, because the bar somebody is watching keeps moving. See
  // stopAll() in api/client.ts. Starting is the plain release, since there is
  // no second kind of start.
  const toggleHalted = async (nextHalted: boolean) => {
    setQueueBusy(true);
    setStartError('');
    try {
      setQueue(nextHalted ? await stopAll(conn, base) : await setQueueHalted(conn, false, base));
    } catch (e) {
      setStartError(e instanceof Error ? e.message : String(e));
    } finally {
      setQueueBusy(false);
    }
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        {/* The way out is a badge to the left of the name, as in Settings. A
            text button naming a destination is a second shape for the one
            meaning this app already draws one way. */}
        <IconBadge icon={<Back color={c.textSub} />} onPress={peer && onBackToOwn ? onBackToOwn : onSwitchConnection} accessibilityLabel={t('settings.back')} />
        <View style={styles.topBarLeft}>
          <Text style={[styles.title, { color: c.text }]}>{peer ? (peer.displayName ?? peer.name) : conn.name}</Text>
          {/* Only while it is not connected. Connected is the ordinary case, so
              a label saying so is one nobody reads. Still connecting, or
              dropped, is worth saying. */}
          {!connected && (
            <Text style={[styles.connState, { color: c.statusWarnSolid }]}>
              {t('downloads.connecting')}
            </Text>
          )}
        </View>
        <View style={styles.topBarRight}>
          {/* Removing this connection belongs here, on the thing being removed.
              On the overview it would sit on every row of a list somebody taps
              to open, which is a mis-tap waiting to happen.

              It is the only badge on this side. Settings are not a property of
              one instance, so the gear lives on the overview alone. */}
          {!peer && onRemoveConnection && (
            <IconBadge
              icon={<Trash color={c.textSub} />}
              onPress={onRemoveConnection}
              accessibilityLabel={t('connections.remove')}
            />
          )}
        </View>
      </View>

      {/* Grouped into packages rather than one row per link. A container is one
          thing somebody added, and a flat list of its hundred files says
          nothing about what was added, which is why the web interface and
          JDownloader group the same way.

          Everything above the rows travels as this list's header rather than as
          its siblings. As siblings, each piece carries its own copy of the
          list's width cap, centring and margins, four places to keep in step;
          inside the content container there are none. */}
      <PackageList
        tasks={tab === 'collector' && collected.length > 0 ? collected : queued}
        empty={connected ? t('downloads.empty') : t('downloads.emptyConnecting')}
        header={
          <>
            {/* One card, figures and curve together. A graph sitting below the
                queue bar with a surface and a radius of its own is a second
                card by every property that makes something look like one, and
                on a tablet the two stand side by side. The overview's summary
                card holds both in one box, and this is the same reading of the
                same instance. */}
            <View style={[styles.queueCard, { backgroundColor: c.surface, borderRadius: radii.card }]}>
              <View style={styles.queueBar}>
                <Text style={[styles.queueLabel, { color: c.textMuted }]}>
                  {queue ? (queue.halted ? t('downloads.queueHalted') : t('downloads.queueRunning')) : '-'}
                  {queue && queue.running > 0 ? ` · ${t('downloads.queueActive', { n: queue.running })}` : ''}
                  {speed > 0 ? ` · ${fmtBytes(speed)}/s` : ''}
                </Text>
                {/* BOTH options, always on screen, with only the one in force
                    filled - the same control the overview's own summary card
                    uses, and the same one the browser extension has drawn since
                    it shipped (shared.js: start and stop as two separate
                    actions).

                    It was ONE badge whose glyph flipped with the state, and the
                    argument written here for it - that such a badge can never
                    offer the thing that is already true - is the argument the
                    design language answers directly: the person is then asked
                    to infer the alternative from a glyph that is not on the
                    screen, which is fine for a preference and not for anything
                    with a consequence. Stopping a download queue has one. Two
                    surfaces of one product also disagreed about it, which is
                    how it was found.

                    Filled means "this is what the queue is doing", not "press
                    me": a two-option pair is a selector with icon-only
                    segments, and in a selector the fill marks the value that is
                    in force. Pressing the filled one is a no-op. Stopping is
                    the HARD stop: see toggleHalted. */}
                {queueBusy ? (
                  <ActivityIndicator color={accentInk} size="small" />
                ) : (
                  <View style={styles.queueActions}>
                    <IconBadge
                      symbol="▶"
                      accent={queue?.halted === false}
                      onPress={() => queue?.halted && toggleHalted(false)}
                      accessibilityLabel={t('downloads.start')}
                    />
                    <IconBadge
                      symbol="■"
                      accent={queue?.halted === true}
                      onPress={() => queue?.halted === false && toggleHalted(true)}
                      accessibilityLabel={t('downloads.stop')}
                    />
                  </View>
                )}
              </View>

              {/* Shown whenever something is in the queue rather than only
                  while bytes are moving. Testing `speed > 0` is right for an
                  empty instance and wrong for a queue that says "running" while
                  nothing moves, where a line flat at zero is the answer. */}
              {(speed > 0 || queued.length > 0) && <SpeedGraph speed={speed} />}
            </View>

            {/* Why the last start or stop did not take. One line, in the fail
                colour, only when there is something to say. Outside the card,
                because it is about the last press rather than about the queue,
                the placement the overview gives its own. */}
            {startError !== '' && (
              <Text style={[styles.queueError, { color: c.statusFailSolid }]} numberOfLines={2}>
                {startError}
              </Text>
            )}

            {/* The strip only appears once there is something staged: a tab
                that is always empty teaches people to ignore the strip. */}
            {collected.length > 0 && (
              <View style={styles.tabs}>
                <WellSelector
                  options={[
                    { value: 'downloads', label: t('downloads.tabDownloads') },
                    { value: 'collector', label: `${t('downloads.tabCollector')} (${collected.length})` },
                  ]}
                  value={tab}
                  onPick={(v) => setTab(v)}
                />
              </View>
            )}
          </>
        }
        onStartPackage={
          tab === 'collector' && collected.length > 0
            ? async (pkg) => {
                // Straight into the queue and out of this tab. Switching tabs
                // first would leave somebody looking at a collector one package
                // emptier for no visible reason.
                //
                // A refused start is a line, and the tab only changes when
                // there is something to see in it. Swallowing the rejection
                // would switch the tab while the package stayed where it was,
                // with nothing on screen knowing why.
                setStartError('');
                try {
                  const r = await startTasks(conn, pkg.tasks.map((x) => x.id), base);
                  // The three ways a start can do nothing, each said out loud.
                  // The server reports which one it was, and leaving that
                  // unread here would put the silence back one layer down.
                  if (r.blocked) setStartError(t('downloads.startBlocked'));
                  else if (r.started === 0 && r.skipped > 0) setStartError(t('downloads.startSkipped', { n: r.skipped }));
                  // The switch flipped on the server, so show it now rather
                  // than at the next five-second poll.
                  if (r.released) setQueue((q) => (q ? { ...q, halted: false } : q));
                  if (r.started > 0) {
                    // Pull once before switching, so the package is there when
                    // the tab arrives. Nothing optimistic is invented: this
                    // asks the server and shows what it answers. On a direct
                    // connection the socket has usually delivered it already
                    // and this costs one request; over the relay it is the
                    // difference between seeing the move now and waiting out
                    // the polling cycle plus a round trip.
                    await live.current?.refresh?.().catch(() => {
                      /* the next tick will get it; a failed refresh is not a
                         reason to leave somebody on the wrong tab */
                    });
                    setTab('downloads');
                  }
                } catch (e) {
                  setStartError(e instanceof Error ? e.message : String(e));
                }
              }
            : undefined
        }
        // Both tabs rather than only the collector: a package in the queue is
        // as likely to be the one somebody wants rid of. The list asks first,
        // so this runs only on a yes.
        //
        // The files stay: this removes the entries rather than what has already
        // been downloaded. Erasing those is a second, differently dangerous
        // decision, and a folder's bin badge is not the place to offer it.
        onDeletePackage={async (pkg) => {
          setStartError('');
          try {
            await deleteTasks(conn, pkg.tasks.map((x) => x.id), false, base);
          } catch (e) {
            setStartError(e instanceof Error ? e.message : String(e));
          }
        }}
        /* Both tabs reorder. A band is every task that is neither done nor
           failed (movable, app_queue.go), a staged one included, and it carries
           a position like any other, so the collector has an order to write.
           Passing `undefined` here arms the gesture, lifts the row and moves
           the neighbours aside, and then drops the result with nothing on
           screen to say so. */
        onReorder={async (ids) => {
          setStartError('');
          try {
            await reorderTasks(conn, ids, base);
          } catch (e) {
            setStartError(e instanceof Error ? e.message : String(e));
            // Passed on as well as shown: the list holds the dropped order
            // until this settles, and a refusal is what tells it to let go.
            throw e;
          }
          // The list holds that order until the live one agrees, and over a
          // relay or to a peer the live one would agree only at the next poll.
          await live.current?.refresh?.().catch(() => {
            /* the next tick brings it */
          });
        }}
      />

      <TouchableOpacity
        style={[styles.fab, { backgroundColor: accent, borderRadius: radii.pill }]}
        onPress={onAddPress}
      >
        <Text style={[styles.fabText, { color: accentContrast }]}>+</Text>
      </TouchableOpacity>
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
//
// One column stretched across a tablet is a card 900 points wide with its text
// at one edge and its badge at the other. A cap plus centring costs a phone
// nothing, since 640 is wider than every phone, and makes a tablet readable.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { ...capped,
    flexDirection: 'row',
    justifyContent: 'space-between',
    // Centre rather than flex-start: the back badge and the title share this
    // row, and a badge top-aligned against a 20px line reads as a mistake.
    alignItems: 'center',
    gap: 12,
    padding: 16,
    paddingTop: 56,
  },
  topBarLeft: { flex: 1, minWidth: 0 },
  topBarRight: { flexDirection: 'row', gap: 12, alignItems: 'center' },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  connState: { fontSize: TYPE.dense, marginTop: 2 },
  // No horizontal margin: these live inside the list's own content container,
  // which carries the padding, the cap and the centring, and a margin here
  // would inset them from the cards by another 16. Same padding and gap as the
  // overview's summary card, so the two readings of one instance are drawn in
  // one box on both screens.
  queueCard: { padding: 14, gap: 10, marginBottom: 12 },
  // The top line inside it: state on the left, the start/stop pair on the right.
  queueBar: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  queueActions: { flexDirection: 'row', gap: 8 },
  // Dense, off the scale in theme/tokens.ts, since 13 is a rung between dense
  // and body that the table does not have. The active count and the speed are
  // rewritten every second, so they take tabular numerals; without them the
  // line shifts sideways as digits change width.
  queueLabel: { fontSize: TYPE.dense, fontVariant: ['tabular-nums'] },
  queueError: { marginBottom: 8, fontSize: TYPE.caption },
  tabs: { marginBottom: 10 },
  empty: { textAlign: 'center', marginTop: 48 },
  fab: {
    position: 'absolute',
    // The trailing edge rather than right: under a right-to-left language the
    // whole layout mirrors while a physical edge stays put, leaving the button
    // on the side the thumb has stopped expecting.

    insetInlineEnd: 20,
    bottom: 32,
    width: 56,
    height: 56,
    alignItems: 'center',
    justifyContent: 'center',
    elevation: 4,
  },
  fabText: { fontSize: 28, lineHeight: 30, fontWeight: '400' },
});
