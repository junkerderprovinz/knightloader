import { useState } from 'react';
import { ActivityIndicator, Linking, Modal, StyleSheet, View } from 'react-native';
import { WebView } from 'react-native-webview';
import { captchaWidgetSource } from '../api/client';
import { WIDGET_BRIDGE, widgetFailure, widgetMessage } from '../api/captcha';
import type { CaptchaChallenge, CaptchaWidgetPayload, DirectConnection } from '../api/types';
import { useT, type TranslationKey } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE, inkFor } from '../theme/tokens';
import { GlimButton } from './glim';
import IconBadge, { Cross } from './IconBadge';
import { InfoTip } from './InfoTip';
import { NoArrival } from './Moving';
import { Text } from './Text';

type Status = 'loading' | 'ready' | 'expired' | 'error' | 'unsolvable';

// Why the widget page gave up before loading any vendor script.
const UNSOLVABLE_WHY: Partial<Record<string, TranslationKey>> = {
  vendor: 'captcha.unsolvableVendor',
  turnstile: 'captcha.unsolvableTurnstile',
  action: 'captcha.unsolvableAction',
};

/**
 * A reCAPTCHA or hCaptcha challenge, solved in the instance's own widget page
 * (internal/api/routes_captcha_widget.go). It is the page the web UI puts in an
 * iframe, loaded from the same address with its own Content-Security-Policy, so
 * the vendor sees the origin a browser would show it and the app runs no vendor
 * script of its own.
 *
 * A whole window rather than a box in the card, because the vendor's picture
 * grid opens over the page at its own size, and a box would cut it off.
 */
export function CaptchaWidget({
  conn,
  challenge,
  onSolved,
  onClose,
}: {
  conn: DirectConnection;
  challenge: CaptchaChallenge;
  onSolved: (token: string) => void;
  onClose: () => void;
}) {
  const { t, lang } = useT();
  const { c, accent, corners } = useAppearance();
  const { motion } = useMotion();
  const [status, setStatus] = useState<Status>('loading');
  const [detail, setDetail] = useState<string | null>(null);
  // Set when the instance answered the page itself with an error status.
  const [httpStatus, setHttpStatus] = useState<number | null>(null);
  // Bumped by Refresh, which mounts a fresh WebView rather than reloading the
  // old one, so a vendor script that wedged the page goes with it.
  const [round, setRound] = useState(0);

  const fail = (kind: 'error' | 'unsolvable', why: string | null) => {
    setStatus(kind);
    setDetail(why);
  };

  const onMessage = (raw: string) => {
    const m = widgetMessage(raw, challenge.id);
    if (!m) return;
    if (m.kind === 'ready') setStatus((s) => (s === 'loading' ? 'ready' : s));
    else if (m.kind === 'expired') setStatus('expired');
    else if (m.kind === 'error' || m.kind === 'unsolvable') fail(m.kind, m.detail);
    else if (m.kind === 'solved' && m.detail) onSolved(m.detail);
  };

  const why = status === 'unsolvable' && detail ? UNSOLVABLE_WHY[detail] : undefined;
  const failure = widgetFailure(httpStatus, detail);
  const failureText =
    failure.by === 'instance'
      ? t('captcha.widgetPageRefused', { code: failure.status })
      : failure.by === 'vendor'
        ? t('captcha.widgetRefused', { code: failure.code })
        : t('captcha.widgetUnreachable');
  // A score-based reCAPTCHA has nothing to tap: the page asks for the token
  // itself.
  const hint = (challenge.payload as CaptchaWidgetPayload | undefined)?.v3Action
    ? t('captcha.widgetScoreHint')
    : t('captcha.widgetHint');

  return (
    <Modal visible animationType={motion === 'off' ? 'none' : 'slide'} onRequestClose={onClose}>
      <NoArrival>
        <View style={[styles.window, { backgroundColor: c.bg }]}>
          <View style={styles.topBar}>
            <View style={styles.titles}>
              <View style={styles.titleLine}>
                <Text style={[styles.title, { color: c.text }]} numberOfLines={1}>
                  {challenge.host || '?'}
                </Text>
                <InfoTip text={hint} />
              </View>
              {challenge.prompt ? (
                <Text style={[styles.prompt, { color: c.textSub }]} numberOfLines={2}>
                  {challenge.prompt}
                </Text>
              ) : null}
            </View>
            <IconBadge icon={<Cross color={c.textSub} />} onPress={onClose} accessibilityLabel={t('captcha.close')} />
          </View>

          {status === 'error' || status === 'unsolvable' ? (
            <View style={[styles.gone, { backgroundColor: c.surface, ...corners.card }]}>
              <Text style={[styles.goneText, { color: status === 'error' ? c.statusFailText : c.text }]}>
                {status === 'error' ? t('captcha.widgetUnavailable') : t('captcha.unsolvable')}
              </Text>
              {status === 'error' ? (
                <InfoTip text={failureText} />
              ) : why ? (
                <InfoTip text={t(why)} />
              ) : null}
            </View>
          ) : (
            // The page is drawn on white, as the vendors' widgets expect, so the
            // box around it is white in both themes.
            <View style={[styles.page, corners.control]}>
              <WebView
                key={round}
                source={captchaWidgetSource(conn, challenge, lang)}
                // The page carries no viewport of its own, and a wide one would
                // draw the checkbox at a third of its size.
                scalesPageToFit={false}
                domStorageEnabled
                injectedJavaScriptBeforeContentLoaded={WIDGET_BRIDGE}
                injectedJavaScript={WIDGET_BRIDGE}
                onMessage={(e) => onMessage(e.nativeEvent.data)}
                onLoadEnd={() => setStatus((s) => (s === 'loading' ? 'ready' : s))}
                onError={() => fail('error', null)}
                onHttpError={(e) => {
                  setHttpStatus(e.nativeEvent.statusCode);
                  fail('error', null);
                }}
                // The vendors' privacy and terms links open a window, which
                // belongs in the phone's browser rather than over the challenge.
                onOpenWindow={(e) => {
                  const url = e.nativeEvent.targetUrl;
                  if (/^https?:\/\//.test(url)) void Linking.openURL(url);
                }}
                style={styles.webview}
              />
              {status === 'loading' && (
                <View style={styles.loading} pointerEvents="none">
                  {/* The accent as ink on white in either theme, since the page
                      under it is white in both. */}
                  <ActivityIndicator color={inkFor(accent)} size="large" />
                </View>
              )}
            </View>
          )}

          {status === 'expired' && (
            <Text style={[styles.note, { color: c.statusFailText }]}>{t('captcha.tooLate')}</Text>
          )}

          <View style={styles.actions}>
            <GlimButton
              tone="quiet"
              grow
              label={t('captcha.refresh')}
              onPress={() => {
                setStatus('loading');
                setDetail(null);
                setHttpStatus(null);
                setRound((r) => r + 1);
              }}
            />
          </View>
        </View>
      </NoArrival>
    </Modal>
  );
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  window: { flex: 1, padding: 16, paddingTop: 56, gap: 12 },
  topBar: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  titles: { flex: 1, minWidth: 0, gap: 2 },
  titleLine: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  title: { fontSize: TYPE.heading, fontWeight: '600', flexShrink: 1 },
  prompt: { fontSize: TYPE.dense },
  page: { flex: 1, overflow: 'hidden', backgroundColor: '#fff' },
  webview: { flex: 1, backgroundColor: '#fff' },
  loading: { position: 'absolute', top: 0, start: 0, end: 0, bottom: 0, alignItems: 'center', justifyContent: 'center' },
  gone: { flexDirection: 'row', alignItems: 'center', gap: 8, padding: 16 },
  goneText: { fontSize: TYPE.body, flexShrink: 1 },
  note: { fontSize: TYPE.dense },
  actions: { flexDirection: 'row', gap: 8, paddingBottom: 16 },
});
