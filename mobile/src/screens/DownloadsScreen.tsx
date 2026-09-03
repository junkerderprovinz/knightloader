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

// peer, when set, means this screen is showing a FEDERATION PEER of conn
// rather than conn's own queue: base becomes the proxy prefix
// (/api/instances/{name}, see internal/api/routes_federation.go).
//
// Whether the task list then streams or polls is liveTasks' decision, not
// this screen's - a federation peer and a relay connection both forward
// plain REST calls with no socket to attach to, and only api/client.ts knows
// which of those is in play.
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
  /** What the last start or stop actually did, when it did not work. Shown
   *  rather than swallowed, the same line the overview carries: a control that
   *  reports nothing is indistinguishable from a control that does nothing. */
  const [startError, setStartError] = useState('');
  // Which half of the instance is on screen (jdp, 2026-08-30: "Zwei tabs soll
  // es geben: Dowload und Sammler"). The two are one task list with one status
  // telling them apart - "collected" means staged and not started - so this is
  // a filter over what already streams, not a second request.
  const [tab, setTab] = useState<'downloads' | 'collector'>('downloads');
  // Summed from the task list this screen already streams: no second request,
  // and no second truth about the same number.
  const speed = tasks.reduce((n, t) => n + (t.speed || 0), 0);
  const collected = tasks.filter((x) => x.status === 'collected');
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

  // try/finally with no catch was the bug, not a style choice: the finally
  // cleared the spinner and the rejection went nowhere, so a queue call the
  // instance refused looked exactly like a button that was not wired up.
  //
  // Stopping calls /api/queue/stop, not /api/queue with halted:true (jdp,
  // 2026-08-31: "wenn man auf den stopp button drückt werden sie nicht
  // gestoppt"). Halting stops the DISPATCHER and lets whatever is already
  // downloading run to the end - which is the right default for the server and
  // the wrong verb for this button, because the bar somebody is watching keeps
  // moving. See stopAll() in api/client.ts. Starting is still the plain
  // release, because there is no second kind of start.
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
        {/* The way out is a badge on the LEFT of the name, exactly like the one
            in Settings (jdp, 2026-08-31: "wenn ich eine instanz öffne soll
            statt dem übersichtsbutton ein zurückbutton links neben dem namen
            sein (Wie in den einstellungen)").

            It was a text button called "Übersicht" over on the right, which is
            a destination rather than a direction - and this app already has one
            gesture for "back to where I came from", drawn one way, on the
            settings screen. Two shapes for one meaning is the thing worth
            fixing here, not the wording. */}
        <IconBadge icon={<Back color={c.textSub} />} onPress={peer && onBackToOwn ? onBackToOwn : onSwitchConnection} accessibilityLabel={t('settings.back')} />
        <View style={styles.topBarLeft}>
          <Text style={[styles.title, { color: c.text }]}>{peer ? (peer.displayName ?? peer.name) : conn.name}</Text>
          {/* Only while it is NOT connected (jdp, 2026-08-30: "der
              verbundentext soll weg"). "verbunden" is the ordinary case, so it
              said nothing on the screen it occupied - a label that is true
              almost always is a label nobody reads. Still connecting, or
              dropped, is worth saying, so that half stays. */}
          {!connected && (
            <Text style={[styles.connState, { color: c.statusWarnSolid }]}>
              {t('downloads.connecting')}
            </Text>
          )}
        </View>
        <View style={styles.topBarRight}>
          {/* Removing THIS connection belongs here, on the thing being removed
              (jdp, 2026-08-30: "der löschenbutton soll nur in der instanz
              drinnen zu sehen sein, nicht auf der card"). On the overview it
              sat on every row of a list you tap to open, which is a mis-tap
              waiting to happen.

              It is the only badge left on this side. The gear went (jdp,
              2026-08-31: "Der Eisntellungsbutton soll in der instanzansicht
              weg. den soll es nur in der übersicht geben"): settings are not a
              property of one instance, and offering them from inside one
              suggests they are. One door, on the screen that owns them. */}
          {!peer && onRemoveConnection && (
            <IconBadge
              icon={<Trash color={c.textSub} />}
              onPress={onRemoveConnection}
              accessibilityLabel={t('connections.remove')}
            />
          )}
        </View>
      </View>

      {/* Grouped into packages, not one row per link. A container is ONE thing
          somebody added; a flat list of its hundred files says nothing about
          what was added. Same reasoning as the web interface and JDownloader.

          Everything above the rows travels as this list's HEADER rather than as
          its siblings, and that is the fix for the strip sitting narrower than
          the cards and hard against the left edge (jdp, 2026-08-31: "Der
          download/Sammler selektor soll bündig mit den cards sien"). As
          siblings, each piece carried its own copy of the list's width cap,
          centring and margins - four places to keep in step, and they were not.
          Inside the content container there is nothing to keep in step. */}
      <PackageList
        tasks={tab === 'collector' && collected.length > 0 ? collected : queued}
        empty={connected ? t('downloads.empty') : t('downloads.emptyConnecting')}
        header={
          <>
            {/* One card, figures and curve together (jdp, 2026-09-01: "in der
                instanzansicht ist er wieder eine eigene card. es soll auch mit
                der card darüber veschmolzen werden").

                They were two objects: a queue bar card, and the graph as a
                sibling below it wearing a surface and a radius of its own,
                which is a card by every property that makes something look like
                one. Side by side on a tablet, they were two cards even more
                plainly. The overview's summary card has held both in one box
                since [358]'s first half; this is the same reading of the same
                instance, so it is the same box. */}
            <View style={[styles.queueCard, { backgroundColor: c.surface, borderRadius: radii.card }]}>
              <View style={styles.queueBar}>
                <Text style={[styles.queueLabel, { color: c.textMuted }]}>
                  {queue ? (queue.halted ? t('downloads.queueHalted') : t('downloads.queueRunning')) : '—'}
                  {queue && queue.running > 0 ? ` · ${t('downloads.queueActive', { n: queue.running })}` : ''}
                  {speed > 0 ? ` · ${fmtBytes(speed)}/s` : ''}
                </Text>
                {/* One badge whose offer follows the state, so it can never
                    offer the thing that is already true - the same control the
                    overview's own summary card uses. Stopping is the HARD stop
                    now: see toggleHalted. */}
                {queueBusy ? (
                  <ActivityIndicator color={accentInk} size="small" />
                ) : (
                  <IconBadge
                    symbol={queue?.halted ? '▶' : '■'}
                    accent={queue?.halted === true}
                    onPress={() => queue && toggleHalted(!queue.halted)}
                    accessibilityLabel={t(queue?.halted ? 'downloads.start' : 'downloads.stop')}
                  />
                )}
              </View>

              {/* Shown whenever something is IN the queue, not only while bytes
                  are moving (jdp, 2026-08-31: "Wo ist der downloadgraph in der
                  übericht und in der instanz ansicht?").

                  The old test was `speed > 0`, on the reasoning that an idle
                  graph is a row of nothing costing the height of a graph. That
                  is right for an empty instance and wrong for exactly the case
                  he was looking at: a queue that says "running" while nothing
                  moves. There, a line flat at zero is not nothing - it is the
                  answer. So: something queued, graph; nothing queued, no
                  graph. */}
              {(speed > 0 || queued.length > 0) && <SpeedGraph speed={speed} />}
            </View>

            {/* Why the last start or stop did not take. One line, in the fail
                colour, and only when there is something to say. Outside the
                card on purpose: it is about the last press, not about the
                queue - the same placement the overview gives its own. */}
            {startError !== '' && (
              <Text style={[styles.queueError, { color: c.statusFailSolid }]} numberOfLines={2}>
                {startError}
              </Text>
            )}

            {/* The strip only appears once there is something staged: a tab
                that is always empty is a tab that teaches you to ignore the
                strip. */}
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
                // Straight into the queue and out of this tab (jdp: "der klick
                // auf den playbutton verschiebt den order in den downloadtab
                // und startet den download"). Switching tabs first would leave
                // somebody looking at a collector that is one package emptier
                // for no visible reason.
                //
                // And it says so when it fails. This call used to end in
                // `.catch(() => {})`, which is the exact shape that made "die
                // ganzen play/Stop buttons haben derzeit keine wirkung"
                // unanswerable elsewhere in this app: the tab switched, the
                // package stayed where it was, and nothing on screen knew why.
                // A refused start is now a line, and the tab only changes when
                // there is actually something to see in it.
                setStartError('');
                try {
                  const r = await startTasks(conn, pkg.tasks.map((x) => x.id), base);
                  // The three ways a start can do nothing, each said out loud
                  // (jdp, four rounds of "es lädt nicht herunter"). The server
                  // now reports which one it was; leaving that unread here
                  // would put the silence back one layer down.
                  if (r.blocked) setStartError(t('downloads.startBlocked'));
                  else if (r.started === 0 && r.skipped > 0) setStartError(t('downloads.startSkipped', { n: r.skipped }));
                  // The switch flipped on the server, so show it now rather
                  // than at the next five-second poll.
                  if (r.released) setQueue((q) => (q ? { ...q, halted: false } : q));
                  if (r.started > 0) {
                    // Pull once before switching, so the package is THERE when
                    // the tab arrives (jdp, 2026-09-03: "wenn ich bei einem
                    // ordner im sammler auf play drücke dauert es lange bis er
                    // im downloadtab erscheint").
                    //
                    // Nothing optimistic is invented here: this asks the server
                    // and shows what it answers. On a direct connection the
                    // socket has usually delivered it already and this costs one
                    // extra request; over the relay it is the difference between
                    // seeing the move now and waiting out the three-second
                    // polling cycle plus a round trip.
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
        // Both tabs, not only the collector (jdp, 2026-08-31: "man soll ordner
        // auch löschen können"): a package in the queue is as likely to be the
        // one somebody wants rid of. The list itself asks first, so this only
        // ever runs on a yes.
        //
        // The files stay: this removes the entries, not what has already been
        // downloaded. Erasing those is a second, differently dangerous
        // decision, and a folder's bin badge is not the place to offer it.
        onDeletePackage={async (pkg) => {
          setStartError('');
          try {
            await deleteTasks(conn, pkg.tasks.map((x) => x.id), false, base);
          } catch (e) {
            setStartError(e instanceof Error ? e.message : String(e));
          }
        }}
        /* BOTH tabs reorder, and the belief that the collector could not is
           what made drag and drop look broken for five rounds.

           The reasoning here used to be "nothing in the collector is in the
           wait queue yet, so there is no order to write". The server disagrees
           and always did: a band is every task that is neither done nor failed
           (movable, app_queue.go), a staged one included, and it carries a
           position like any other. So the gesture armed, the row lifted, the
           neighbours moved aside - and the drop was dropped on the floor by the
           `undefined` that used to be here, with nothing on screen to say so.

           jdp's video of 2026-09-01 is entirely of this: three packages, the
           play badge on each of them, which only the collector draws. Every
           drag in it was discarded before it reached the network. */
        onReorder={async (ids) => {
          setStartError('');
          try {
            await reorderTasks(conn, ids, base);
          } catch (e) {
            setStartError(e instanceof Error ? e.message : String(e));
          }
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

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
// One column stretched across a tablet is a card 900 points wide with its
// text at one edge and its badge at the other. A cap plus centring costs a
// phone nothing (640 is wider than every phone) and makes a tablet readable.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { ...capped,
    flexDirection: 'row',
    justifyContent: 'space-between',
    // Centre, not flex-start: the back badge and the title now share this row,
    // and a 36px badge top-aligned against a 20px line reads as a mistake.
    alignItems: 'center',
    gap: 12,
    padding: 16,
    paddingTop: 56,
  },
  topBarLeft: { flex: 1, minWidth: 0 },
  topBarRight: { flexDirection: 'row', gap: 12, alignItems: 'center' },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  connState: { fontSize: TYPE.dense, marginTop: 2 },
  // No horizontal margin any more: these live inside the list's own content
  // container, which already carries the padding, the cap and the centring.
  // Keeping a margin here would inset them from the cards by another 16.
  // The card. Same padding and gap as the overview's summary card, so the two
  // readings of one instance are drawn in one box on both screens.
  queueCard: { padding: 14, gap: 10, marginBottom: 12 },
  // The top line inside it: state on the left, the one badge on the right.
  queueBar: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  queueLabel: { fontSize: 13 },
  queueError: { marginBottom: 8, fontSize: TYPE.caption },
  tabs: { marginBottom: 10 },
  empty: { textAlign: 'center', marginTop: 48 },
  fab: {
    position: 'absolute',
    right: 20,
    bottom: 32,
    width: 56,
    height: 56,
    alignItems: 'center',
    justifyContent: 'center',
    elevation: 4,
  },
  fabText: { fontSize: 28, lineHeight: 30, fontWeight: '400' },
});
