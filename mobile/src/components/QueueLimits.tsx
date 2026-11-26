import { useEffect, useState } from 'react';
import { ActivityIndicator, Modal, Pressable, StyleSheet, View } from 'react-native';
import { errorText } from '../api/client';
import { fetchLimits, setLimits, type QueueLimits as Limits } from '../api/listActions';
import type { ServerConnection } from '../api/types';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { NUM, TYPE } from '../theme/tokens';
import { GlimButton, GlimRow, NotchCard, WellSelector } from './glim';
import IconBadge, { Confirm, Cross } from './IconBadge';
import { RATE_UNITS, amountIn, bytesOf, countOf, stepCount, unitFor, type RateUnit } from './limitForm';
import { NoArrival } from './Moving';
import { Text, TextInput } from './Text';

/**
 * QueueLimits is the web's quick settings for the two values people reach for
 * while downloads run: the speed limit and how many run at once. They are read
 * each time the window opens, so a change made on the web shows, and saved
 * together through the queue's own route, since the settings stay closed to a
 * phone. A window like the confirmation, with the way out first and Save at
 * the end of the row.
 */
export function QueueLimits({
  conn,
  visible,
  onClose,
  onSaved,
}: {
  conn: ServerConnection;
  visible: boolean;
  onClose: () => void;
  /** After a save, so the speed graph can show the new limit. */
  onSaved: (limits: Limits) => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const { motion } = useMotion();
  const [stored, setStored] = useState<Limits | null>(null);
  const [unit, setUnit] = useState<RateUnit>(RATE_UNITS[1]);
  const [speedText, setSpeedText] = useState('');
  const [countText, setCountText] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!visible) return;
    let alive = true;
    setStored(null);
    setError('');
    fetchLimits(conn).then(
      (l) => {
        if (!alive) return;
        const u = unitFor(l.speedLimit);
        setStored(l);
        setUnit(u);
        setSpeedText(amountIn(l.speedLimit, u));
        setCountText(String(l.maxConcurrent));
      },
      (e) => alive && setError(errorText(t, e)),
    );
    return () => {
      alive = false;
    };
  }, [visible, conn, t]);

  const speed = bytesOf(speedText, unit);
  const count = countOf(countText);

  // Switching the unit keeps the amount the field shows, the way a unit typed
  // after the digits does on the web, so "2" in kB/s becomes 2 MB/s.
  const pickUnit = (label: RateUnit['label']) => setUnit(RATE_UNITS.find((u) => u.label === label) ?? RATE_UNITS[1]);

  const step = (by: 1 | -1) => setCountText(String(stepCount(count ?? stored?.maxConcurrent ?? 1, by)));

  const save = async () => {
    if (speed === null || count === null) return;
    setBusy(true);
    setError('');
    try {
      const saved = await setLimits(conn, { speedLimit: speed, maxConcurrent: count });
      onSaved(saved);
      onClose();
    } catch (e) {
      setError(errorText(t, e));
    } finally {
      setBusy(false);
    }
  };

  const field = [styles.field, NUM, { backgroundColor: c.surface2, color: c.text, ...corners.control }];

  return (
    <Modal visible={visible} transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={onClose}>
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={onClose}>
          {/* Swallows the press so a tap on the card itself closes nothing. */}
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title={t('quick.title')} style={styles.flush}>
              {stored === null && error === '' ? (
                <ActivityIndicator style={styles.loading} color={c.textMuted} />
              ) : stored !== null ? (
                <View style={styles.rows}>
                  <GlimRow label={t('settings.globalSpeedLimit')} info={t('settings.speedHint')} />
                  {/* The figure and its unit read left to right in every
                      language, as every figure with a unit does. */}
                  <View style={styles.ltr}>
                    <TextInput
                      style={[field, styles.speed]}
                      value={speedText}
                      onChangeText={setSpeedText}
                      keyboardType="decimal-pad"
                      accessibilityLabel={`${t('settings.globalSpeedLimit')}, ${unit.label}`}
                      selectTextOnFocus
                    />
                    <View style={styles.units}>
                      <WellSelector
                        options={RATE_UNITS.map((u) => ({ value: u.label, label: u.label }))}
                        value={unit.label}
                        onPick={pickUnit}
                        hue={0}
                      />
                    </View>
                  </View>

                  <GlimRow
                    label={t('settings.maxConcurrent')}
                    info={t('settings.maxConcurrentHint')}
                    control={
                      <View style={styles.stepper}>
                        <IconBadge symbol="−" onPress={() => step(-1)} accessibilityLabel={`${t('settings.maxConcurrent')} −1`} />
                        <TextInput
                          style={[field, styles.count]}
                          value={countText}
                          onChangeText={setCountText}
                          keyboardType="number-pad"
                          accessibilityLabel={t('settings.maxConcurrent')}
                          selectTextOnFocus
                        />
                        <IconBadge symbol="+" onPress={() => step(1)} accessibilityLabel={`${t('settings.maxConcurrent')} +1`} />
                      </View>
                    }
                  />
                </View>
              ) : null}

              {error !== '' && (
                <Text style={[styles.error, { color: c.statusFailSolid }]} numberOfLines={3}>
                  {error}
                </Text>
              )}

              <View style={styles.actions}>
                <GlimButton tone="quiet" grow label={t('settings.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
                <GlimButton
                  grow
                  label={t('settings.save')}
                  icon={(ink) => <Confirm color={ink} />}
                  busy={busy}
                  disabled={stored === null || speed === null || count === null}
                  onPress={save}
                />
              </View>
            </NotchCard>
          </Pressable>
        </Pressable>
      </NoArrival>
    </Modal>
  );
}

const styles = StyleSheet.create({
  // No backgroundColor here: it is `c.scrim`, applied at render time, so it
  // follows the theme.
  scrim: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24 },
  window: { width: '100%', maxWidth: 420 },
  flush: { marginTop: 0 },
  loading: { marginVertical: 24 },
  rows: { gap: 12, marginTop: 4 },
  ltr: { direction: 'ltr', flexDirection: 'row', alignItems: 'center', gap: 8 },
  field: { height: 40, paddingHorizontal: 12, fontSize: TYPE.body },
  speed: { flex: 1, minWidth: 0 },
  units: { flex: 1.5, minWidth: 0 },
  stepper: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  count: { width: 56, textAlign: 'center', paddingHorizontal: 4 },
  error: { marginTop: 12, fontSize: TYPE.subline },
  actions: { flexDirection: 'row', gap: 8, marginTop: 20 },
});
