import { useEffect, useState } from 'react';
import { Image, Pressable, StyleSheet, View } from 'react-native';
import { answerCaptcha, captchaErrorText, skipCaptcha } from '../api/client';
import {
  answerableHere,
  clickAnswer,
  fmtCountdown,
  secondsLeft,
  solverStatus,
  widgetRuns,
  type ClickPoint,
  type Phrase,
} from '../api/captcha';
import {
  isRelayConnection,
  type CaptchaAbortScope,
  type CaptchaChallenge,
  type CaptchaImagePayload,
  type CaptchaUnsupportedPayload,
  type CaptchaWidgetPayload,
  type ServerConnection,
} from '../api/types';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useCaptchas } from './CaptchaWatch';
import { CaptchaWidget } from './CaptchaWidget';
import { GlimButton, NotchCard, UnavailableNotice } from './glim';
import { Check, Cross, Play } from './IconBadge';
import { InfoTip } from './InfoTip';
import { Text, TextInput } from './Text';

// The vendors' own spelling of their names.
const VENDOR_NAMES: Record<string, string> = {
  recaptcha: 'reCAPTCHA',
  hcaptcha: 'hCaptcha',
  turnstile: 'Cloudflare Turnstile',
};

/**
 * One waiting captcha: who asks, how long is left, and the way to answer it
 * that its kind allows. Image and click challenges are answered here, a widget
 * challenge in CaptchaWidget's window.
 */
export function CaptchaCard({
  conn,
  challenge,
  hue,
  now,
  onSettled,
}: {
  conn: ServerConnection;
  challenge: CaptchaChallenge;
  hue: number;
  now: number;
  /** Called once the instance has taken an answer or a skip, with what the
   *  person should still read after this card has gone. */
  onSettled: (note?: string) => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const { settleHere } = useCaptchas();
  const [answer, setAnswer] = useState('');
  const [points, setPoints] = useState<ClickPoint[]>([]);
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [more, setMore] = useState(false);
  const [solving, setSolving] = useState(false);

  const kind = challenge.kind;
  const picture = (challenge.payload as CaptchaImagePayload | undefined)?.dataUrl;
  // What the hoster asks, or for a widget without a question which service it
  // is, since that decides what the window will show.
  const vendor = kind === 'widget' ? (challenge.payload as CaptchaWidgetPayload | undefined)?.vendor : undefined;
  const vendorName = vendor ? (VENDOR_NAMES[vendor] ?? vendor) : '';
  const runs = kind === 'widget' && widgetRuns(challenge);
  const line = challenge.prompt || (runs ? vendorName : '');
  const left = secondsLeft(challenge, now);
  const widgetHere = runs && !isRelayConnection(conn);
  // A widget the page cannot run is as far out of reach as an unknown kind.
  const unshown = kind === 'unsupported' || (kind === 'widget' && !runs);
  const unshownName =
    kind === 'unsupported' ? (challenge.payload as CaptchaUnsupportedPayload | undefined)?.vendor : vendorName;
  const solver = challenge.solver
    ? solverStatus(challenge.solver, now, answerableHere(challenge, isRelayConnection(conn)))
    : null;

  // The answer is in the picture's own pixels, so its real size is needed
  // before a click can be sent.
  useEffect(() => {
    if (!picture || (kind !== 'image' && kind !== 'click')) return;
    let alive = true;
    Image.getSize(
      picture,
      (w, h) => {
        if (alive) setNatural({ w, h });
      },
      () => {},
    );
    return () => {
      alive = false;
    };
  }, [picture, kind]);

  const send = async (text: string) => {
    setBusy(true);
    setError('');
    try {
      const { stillValid } = await settleHere(challenge.id, () => answerCaptcha(conn, challenge.id, text));
      onSettled(stillValid ? undefined : t('captcha.tooLate'));
    } catch (e) {
      setError(captchaErrorText(t, conn, e));
    } finally {
      setBusy(false);
    }
  };

  const skip = async (scope: CaptchaAbortScope) => {
    setBusy(true);
    setError('');
    try {
      await settleHere(challenge.id, () => skipCaptcha(conn, challenge.id, scope));
      onSettled();
    } catch (e) {
      setError(captchaErrorText(t, conn, e));
    } finally {
      setBusy(false);
    }
  };

  const ready =
    kind === 'image' ? answer.trim() !== '' : kind === 'click' ? points.length > 0 && natural !== null : false;
  const submit = () => {
    if (!ready || busy) return;
    void send(kind === 'click' && natural ? clickAnswer(points, natural.w, natural.h) : answer);
  };

  // A widget challenge explains itself in its own window.
  const hint = kind === 'click' ? t('captcha.clickHint') : unshown ? t('captcha.unsupportedHint') : undefined;

  return (
    <NotchCard title={challenge.host || '?'} hue={hue} info={hint}>
      {(line || left !== null) && (
        <View style={styles.meta}>
          <Text style={[styles.prompt, { color: c.textSub }]} numberOfLines={3}>
            {line}
          </Text>
          {left !== null && <Text style={[styles.clock, { color: c.textMuted }]}>{fmtCountdown(left)}</Text>}
        </View>
      )}
      {solver && <SolverLine status={solver} />}

      {kind === 'image' && picture && (
        <>
          <Picture uri={picture} natural={natural} />
          <TextInput
            style={[styles.field, { backgroundColor: c.surface2, color: c.text, ...corners.control }]}
            value={answer}
            onChangeText={setAnswer}
            placeholder={t('captcha.answerPlaceholder')}
            placeholderTextColor={c.textMuted}
            accessibilityLabel={t('captcha.answerLabel')}
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="send"
            onSubmitEditing={submit}
          />
        </>
      )}

      {kind === 'click' && picture && (
        <>
          <Picture
            uri={picture}
            natural={natural}
            points={points}
            onTap={(p) => setPoints((all) => [...all, p])}
          />
          <View style={styles.clickLine}>
            <Text style={[styles.clickCount, { color: c.textMuted }]}>{t('captcha.clickCount', { n: points.length })}</Text>
            {points.length > 0 && (
              <GlimButton tone="quiet" label={t('captcha.clickClear')} onPress={() => setPoints([])} />
            )}
          </View>
        </>
      )}

      {runs && !widgetHere && (
        <UnavailableNotice title={t('captcha.widgetRelayTitle')} reason={t('captcha.widgetRelayReason')} />
      )}

      {unshown && (
        <Text style={[styles.body, { color: c.text }]}>
          {/* A Turnstile is out of reach here but not for the paid solvers. */}
          {vendor === 'turnstile'
            ? t('captcha.unsolvableTurnstile')
            : t('captcha.unsupported', { vendor: unshownName || '?' })}
        </Text>
      )}

      {error !== '' && <Text style={[styles.error, { color: c.statusFailText }]}>{error}</Text>}

      {/* The way out first and the one that goes ahead at the end. */}
      <View style={styles.actions}>
        <GlimButton
          tone="quiet"
          grow
          label={t('captcha.cancel')}
          icon={(ink) => <Cross color={ink} />}
          disabled={busy}
          onPress={() => void skip('skip-once')}
        />
        {(kind === 'image' || kind === 'click') && (
          <GlimButton
            hue={hue}
            grow
            label={t('captcha.continue')}
            icon={(ink) => <Check color={ink} />}
            busy={busy}
            disabled={!ready}
            onPress={submit}
          />
        )}
        {widgetHere && (
          <GlimButton
            hue={hue}
            grow
            label={t('captcha.solve')}
            icon={(ink) => <Play color={ink} />}
            busy={busy}
            onPress={() => setSolving(true)}
          />
        )}
      </View>

      <View style={styles.more}>
        <GlimButton tone="quiet" label={t('captcha.moreOptions')} onPress={() => setMore((v) => !v)} />
        {more && (
          <>
            <GlimButton
              tone="quiet"
              label={t('captcha.blockHoster', { host: challenge.host || '?' })}
              disabled={busy}
              onPress={() => void skip('blacklist-hoster')}
            />
            <GlimButton
              tone="quiet"
              label={t('captcha.blockEverywhere')}
              disabled={busy}
              onPress={() => void skip('blacklist-everywhere')}
            />
          </>
        )}
      </View>

      {solving && !isRelayConnection(conn) && (
        <CaptchaWidget
          conn={conn}
          challenge={challenge}
          onClose={() => setSolving(false)}
          onSolved={(token) => {
            setSolving(false);
            void send(token);
          }}
        />
      )}
    </NotchCard>
  );
}

/** The paid solvers' state, with the why and every solver that did not deliver
 *  behind the (i), as the web UI's SolverStatus shows it. */
function SolverLine({ status }: { status: ReturnType<typeof solverStatus> }) {
  const { t } = useT();
  const { c } = useAppearance();
  const say = (p: Phrase) => t(p.key, p.vars);
  const tip = [status.hint, ...status.refusals]
    .filter((p): p is Phrase => p !== undefined)
    .map(say)
    .join('\n\n');
  return (
    <View style={styles.solver}>
      <Text style={[styles.solverText, { color: c.textMuted }]}>{say(status.line)}</Text>
      {tip !== '' && <InfoTip text={tip} />}
    </View>
  );
}

/**
 * The captcha's picture on white, the ground it was drawn for. Given `onTap`,
 * it takes taps and marks each one where it landed.
 */
function Picture({
  uri,
  natural,
  points,
  onTap,
}: {
  uri: string;
  natural: { w: number; h: number } | null;
  points?: ClickPoint[];
  onTap?: (p: ClickPoint) => void;
}) {
  const { t } = useT();
  const { accent, corners } = useAppearance();
  const [drawn, setDrawn] = useState<{ w: number; h: number } | null>(null);
  // Up to twice its own size: a captcha is small, and blown up further it
  // turns to mush without getting any easier to read.
  const size = natural
    ? { width: '100%' as const, maxWidth: natural.w * 2, aspectRatio: natural.w / natural.h }
    : { width: '100%' as const, aspectRatio: 3 };
  const image = (
    <Image source={{ uri }} style={styles.fill} resizeMode="contain" accessibilityLabel={t('captcha.title')} />
  );
  return (
    <View style={[styles.pictureGround, corners.control]}>
      {onTap ? (
        <Pressable
          style={size}
          onLayout={(e) => setDrawn({ w: e.nativeEvent.layout.width, h: e.nativeEvent.layout.height })}
          onPress={(e) => {
            if (!drawn) return;
            onTap({ x: e.nativeEvent.locationX / drawn.w, y: e.nativeEvent.locationY / drawn.h });
          }}
        >
          {image}
          {points?.map((p, i) => (
            <View
              key={i}
              pointerEvents="none"
              style={[styles.marker, corners.pill, { left: `${p.x * 100}%`, top: `${p.y * 100}%` }]}
            >
              <View style={[styles.markerDot, corners.pill, { backgroundColor: accent }]} />
            </View>
          ))}
        </Pressable>
      ) : (
        <View style={size}>{image}</View>
      )}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  meta: { flexDirection: 'row', alignItems: 'flex-start', gap: 12, marginBottom: 6 },
  prompt: { flex: 1, fontSize: TYPE.dense, lineHeight: 17 },
  // Rewritten every second, so the digits keep their width.
  clock: { fontSize: TYPE.dense, fontVariant: ['tabular-nums'] },
  solver: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 6 },
  solverText: { flexShrink: 1, fontSize: TYPE.dense, lineHeight: 17 },
  pictureGround: { backgroundColor: '#fff', padding: 8, alignItems: 'center', marginVertical: 6 },
  fill: { width: '100%', height: '100%' },
  // A dot centred on the tap, ringed in white so it shows on any picture. The
  // ring is a white disc under the dot rather than a border.
  marker: {
    position: 'absolute',
    width: 14,
    height: 14,
    marginLeft: -7,
    marginTop: -7,
    backgroundColor: '#fff',
    alignItems: 'center',
    justifyContent: 'center',
  },
  markerDot: { width: 10, height: 10 },
  field: { marginTop: 6, paddingHorizontal: 14, paddingVertical: 10, fontSize: TYPE.body },
  clickLine: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12, minHeight: 40 },
  clickCount: { fontSize: TYPE.dense },
  body: { fontSize: TYPE.body, lineHeight: 20, marginTop: 4 },
  error: { fontSize: TYPE.dense, marginTop: 8 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 12 },
  more: { alignItems: 'flex-start', gap: 8, marginTop: 8 },
});
