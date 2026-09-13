import { useCallback, useEffect, useState } from 'react';
import { FlatList, Image, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { checkConnection, setQueueHalted } from '../api/client';
import { listConnections, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { contentMax, useWide } from '../theme/layout';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import IconBadge, { Connect, Gear, boxForInk } from '../components/IconBadge';
import SpeedGraph from '../components/SpeedGraph';
import { GlimButton, StatusBadge } from '../components/glim';
import { aggregate, fetchInstanceStats, fmtBytes, type InstanceStats } from '../api/stats';

/**
 * The one line under an instance's name: the same four figures, in the same
 * order, as the browser extension's own instance card - state, file count,
 * bytes left, speed. The file count is always shown, zero included, so the row
 * cannot change height the moment a download appears.
 *
 * `undefined` means the numbers have not arrived yet, `null` means they were
 * asked for and did not come. Those are different facts and the line says so
 * rather than drawing zeroes for an instance that is not there.
 */
function statusLine(
  // The catalogue's own key union, not a loose `string`: a typo in a key here
  // would otherwise compile and show an empty line in 42 languages.
  t: ReturnType<typeof useT>['t'],
  s: InstanceStats | null | undefined,
  reach: ConnStatus,
): string {
  if (s === undefined) return reach === 'offline' ? t('instance.offline') : '…';
  if (s === null) return t('instance.offline');
  const parts: string[] = [];
  if (s.halted) parts.push(t('downloads.queueHalted'));
  else if (s.running > 0) parts.push(t('downloads.queueRunning'));
  parts.push(`${s.files} ${t('instance.files')}`);
  if (s.remaining > 0) parts.push(`${fmtBytes(s.remaining)} ${t('instance.left')}`);
  if (s.speed > 0) parts.push(`${fmtBytes(s.speed)}/s`);
  return parts.join(' · ');
}

// The app's own mark, beside the name it belongs to (jdp, 2026-08-30: "In der
// Übersicht soll links von der Überschrift auch das Logo sein").
//
// The adaptive icon's FOREGROUND layer, not icon.png (jdp, same day: "Das logo
// in der übersicht soll das ohne hintergrundkachel sein"): icon.png is the
// finished launcher tile with mark and ground baked together, so on a card it
// read as a little app icon pasted onto the page rather than as this product's
// own mark. The foreground layer is the mark alone, on nothing.
//
// require() and not a URI: a shipped asset, resolved by the bundler, so it is
// on screen at first paint with nothing to fetch.
const MARK = require('../../assets/android-icon-foreground.png');

/**
 * blend lays `over` on `base` at `alpha`, returning an opaque colour.
 *
 * A copy of the one in components/TaskRow.tsx, which is where it was written
 * and where the reasoning for computing the mix rather than layering a
 * translucent view sits. It is duplicated here and should not stay that way:
 * the pair belongs beside the palette in theme/tokens.ts, with both call sites
 * reading it from there.
 */
function blend(base: string, over: string, alpha: number): string {
  const b = rgb(base);
  const o = rgb(over);
  if (!b || !o) return base;
  const mix = (x: number, y: number) => Math.round(x + (y - x) * alpha);
  return `rgb(${mix(b.r, o.r)}, ${mix(b.g, o.g)}, ${mix(b.b, o.b)})`;
}

function rgb(hex: string): { r: number; g: number; b: number } | null {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return null;
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

type ConnStatus = 'checking' | 'online' | 'offline';

// The home screen, opened straight from a fresh install rather than the
// connect form: it lands you IN the app first, empty state included, with
// the connect screen and Settings only ever a badge tap away, not something
// forced on you before you have looked at anything (jdp, 2026-08-24: "es
// soll nicht sofort gleich der Verbindungsbildschirm kommen"). A tap on a
// row makes that connection active and opens its Downloads screen; this
// screen itself never shows tasks, so it stays fast to scan even with
// several boxes on flaky Wi-Fi.
export default function ConnectionsScreen({
  onActivate,
  onAddPress,
  onOpenSettings,
}: {
  onActivate: (conn: ServerConnection) => void;
  onAddPress: () => void;
  onOpenSettings: () => void;
}) {
  const { t } = useT();
  const { c, accent, radii, hueAt, rainbow } = useAppearance();
  const wide = useWide();
  const [connections, setConnections] = useState<ServerConnection[]>([]);
  const [status, setStatus] = useState<Record<string, ConnStatus>>({});
  const [loaded, setLoaded] = useState(false);
  const [stats, setStats] = useState<Record<string, InstanceStats | null>>({});
  const [why, setWhy] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  /** What the last start/stop actually did, when it did not work. Shown rather
   *  than swallowed: a control that reports nothing is indistinguishable from
   *  a control that does nothing. */
  const [queueError, setQueueError] = useState('');

  const reload = useCallback(async () => {
    const list = await listConnections();
    setConnections(list);
    setLoaded(true);
    list.forEach((conn) => {
      setStatus((s) => ({ ...s, [conn.id]: 'checking' }));
      checkConnection(conn)
        .then((auth) => setStatus((s) => ({ ...s, [conn.id]: auth.authenticated ? 'online' : 'offline' })))
        .catch(() => setStatus((s) => ({ ...s, [conn.id]: 'offline' })));
    });
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  // The numbers behind every card and behind the summary above them. Polled
  // rather than streamed: a socket per instance, for a list you only glance at,
  // would be a lot of machinery for a five-second refresh. Two calls each - the
  // queue for "is it halted", /api/queue/counters for the figures.
  const load = useCallback(async () => {
    const list = await listConnections();
    const results = await Promise.all(list.map((conn) => fetchInstanceStats(conn)));
    setStats(Object.fromEntries(list.map((conn, i) => [conn.id, results[i].ok ? results[i].stats : null])));
    // The reason, kept rather than dropped. See stats.ts: swallowing these is
    // what made "the buttons have no effect" impossible to explain.
    setWhy(
      Object.fromEntries(
        list.map((conn, i) => [conn.id, results[i].ok ? '' : (results[i] as { reason: string }).reason]),
      ),
    );
  }, []);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 5000);
    return () => clearInterval(id);
  }, [load]);

  const alle = connections.map((conn) => stats[conn.id] ?? null);
  const gesamt = aggregate(alle);

  // One button for the whole group, and it follows the group's own state
  // rather than carrying two controls that are wrong half the time (jdp:
  // "evtl mit play und stop button auf der card sie sich automatisch an den
  // zustand anpassen"). Halted everywhere means the offer is "start"; anything
  // else means "stop".
  const toggleAll = async () => {
    setBusy(true);
    setQueueError('');
    try {
      const list = await listConnections();
      // Settled, not all: one unreachable instance must not cancel the others,
      // and every failure is COLLECTED rather than swallowed. The first cut
      // wrote `.catch(() => {})` here, which is how a button that was failing
      // every time looked exactly like a button that did nothing.
      const results = await Promise.allSettled(list.map((conn) => setQueueHalted(conn, !gesamt.halted)));
      const failed = results.filter((r) => r.status === 'rejected') as PromiseRejectedResult[];
      if (failed.length > 0) {
        const reason = failed[0].reason;
        setQueueError(`${failed.length}/${list.length}: ${reason instanceof Error ? reason.message : String(reason)}`);
      }
      await load();
    } catch (e) {
      setQueueError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const activate = async (conn: ServerConnection) => {
    await setActiveConnectionId(conn.id);
    onActivate(conn);
  };


  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <View style={styles.brand}>
          <Image source={MARK} style={styles.mark} resizeMode="contain" />
          <Text style={[styles.title, { color: c.text }]}>KnightLoader</Text>
        </View>
        <View style={styles.badgeRow}>
          <IconBadge symbol="+" accent onPress={onAddPress} accessibilityLabel={t('connections.addButton')} />
          <IconBadge
            icon={<Gear color={c.textSub} hole={c.surface2} />}
            onPress={onOpenSettings}
            accessibilityLabel={t('settings.title')}
          />
        </View>
      </View>

      <FlatList
        data={connections}
        /* The rows read `status`, `stats` and `why` - three pieces of state the
           list knows nothing about - while `connections` is set once and never
           again. Without this, VirtualizedList never redraws a cell, so every
           status badge and every figure freezes at whatever it said on the first
           paint while the five-second poll updates state nobody redraws.
           Found while fixing the same defect in DragList; the rule is the same
           one both times: a cell that reads state outside `data` must say so. */
        extraData={[status, stats, why]}
        // The summary, the failure line and the graph travel as the list's own
        // HEADER rather than as siblings above it (jdp, 2026-08-31: "in der
        // Übesicht soll die alle instanzen card genauso groß sein wie die
        // instanzen card"). As a sibling the card carried its own copy of the
        // list's width cap plus a horizontal margin, and `width: '100%'` with a
        // margin does not shrink the way padding inside a container does - so it
        // came out wider than the cards it summarises. Inside the content
        // container there is no second copy to disagree.
        //
        // On a tablet the header spans BOTH columns, which is right: it is the
        // group's own line, not a third card competing for a slot.
        ListHeaderComponent={
          connections.length > 0 ? (
            <View style={styles.header}>
              {/* A COLUMN now, not a row: the figures and the badge share the
                  top line, and the graph sits under them inside the same card
                  (jdp, 2026-09-01: "der graph soll in die 'Alle instanzen' card
                  integriert sein"). It used to be a sibling below the card,
                  which made the group's own numbers and the group's own curve
                  two objects saying one thing. */}
              <View style={[styles.summary, { backgroundColor: c.surface, borderRadius: radii.card }]}>
                <View style={styles.summaryTop}>
                  <View style={styles.summaryText}>
                    <Text style={[styles.summaryTitle, { color: c.text }]}>{t('overview.title')}</Text>
                    <Text style={[styles.summaryLine, { color: c.textMuted }]} numberOfLines={1}>
                      {[
                        t('overview.online', { n: gesamt.online, total: gesamt.total }),
                        `${gesamt.files} ${t('instance.files')}`,
                        gesamt.remaining > 0 ? `${fmtBytes(gesamt.remaining)} ${t('instance.left')}` : null,
                        gesamt.speed > 0 ? `${fmtBytes(gesamt.speed)}/s` : null,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </Text>
                  </View>
                  {/* Only offered when at least one instance answered: a start
                      button over a group that is entirely unreachable promises
                      something it cannot do.

                      BOTH options, with only the one in force filled - the
                      same pair the instance screen draws and the same one the
                      browser extension has drawn since it shipped. It was one
                      badge whose glyph flipped with the state, which asks the
                      person to infer the alternative from a glyph that is not
                      on the screen; halting the queues of every instance at
                      once is about as far from "a preference" as this app
                      goes. Filled says what the group IS doing, not what the
                      press would do - a two-option pair is a selector with
                      icon-only segments - so pressing the filled one does
                      nothing. */}
                  {gesamt.online > 0 && (
                    <View style={styles.summaryActions}>
                      <IconBadge
                        symbol="▶"
                        accent={!gesamt.halted}
                        onPress={() => {
                          if (!busy && gesamt.halted) void toggleAll();
                        }}
                        accessibilityLabel={t('downloads.start')}
                      />
                      <IconBadge
                        symbol="■"
                        accent={gesamt.halted}
                        onPress={() => {
                          if (!busy && !gesamt.halted) void toggleAll();
                        }}
                        accessibilityLabel={t('downloads.stop')}
                      />
                    </View>
                  )}
                </View>

                {/* Shown whenever the group has anything queued, not only while
                    bytes move: a line flat at zero beside a queue that says
                    "running" is the answer, not an empty row. */}
                {(gesamt.speed > 0 || gesamt.files > 0) && (
                  <View style={styles.summaryGraph}>
                    <SpeedGraph speed={gesamt.speed} />
                  </View>
                )}
              </View>

              {/* Why the last start/stop did not take. One line, in the fail
                  colour, and only when there is something to say. Outside the
                  card: it is about the last click, not about the group. */}
              {queueError !== '' && (
                <Text style={[styles.queueError, { color: c.statusFailSolid }]} numberOfLines={2}>
                  {queueError}
                </Text>
              )}
            </View>
          ) : null
        }
        // Two columns on a tablet, one on a phone. key is tied to the count
        // because FlatList cannot change numColumns on an existing list - it
        // throws rather than re-laying out - so the list is remounted on a
        // rotation instead, which is cheap for a handful of cards and is the
        // documented way to do this.
        key={wide ? 'zwei' : 'eins'}
        numColumns={wide ? 2 : 1}
        columnWrapperStyle={wide ? styles.columns : undefined}
        keyExtractor={(conn) => conn.id}
        contentContainerStyle={[styles.list, { maxWidth: contentMax(wide) }]}
        renderItem={({ item, index }) => {
          const s = status[item.id] ?? 'checking';
          /* This list is a set of equal members, so each card owns a position
             in the palette - the thing that turns "rainbow mode" from one
             wired list into a property of the app. It is also the longest list
             here and the first screen anybody sees, and it was the one set in
             the app that had never been wired: the settings screen next door
             hands positions to five cards and four buttons, so the difference
             showed up inside one product.

             The position sits on the CARD, not on something inside it, which
             is what makes it reach the whole row.

             The wash is TaskRow's, number for number: 16% at rest, 22% for a
             card whose instance is actually downloading. Without a wash the
             colour would reach the row through nothing at all here - there is
             no progress bar on this card - and 16 rather than the 7 this
             family started with is the figure GlimStone names as the one
             people can actually see. Under the reactive reading the rest
             colour goes away and only what is running is lit; there is no
             hover on a phone, so "running" is this list's active state. */
          const st = stats[item.id];
          const laeuft = st != null && !st.halted && st.running > 0;
          const hue = hueAt(index);
          return (
            <TouchableOpacity
              style={[
                styles.row,
                { backgroundColor: c.surface, borderRadius: radii.card },
                hue && (!rainbow.reactive || laeuft)
                  ? { backgroundColor: blend(c.surface, hue, laeuft ? 0.22 : 0.16) }
                  : null,
              ]}
              onPress={() => activate(item)}
            >
              {/* The same card the extension draws: logo, name, what it is
                  doing (jdp, 2026-09-01: "Die Instanzencard sollen gleich
                  aussehen wie in der erweiterung: mit Logo"). Two surfaces
                  showing one group had two different objects for it, and the
                  one thing that says "this is a KnightLoader" at a glance was
                  the one the app left out. */}
              {/* The mark, bare (jdp, 2026-09-01: "das logo auf den instanzen
                  cards ohne hintergrund"). The tile behind it was borrowed from
                  the extension's own card, where it sits on a page ground and
                  needs a surface to sit on; here it is already inside a card, so
                  the tile was a second surface on top of a first one, and what
                  it drew was a grey square around a logo rather than a logo. */}
              <View style={styles.rowMark}>
                <Image source={MARK} style={styles.rowMarkImg} resizeMode="contain" />
              </View>
              <View style={styles.rowText}>
                <View style={styles.rowTop}>
                  <Text style={[styles.rowName, { color: c.text }]} numberOfLines={1}>
                    {item.name}
                  </Text>
                  {/* A badge with the word on it, not a coloured dot (jdp, same
                      message: "der statuspunkt soll ein kleiner badge mit
                      online/offlin text sein und auch dem status entsprechend
                      eingefärbt sein").
                      A 10-point dot asks the reader to know the colour code,
                      and it says nothing at all to somebody who cannot tell the
                      green from the red. The word carries the meaning and the
                      colour carries the urgency, which is the pairing every
                      other status in this family already uses. */}
                  <StatusBadge status={s} />
                </View>
                {/* What the instance is DOING, not where the connection goes
                    (jdp, 2026-08-30: "über relay text soll nicht dort stehen.
                    dort sollen die gleichen infos in der card stehen wie in
                    den cards in der browsererweiterung"). The relay address
                    answered a question nobody was asking - it is the same for
                    every card in the list, so it distinguished nothing while
                    taking the one line that could have. Same four figures and
                    the same order as the extension's own card. */}
                <Text style={[styles.rowUrl, { color: c.textMuted }]} numberOfLines={1}>
                  {stats[item.id] === null && why[item.id] ? why[item.id] : statusLine(t, stats[item.id], s)}
                </Text>
              </View>
              {/* No delete here any more (jdp, 2026-08-30: "der löschenbutton
                  soll nur in der instanz drinnen zu sehen sein, nicht auf der
                  card"). A bin sitting on every row of a list you tap to open
                  is a mis-tap waiting to happen, and it competed with the only
                  action the row actually has. It lives inside the instance
                  now, where the thing being removed is what you are looking
                  at - see DownloadsScreen. */}
            </TouchableOpacity>
          );
        }}
        /* The shared empty state: a card, a muted glyph at reduced opacity, a
           muted line, the one action. It had the line and the action and
           neither of the other two, so the screen somebody sees on a fresh
           install was text floating on the page ground while every other
           surface in the app is built out of cards.

           The glyph's size is the role's own number (26 points of ink, what an
           empty state takes in the web UI), converted through boxForInk
           because a drawn glyph fills less than the box it is handed. */
        ListEmptyComponent={
          loaded ? (
            <View style={[styles.empty, { backgroundColor: c.surface, borderRadius: radii.card }]}>
              <View style={styles.emptyIcon}>
                <Connect color={c.textMuted} size={boxForInk(26)} />
              </View>
              <Text style={[styles.emptyText, { color: c.textMuted }]}>{t('connections.empty')}</Text>
              <GlimButton hue={0} label={t('connections.emptyButton')} onPress={onAddPress} />
            </View>
          ) : null
        }
      />
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
    alignItems: 'center',
    padding: 16,
    paddingTop: 56,
  },
  // The list's header block. No width, no cap, no horizontal margin: it lives
  // inside the list's own content container, which already carries all three.
  // Every one of those it used to repeat was a chance to disagree, and it did.
  header: { marginBottom: 8 },
  summary: {
    // Same box as a row below it (jdp: "Übersichtscard soll genau so groß sein
    // wie die instanzencards"): same padding, same radius, same width - the
    // last one by construction rather than by matching numbers. A column, so
    // the graph can live under the figures inside the same card.
    padding: 14,
    gap: 10,
  },
  summaryTop: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  summaryActions: { flexDirection: 'row', gap: 8 },
  summaryText: { flex: 1, minWidth: 0, gap: 2 },
  // Body, off the scale in theme/tokens.ts: 15 is a rung between Dense and Body
  // that the table does not have.
  summaryTitle: { fontSize: TYPE.body, fontWeight: '600' },
  // Counts, bytes and a speed, all rewritten every five seconds, so the figures
  // take tabular numerals - otherwise the line shuffles sideways as digits
  // change width while somebody is reading it.
  summaryLine: { fontSize: TYPE.dense, fontVariant: ['tabular-nums'] },
  queueError: { marginTop: 8, fontSize: TYPE.caption },
  summaryGraph: {},
  brand: { flexDirection: 'row', alignItems: 'center', gap: 0, flexShrink: 1, minWidth: 0 },
  // 44, and both margins are negative, which looks like a hack and is not (jdp,
  // 2026-08-31: "der abstand von name und logo ist zu groß in der übersicht").
  //
  // The asset is the adaptive icon's FOREGROUND layer, and an adaptive icon
  // carries a safe zone: roughly a quarter of the box on each side is
  // transparent by specification, because every launcher crops into it. So the
  // mark is drawn at 44 to come out the right visual size, and then about
  // eleven points of that 44 are nothing at all on each side. A `gap` measures
  // the BOX, not the ink, so a gap of 10 read as 21 and the name looked adrift.
  //
  // The negative margins take back what the safe zone padded out. The gap is 0
  // for the same reason: there are already eleven invisible points between the
  // mark and the name.
  //
  // Logical edges, not left/right: under a right-to-left language the mark and
  // the name swap sides with the rest of the layout, and a physical margin
  // would take back the safe zone on whichever side it was written for rather
  // than on the side the mark has moved to.
  mark: { width: 44, height: 44, marginStart: -10, marginEnd: -6 },
  // The WORDMARK, and that is why it is not on the type scale: the scale's own
  // table sets a heading at 20 and names a brand-name instance as one of the
  // two sizes deliberately outside it. This is the product's name beside the
  // product's mark, not this screen's title - the screen titles next door
  // (Downloads, Settings) are the 20 the scale asks for.
  title: { fontSize: 22, fontWeight: '700' },
  badgeRow: { flexDirection: 'row', gap: 10 },
  list: { ...capped, paddingHorizontal: 16, paddingBottom: 32, gap: 8 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    padding: 14,
    gap: 12,
    // flex only bites inside a column wrapper, where two siblings share a
    // line; in a one-column list it is a no-op.
    flex: 1,
  },
  columns: { gap: 8 },
  // The logo's box. No ground of its own: see the comment at the call site.
  rowMark: { width: 38, height: 38, alignItems: 'center', justifyContent: 'center' },
  rowMarkImg: { width: 38, height: 38 },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  rowTop: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  rowName: { fontSize: TYPE.body, fontWeight: '600', flexShrink: 1 },
  // File counts, bytes left and a speed, refreshed every five seconds down a
  // stacked list: both halves of the tabular-numerals rule at once.
  rowUrl: { fontSize: TYPE.dense, marginTop: 2, fontVariant: ['tabular-nums'] },
  // The empty state's own card. The padding is the card's; the ground and the
  // radius are applied at the call site from the resolved tokens.
  empty: { alignItems: 'center', marginTop: 48, gap: 14, paddingVertical: 28, paddingHorizontal: 24 },
  emptyIcon: { opacity: 0.5 },
  emptyText: { fontSize: TYPE.body, textAlign: 'center' },
});
