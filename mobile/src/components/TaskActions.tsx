import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  AccessibilityInfo,
  Animated,
  Easing,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  TouchableOpacity,
  View,
  useWindowDimensions,
} from 'react-native';
import { errorText } from '../api/client';
import {
  ACTION_LABELS,
  PRIORITY_KEYS,
  actionCall,
  offeredActions,
  retryFailedCall,
  sharedPriority,
  undoCall,
  type Call,
  type Offer,
  type PriorityChoice,
} from '../api/taskActions';
import { fetchPriorities, sendCall, type BulkResult } from '../api/taskCalls';
import type { ServerConnection, Task } from '../api/types';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { ConfirmDialog } from './ConfirmDialog';
import { GlimButton, NotchCard } from './glim';
import IconBadge, { Back, Check, Cross, Retry, Trash } from './IconBadge';
import { NoArrival } from './Moving';
import { Text } from './Text';

/** One line of the sheet. An entry with `submenu` turns the sheet to those
 *  entries instead of acting, as the seven priorities do. */
export interface ActionEntry {
  id: string;
  label: string;
  /** What the links are set to, at the end of the line. */
  detail?: string;
  checked?: boolean;
  onPress?: () => void;
  submenu?: ActionEntry[];
}

export interface ActionGroup {
  id: string;
  entries: ActionEntry[];
}

/** How long a line with nothing to undo stays up. */
const NOTICE_MS = 4000;
/** The instance's own undo window, for an answer that leaves it out. */
const UNDO_MS = 30000;

/**
 * The sheet a row's More badge opens: what can be done with a link or a whole
 * package, the web's right-click menu as a phone has room for it. A window
 * like ConfirmDialog, its title the name of what it acts on, with groups of
 * entries apart by a gap rather than a line. A press on the scrim or the back
 * gesture closes it, and from a submenu the back gesture goes back first.
 */
export function ActionSheet({
  title,
  groups,
  onClose,
}: {
  /** The link's or the package's name; null keeps the sheet closed. */
  title: string | null;
  groups: ActionGroup[];
  onClose: () => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const { motion } = useMotion();
  const { height } = useWindowDimensions();
  const [page, setPage] = useState<ActionEntry | null>(null);
  const close = () => {
    setPage(null);
    onClose();
  };
  const shown = page ? [{ id: page.id, entries: page.submenu ?? [] }] : groups;

  const row = (e: ActionEntry) => (
    <TouchableOpacity
      key={e.id}
      accessibilityRole="menuitem"
      accessibilityState={e.checked === undefined ? undefined : { checked: e.checked }}
      onPress={() => {
        if (e.submenu) {
          setPage(e);
          return;
        }
        close();
        e.onPress?.();
      }}
      style={[styles.row, corners.control, e.checked && { backgroundColor: c.surface2 }]}
    >
      <Text style={[styles.label, { color: c.text }]} numberOfLines={1}>
        {e.label}
      </Text>
      {e.detail ? <Text style={[styles.detail, { color: c.textMuted }]}>{e.detail}</Text> : null}
      {e.checked && <Check color={c.text} size={TYPE.body} />}
      {e.submenu && <Text style={[styles.chevron, { color: c.textSub }]}>›</Text>}
    </TouchableOpacity>
  );

  return (
    <Modal
      visible={title !== null}
      transparent
      animationType={motion === 'off' ? 'none' : 'fade'}
      onRequestClose={page ? () => setPage(null) : close}
    >
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={close}>
          {/* Swallows the press so a tap on the card itself closes nothing. */}
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title={page ? page.label : (title ?? '')} style={styles.flush}>
              <ScrollView style={{ maxHeight: height * 0.7 }}>
                {page && (
                  <TouchableOpacity
                    accessibilityRole="button"
                    onPress={() => setPage(null)}
                    style={[styles.row, corners.control]}
                  >
                    <Back color={c.textSub} size={TYPE.body} />
                    <Text style={[styles.label, { color: c.textSub }]}>{t('settings.back')}</Text>
                  </TouchableOpacity>
                )}
                {shown.map((g) => (
                  <View key={g.id} style={styles.group}>
                    {g.entries.map(row)}
                  </View>
                ))}
              </ScrollView>
            </NotchCard>
          </Pressable>
        </Pressable>
      </NoArrival>
    </Modal>
  );
}

/**
 * The line a removal leaves at the foot of the screen, with the undo while
 * the instance still holds the rows, the web's toast in the place a phone
 * keeps one. It says its line to TalkBack once it arrives.
 */
function Notice({ line, onUndo, onClose }: { line: string; onUndo?: () => void; onClose: () => void }) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const { n } = useMotion();
  const v = useRef(new Animated.Value(n.toast === 0 ? 1 : 0)).current;
  useEffect(() => {
    if (n.toast > 0) {
      Animated.timing(v, { toValue: 1, duration: n.toast, easing: Easing.out(Easing.ease), useNativeDriver: true }).start();
    }
    AccessibilityInfo.announceForAccessibility(line);
    // Once per mount: each notice is keyed by when it was raised.
  }, []);
  return (
    <Animated.View
      pointerEvents="box-none"
      style={[
        styles.slot,
        { opacity: v, transform: [{ translateY: v.interpolate({ inputRange: [0, 1], outputRange: [n.travel, 0] }) }] },
      ]}
    >
      <View style={[styles.notice, { backgroundColor: c.surface, ...corners.card }]}>
        <View style={[styles.mark, { backgroundColor: c.statusOkSolid, ...corners.pill }]} />
        <Text style={[styles.noticeLine, { color: c.text }]} numberOfLines={2}>
          {line}
        </Text>
        {onUndo && <GlimButton tone="quiet" label={t('actions.undo')} onPress={onUndo} />}
        <IconBadge icon={<Cross color={c.textSub} />} onPress={onClose} accessibilityLabel={t('actions.close')} />
      </View>
    </Animated.View>
  );
}

/**
 * useTaskActions is what the download screen hands its rows: `open` raises
 * the sheet for a link or a package, `remove` takes links off the list with
 * the undo, `retryFailed` is the button that resets every failed link, and
 * `layer` is the sheet, its confirmation and the undo line, drawn over the
 * screen.
 *
 * The sheet keeps the ids it was opened for and reads the links from `tasks`
 * on every render, so what it offers follows the list as it refreshes.
 */
export function useTaskActions({
  conn,
  base,
  tasks,
  refresh,
  onError,
  extraGroups,
}: {
  conn: ServerConnection;
  base: string;
  tasks: Task[];
  refresh: () => Promise<void>;
  /** The line under the queue card: why a call was refused, or '' to clear it. */
  onError: (line: string) => void;
  /** Entries another part of the screen offers for the same links, placed
   *  before removal. */
  extraGroups?: (chosen: Task[]) => ActionGroup[];
}): {
  open: (chosen: Task[], title: string, where: { settled: boolean; pkg: boolean }) => void;
  remove: (ids: string[]) => Promise<void>;
  retryFailed: ReactNode;
  layer: ReactNode;
} {
  const { t } = useT();
  const [menu, setMenu] = useState<{ ids: Set<string>; title: string; settled: boolean; pkg: boolean } | null>(null);
  const [asking, setAsking] = useState<{ ids: string[]; pkg: boolean } | null>(null);
  const [notice, setNotice] = useState<{ line: string; token?: string; ms: number; at: number } | null>(null);
  const [priorities, setPriorities] = useState<PriorityChoice[]>([]);
  // Which instance the priorities were read from. They are the same on every
  // instance of one version, so they are read once per instance shown.
  const askedOf = useRef('');

  const chosen = menu ? tasks.filter((x) => menu.ids.has(x.id)) : [];
  // A link can leave the list while its sheet is open, finished or removed
  // from a browser.
  useEffect(() => {
    if (menu && chosen.length === 0) setMenu(null);
  }, [menu, chosen.length]);

  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice((now) => (now === notice ? null : now)), notice.ms);
    return () => clearTimeout(timer);
  }, [notice]);

  const run = async (call: Call): Promise<BulkResult | undefined> => {
    onError('');
    try {
      const r = await sendCall(conn, base, call);
      await refresh().catch(() => {
        /* the next tick brings it */
      });
      return r;
    } catch (e) {
      onError(errorText(t, e));
      return undefined;
    }
  };

  const remove = async (ids: string[]) => {
    const r = await run(actionCall({ id: 'remove', ids }));
    if (!r) return;
    setNotice({
      line: t('actions.removed', { n: r.count }),
      token: r.undo,
      ms: r.undo ? (r.undoMs ?? UNDO_MS) : NOTICE_MS,
      at: Date.now(),
    });
  };

  // An expired token answers with nothing put back, which is too late rather
  // than a failure.
  const undo = async (token: string) => {
    setNotice(null);
    const r = await run(undoCall(token));
    if (!r) return;
    setNotice({
      line: r.count > 0 ? t('actions.undone', { n: r.count }) : t('actions.undoTooLate'),
      ms: NOTICE_MS,
      at: Date.now(),
    });
  };

  const open = (links: Task[], title: string, where: { settled: boolean; pkg: boolean }) => {
    setMenu({ ids: new Set(links.map((x) => x.id)), title, ...where });
    const of = `${conn.relayKey}:${conn.instanceId}${base}`;
    if (askedOf.current === of) return;
    askedOf.current = of;
    setPriorities([]);
    fetchPriorities(conn, base).then(setPriorities, () => {
      // Without them the sheet offers no priority, and the next open asks again.
      askedOf.current = '';
    });
  };

  const priorityWord = (p: PriorityChoice) => (Object.hasOwn(PRIORITY_KEYS, p.id) ? t(PRIORITY_KEYS[p.id]) : p.id);

  const entry = (offer: Offer): ActionEntry => {
    const label = t(ACTION_LABELS[offer.id]);
    if (offer.id === 'priority') {
      const shared = sharedPriority(chosen);
      const current = priorities.find((p) => p.value === shared);
      return {
        id: offer.id,
        label,
        detail: current && priorityWord(current),
        submenu: priorities.map((p) => ({
          id: p.id,
          label: priorityWord(p),
          checked: p.value === shared,
          onPress: () => void run(actionCall(offer, p.value)),
        })),
      };
    }
    if (offer.id === 'remove') {
      const pkg = !!menu?.pkg;
      return { id: offer.id, label, onPress: () => setAsking({ ids: offer.ids, pkg }) };
    }
    return { id: offer.id, label, onPress: () => void run(actionCall(offer)) };
  };

  const own: ActionGroup[] = menu
    ? offeredActions(chosen, { settled: menu.settled, priorities: priorities.length > 0, now: Date.now() }).map((g) => ({
        id: g[0].id,
        entries: g.map(entry),
      }))
    : [];
  // Removal is always the last of them and stays last.
  const groups = menu ? [...own.slice(0, -1), ...(extraGroups?.(chosen) ?? []), ...own.slice(-1)] : [];

  const token = notice?.token;
  const failed = tasks.filter((x) => x.status === 'error').length;
  const retryFailed =
    failed > 0 ? (
      <GlimButton
        tone="quiet"
        label={`${t('actions.retryFailed')} (${failed})`}
        icon={(ink) => <Retry color={ink} />}
        onPress={() => void run(retryFailedCall)}
        style={styles.retryFailed}
      />
    ) : null;

  const layer = (
    <>
      <ActionSheet title={menu && chosen.length > 0 ? menu.title : null} groups={groups} onClose={() => setMenu(null)} />
      <ConfirmDialog
        visible={asking !== null}
        title={t(asking?.pkg ? 'packages.deleteConfirmTitle' : 'actions.removeLinkTitle')}
        message={asking?.pkg ? t('packages.deleteConfirmMessage', { n: asking.ids.length }) : t('actions.removeLinkMessage')}
        cancelLabel={t('settings.cancel')}
        confirmLabel={t('packages.deleteConfirmButton')}
        confirmIcon={(ink) => <Trash color={ink} />}
        onCancel={() => setAsking(null)}
        onConfirm={() => {
          const a = asking;
          setAsking(null);
          if (a) void remove(a.ids);
        }}
      />
      {notice && (
        <Notice
          key={notice.at}
          line={notice.line}
          onUndo={token ? () => void undo(token) : undefined}
          onClose={() => setNotice(null)}
        />
      )}
    </>
  );

  return { open, remove, retryFailed, layer };
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  scrim: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24 },
  window: { width: '100%', maxWidth: 420 },
  flush: { marginTop: 0 },
  group: { gap: 2, marginTop: 8 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingVertical: 10, paddingHorizontal: 12 },
  label: { flex: 1, fontSize: TYPE.body },
  detail: { fontSize: TYPE.subline },
  chevron: { fontSize: 17, lineHeight: 20, width: 12, textAlign: 'center' },
  retryFailed: { alignSelf: 'flex-start', marginBottom: 12 },
  // Above the add button, which owns the trailing corner.
  slot: { position: 'absolute', bottom: 100, start: 0, end: 0, paddingHorizontal: 16, alignItems: 'center' },
  notice: {
    width: '100%',
    maxWidth: 640,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    padding: 10,
    paddingStart: 14,
    elevation: 6,
    shadowColor: '#000',
    shadowOpacity: 0.3,
    shadowRadius: 10,
    shadowOffset: { width: 0, height: 4 },
  },
  mark: { width: 4, alignSelf: 'stretch' },
  noticeLine: { flex: 1, minWidth: 0, fontSize: TYPE.body },
});
