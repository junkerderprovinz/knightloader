import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Animated, AppState, Easing, StyleSheet, View } from 'react-native';
import { pollCaptchas, refreshCaptchas, type Polling } from '../api/client';
import { byDeadline, noticeFor, type CaptchaNotice } from '../api/captcha';
import type { CaptchaChallenge, ServerConnection } from '../api/types';
import { useT, type TranslationKey } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { CardButton } from './glim';
import IconBadge, { Cross } from './IconBadge';
import { Text } from './Text';

export interface CaptchaWatchState {
  /** Nearest deadline first. */
  list: CaptchaChallenge[];
  /** Whether a list has arrived from the current connection. */
  loaded: boolean;
  /** Why the last look failed; the next one that works clears it. */
  error: unknown;
  /** Pulls the list now, for a screen that has just answered or skipped one. */
  reload: () => Promise<void>;
  /** Has the instance ask JD right away, then pulls the list. */
  refresh: () => Promise<void>;
  /** Runs an answer or a skip of challenge `id`. Its card then leaves without
   *  the banner saying it went elsewhere, unless the call fails. */
  settleHere: <T>(id: string, call: () => Promise<T>) => Promise<T>;
}

const Ctx = createContext<CaptchaWatchState>({
  list: [],
  loaded: false,
  error: null,
  reload: async () => {},
  refresh: async () => {},
  settleHere: (_id, call) => call(),
});

export const useCaptchas = () => useContext(Ctx);

/** How long the banner stays when nobody touches it. */
const BANNER_MS = 6000;

/**
 * Watches the active connection for captchas while the app is in front, and
 * puts up a banner when a new one starts waiting or one leaves without an
 * answer from this phone, as the web UI's toasts do.
 *
 * Polled rather than streamed, since the relay carries no socket, and every
 * five seconds like the queue. The app runs nothing in the background and
 * holds no notification permission, so a captcha that arrives while it is away
 * is announced when it comes back to the front. The watch keeps its last look
 * across that, which is what makes such a captcha news, but only while Android
 * keeps the app in memory; a cold start has nothing to compare with. The other
 * saved instances are not watched, and show their count on the overview.
 */
export function CaptchaWatch({
  conn,
  bannerHidden,
  onOpen,
  children,
}: {
  conn: ServerConnection | null;
  /** True while the captcha list itself is on screen. */
  bannerHidden: boolean;
  onOpen: () => void;
  children: ReactNode;
}) {
  const [list, setList] = useState<CaptchaChallenge[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<unknown>(null);
  // Not "active" alone: the state can read "unknown" before the first change
  // event, and a watch waiting for that would never start.
  const [front, setFront] = useState(AppState.currentState !== 'background');
  const [notice, setNotice] = useState<CaptchaNotice | null>(null);
  const live = useRef<Polling | null>(null);
  const seen = useRef<CaptchaChallenge[] | null>(null);
  // Answered or skipped from this phone, whose card already said how it went.
  const mine = useRef(new Set<string>());
  // Read by the poll's callback, which outlives the render that made it.
  const hidden = useRef(bannerHidden);
  hidden.current = bannerHidden;

  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => setFront(s !== 'background'));
    return () => sub.remove();
  }, []);

  // Another instance's first list is what is waiting there, not news.
  useEffect(() => {
    seen.current = null;
    mine.current.clear();
    setList([]);
    setLoaded(false);
    setError(null);
    setNotice(null);
  }, [conn]);

  useEffect(() => {
    if (!conn || !front) return;
    const handle = pollCaptchas(
      conn,
      (next) => {
        const sorted = byDeadline(next);
        const said = noticeFor(seen.current, sorted, mine.current, Date.now(), hidden.current);
        seen.current = sorted;
        for (const id of mine.current) {
          if (!sorted.some((ch) => ch.id === id)) mine.current.delete(id);
        }
        setList(sorted);
        setLoaded(true);
        setError(null);
        if (said) setNotice(said);
      },
      setError,
    );
    live.current = handle;
    return () => {
      live.current = null;
      handle();
    };
  }, [conn, front]);

  useEffect(() => {
    if (bannerHidden) setNotice((n) => (n?.kind === 'arrived' ? null : n));
  }, [bannerHidden]);

  // A banner about a new captcha that has gone again is a banner about nothing.
  const shown =
    notice && (notice.kind !== 'arrived' || list.some((ch) => ch.id === notice.challenge.id)) ? notice : null;

  const reload = useCallback(async () => {
    await live.current?.refresh();
  }, []);

  const refresh = useCallback(async () => {
    if (!conn) return;
    await refreshCaptchas(conn);
    await live.current?.refresh();
  }, [conn]);

  const settleHere = useCallback(async <T,>(id: string, call: () => Promise<T>): Promise<T> => {
    mine.current.add(id);
    try {
      return await call();
    } catch (e) {
      mine.current.delete(id);
      throw e;
    }
  }, []);

  const value = useMemo(
    () => ({ list, loaded, error, reload, refresh, settleHere }),
    [list, loaded, error, reload, refresh, settleHere],
  );

  return (
    <Ctx.Provider value={value}>
      {children}
      {shown && conn && (
        <Banner
          key={`${shown.kind} ${shown.challenge.id}`}
          notice={shown}
          instance={conn.name}
          onOpen={
            shown.kind === 'arrived'
              ? () => {
                  setNotice(null);
                  onOpen();
                }
              : undefined
          }
          onClose={() => setNotice(null)}
        />
      )}
    </Ctx.Provider>
  );
}

const NOTICE_TEXT: Record<CaptchaNotice['kind'], TranslationKey> = {
  arrived: 'captcha.waiting',
  timedOut: 'captcha.timedOut',
  resolved: 'captcha.resolvedElsewhere',
};

/**
 * The heads-up for one notice, over whatever screen is open. Given `onOpen`, a
 * press opens the list; it leaves by itself after BANNER_MS.
 */
function Banner({
  notice,
  instance,
  onOpen,
  onClose,
}: {
  notice: CaptchaNotice;
  instance: string;
  onOpen?: () => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const line = t(NOTICE_TEXT[notice.kind], { host: notice.challenge.host || '?' });
  // Warn for a captcha that wants an answer, info for news about one, the
  // families the web UI's toasts use for the same three.
  const bar = notice.kind === 'arrived' ? c.statusWarnSolid : c.statusInfoSolid;
  const { n } = useMotion();
  const v = useRef(new Animated.Value(n.toast === 0 ? 1 : 0)).current;
  const close = useRef(onClose);
  close.current = onClose;

  useEffect(() => {
    if (n.toast > 0) {
      Animated.timing(v, { toValue: 1, duration: n.toast, easing: Easing.out(Easing.ease), useNativeDriver: true }).start();
    }
    const timer = setTimeout(() => close.current(), BANNER_MS);
    return () => clearTimeout(timer);
    // Once per mount: the banner is keyed by its challenge, and a level picked
    // while it shows shapes the next one.
  }, []);

  return (
    <Animated.View
      pointerEvents="box-none"
      style={[
        styles.slot,
        { opacity: v, transform: [{ translateY: v.interpolate({ inputRange: [0, 1], outputRange: [-n.travel, 0] }) }] },
      ]}
    >
      {/* The close badge beside the pressable part rather than inside it: a
          touchable inside another is one element to TalkBack, and the badge
          could not be reached on its own. */}
      <View style={[styles.banner, { backgroundColor: c.surface, ...corners.card }]}>
        {onOpen ? (
          <CardButton style={styles.open} onPress={onOpen} accessibilityLabel={line}>
            <BannerText line={line} instance={instance} bar={bar} />
          </CardButton>
        ) : (
          <View style={styles.open}>
            <BannerText line={line} instance={instance} bar={bar} />
          </View>
        )}
        <IconBadge icon={<Cross color={c.textSub} />} onPress={onClose} accessibilityLabel={t('captcha.close')} />
      </View>
    </Animated.View>
  );
}

function BannerText({ line, instance, bar }: { line: string; instance: string; bar: string }) {
  const { c, corners } = useAppearance();
  return (
    <>
      <View style={[styles.mark, { backgroundColor: bar, ...corners.pill }]} />
      <View style={styles.text}>
        <Text style={[styles.line, { color: c.text }]} numberOfLines={2}>
          {line}
        </Text>
        <Text style={[styles.instance, { color: c.textMuted }]} numberOfLines={1}>
          {instance}
        </Text>
      </View>
    </>
  );
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  // Level with the screens' own top bars, which start 56 down, and capped like
  // their content.
  slot: { position: 'absolute', top: 48, start: 0, end: 0, paddingHorizontal: 16, alignItems: 'center' },
  banner: {
    width: '100%',
    maxWidth: 640,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    padding: 14,
    elevation: 6,
    shadowColor: '#000',
    shadowOpacity: 0.3,
    shadowRadius: 10,
    shadowOffset: { width: 0, height: 4 },
  },
  open: { flex: 1, minWidth: 0, flexDirection: 'row', alignItems: 'center', gap: 12 },
  // A status family's solid, as a bar at the leading edge: the colour says how
  // urgent it is and the sentence says what.
  mark: { width: 4, alignSelf: 'stretch' },
  text: { flex: 1, minWidth: 0, gap: 2 },
  line: { fontSize: TYPE.body, fontWeight: '500' },
  instance: { fontSize: TYPE.dense },
});
