import { useCallback, useEffect, useState } from 'react';
import { AppState, Linking, StyleSheet, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import { KnightWatch } from '../../modules/watch';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { anyKind, loadNotifyPrefs, saveNotifyPrefs, type NotifyPrefs } from '../watch/prefs';
import { askNotifications, notificationsAllowed, startWatch, stopWatch } from '../watch/watch';
import { GlimButton, GlimRow, GlimToggle, NotchCard } from './glim';
import { Check, Gear } from './IconBadge';
import { Text } from './Text';

type Kind = 'captcha' | 'finished' | 'failed';

const KINDS: { kind: Kind; label: 'settings.notifyCaptcha' | 'settings.notifyFinished' | 'settings.notifyFailed' }[] = [
  { kind: 'captcha', label: 'settings.notifyCaptcha' },
  { kind: 'finished', label: 'settings.notifyFinished' },
  { kind: 'failed', label: 'settings.notifyFailed' },
];

/**
 * The settings card for the background watch: one switch per kind of
 * notification, whether to stay connected all the time, the way back to
 * Android's permission when it is missing, and the two places a phone can stop
 * the watch behind the app's back: battery optimisation and the maker's own
 * background rules. Absent where the watch does not exist, which is everywhere
 * but Android.
 */
export function NotificationsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { c } = useAppearance();
  const [prefs, setPrefs] = useState<NotifyPrefs | null>(null);
  const [allowed, setAllowed] = useState(true);
  const [exempt, setExempt] = useState(true);

  const recheck = useCallback(() => {
    void notificationsAllowed().then(setAllowed);
    setExempt(KnightWatch?.batteryExempt() ?? true);
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
  const vendor = KnightWatch.vendor();

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

  const flipStay = (on: boolean) => {
    const next = { ...prefs, stay: on };
    setPrefs(next);
    void saveNotifyPrefs(next).then(() => {
      // Off lets a running service end by itself once nothing runs.
      if (on) void startWatch();
      else KnightWatch?.autostart(false);
    });
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
      {/* With every kind off there is no connection to keep, and the rows
          about keeping it would be about nothing. */}
      {anyKind(prefs) && (
        <>
          <GlimRow
            label={t('settings.notifyStay')}
            info={t('settings.notifyStayHint')}
            control={<GlimToggle hue={3} value={prefs.stay} onChange={flipStay} />}
          />
          {!exempt && (
            <View style={styles.extra}>
              <GlimRow label={t('settings.battery')} sub={t('settings.batteryOn')} info={t('settings.batteryHint')} />
              <GlimButton
                tone="quiet"
                label={t('settings.batteryOpen')}
                icon={(ink) => <Gear color={ink} />}
                onPress={() => KnightWatch?.openBatterySettings()}
              />
            </View>
          )}
          {vendor && (
            <View style={styles.extra}>
              <GlimRow
                label={t('settings.vendor', { brand: vendor })}
                info={t('settings.vendorHint', { brand: vendor })}
              />
              <GlimButton
                tone="quiet"
                label={t('settings.vendorOpen', { brand: vendor })}
                icon={(ink) => <Gear color={ink} />}
                onPress={() => KnightWatch?.openVendorSettings()}
              />
            </View>
          )}
        </>
      )}
    </NotchCard>
  );
}

const styles = StyleSheet.create({
  blocked: { gap: 8, marginBottom: 10 },
  extra: { gap: 4, marginBottom: 10 },
  blockedText: { fontSize: TYPE.body },
});
