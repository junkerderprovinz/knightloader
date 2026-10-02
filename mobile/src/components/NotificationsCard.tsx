import { useCallback, useEffect, useState } from 'react';
import { AppState, Linking, StyleSheet, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import { KnightWatch } from '../../modules/watch';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { anyKind, loadNotifyPrefs, saveNotifyPrefs, type NotifyPrefs } from '../watch/prefs';
import { askNotifications, notificationsAllowed, stopWatch } from '../watch/watch';
import { GlimButton, GlimRow, GlimToggle, NotchCard } from './glim';
import { Check } from './IconBadge';
import { Text } from './Text';

type Kind = 'captcha' | 'finished' | 'failed';

const KINDS: { kind: Kind; label: 'settings.notifyCaptcha' | 'settings.notifyFinished' | 'settings.notifyFailed' }[] = [
  { kind: 'captcha', label: 'settings.notifyCaptcha' },
  { kind: 'finished', label: 'settings.notifyFinished' },
  { kind: 'failed', label: 'settings.notifyFailed' },
];

/**
 * The settings card for the background watch: one switch per kind of
 * notification, and the way back to Android's permission when it is missing.
 * Absent where the watch does not exist, which is everywhere but Android.
 */
export function NotificationsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { c } = useAppearance();
  const [prefs, setPrefs] = useState<NotifyPrefs | null>(null);
  const [allowed, setAllowed] = useState(true);

  const recheck = useCallback(() => {
    void notificationsAllowed().then(setAllowed);
  }, []);

  useEffect(() => {
    void loadNotifyPrefs().then(setPrefs);
  }, []);

  // Read again on the way back from Android's settings, where the answer changes.
  useFocusEffect(recheck);
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'active') recheck();
    });
    return () => sub.remove();
  }, [recheck]);

  if (!KnightWatch || !prefs) return null;

  const allow = async () => {
    if (await askNotifications()) setAllowed(true);
    else void Linking.openSettings();
  };

  const flip = (kind: Kind, on: boolean) => {
    const next = { ...prefs, [kind]: on };
    setPrefs(next);
    void saveNotifyPrefs(next);
    if (!anyKind(next)) stopWatch();
    else if (on && !allowed) void allow();
  };

  return (
    <NotchCard title={t('settings.notifications')} hue={hue} info={t('settings.notificationsHint')}>
      {!allowed && (
        <View style={styles.blocked}>
          <Text style={[styles.blockedText, { color: c.textSub }]}>{t('settings.notificationsBlocked')}</Text>
          <GlimButton
            hue={0}
            label={t('settings.notificationsAllow')}
            icon={(ink) => <Check color={ink} />}
            onPress={() => void allow()}
          />
        </View>
      )}
      {KINDS.map(({ kind, label }, i) => (
        <GlimRow
          key={kind}
          label={t(label)}
          control={<GlimToggle hue={i} value={prefs[kind]} onChange={(on) => flip(kind, on)} />}
        />
      ))}
    </NotchCard>
  );
}

const styles = StyleSheet.create({
  blocked: { gap: 8, marginBottom: 10 },
  blockedText: { fontSize: TYPE.body },
});
