import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, StyleSheet, View } from 'react-native';
import { captchaErrorText } from '../api/client';
import { expiryMs } from '../api/captcha';
import type { ServerConnection } from '../api/types';
import { CaptchaCard } from '../components/CaptchaCard';
import { useCaptchas } from '../components/CaptchaWatch';
import IconBadge, { Back, Check, boxForInk } from '../components/IconBadge';
import { Arrive, MovingList } from '../components/Moving';
import { Text } from '../components/Text';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';

/**
 * Every captcha waiting on the active connection, nearest deadline first, each
 * answered in its own card. The list is CaptchaWatch's, so this screen and the
 * banner never disagree about what is waiting; pulling the list down has the
 * instance ask JD at once.
 */
export default function CaptchasScreen({ conn, onBack }: { conn: ServerConnection; onBack: () => void }) {
  const { t } = useT();
  const { c, accentInk, corners } = useAppearance();
  const { list, loaded, error, reload, refresh } = useCaptchas();
  const [now, setNow] = useState(() => Date.now());
  const [pulling, setPulling] = useState(false);
  // What an answer left to say after its card went, such as that it came too
  // late.
  const [note, setNote] = useState('');

  // Ticks only while a deadline is on screen.
  const timed = list.some((ch) => expiryMs(ch) !== null);
  useEffect(() => {
    if (!timed) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [timed]);

  const settled = useCallback(
    (said?: string) => {
      setNote(said ?? '');
      void reload();
    },
    [reload],
  );

  const pull = async () => {
    setPulling(true);
    setNote('');
    try {
      await refresh();
    } catch {
      // The watch's next look reports what is wrong.
    } finally {
      setPulling(false);
    }
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
        <View style={styles.titles}>
          <Text style={[styles.title, { color: c.text }]}>{t('captcha.screenTitle')}</Text>
          <Text style={[styles.instance, { color: c.textMuted }]} numberOfLines={1}>
            {conn.name}
          </Text>
        </View>
      </View>

      <MovingList
        data={list}
        keyExtractor={(ch) => ch.id}
        extraData={[conn, now, settled]}
        // A tap on Continue while the keyboard is up answers rather than only
        // closing the keyboard.
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl refreshing={pulling} onRefresh={pull} colors={[accentInk]} progressBackgroundColor={c.surface} />
        }
        contentContainerStyle={styles.list}
        ListHeaderComponent={
          error || note ? (
            <Text style={[styles.error, { color: c.statusFailText }]}>
              {error ? captchaErrorText(t, conn, error) : note}
            </Text>
          ) : null
        }
        renderItem={({ item, index }) => (
          <CaptchaCard conn={conn} challenge={item} hue={index} now={now} onSettled={settled} />
        )}
        ListEmptyComponent={
          loaded ? (
            <Arrive style={[styles.empty, { backgroundColor: c.surface, ...corners.card }]}>
              <View style={styles.emptyIcon}>
                <Check color={c.textMuted} size={boxForInk(26)} />
              </View>
              <Text style={[styles.emptyText, { color: c.textMuted }]}>{t('captcha.empty')}</Text>
            </Arrive>
          ) : null
        }
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { ...capped, flexDirection: 'row', alignItems: 'center', gap: 12, padding: 16, paddingTop: 56 },
  titles: { flex: 1, minWidth: 0 },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  instance: { fontSize: TYPE.dense, marginTop: 2 },
  list: { ...capped, paddingHorizontal: 16, paddingBottom: 32 },
  error: { fontSize: TYPE.dense, marginTop: 8 },
  empty: { alignItems: 'center', marginTop: 48, gap: 14, paddingVertical: 28, paddingHorizontal: 24 },
  emptyIcon: { opacity: 0.5 },
  emptyText: { fontSize: TYPE.body, textAlign: 'center' },
});
