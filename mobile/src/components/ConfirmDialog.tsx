import type { ReactNode } from 'react';
import { Modal, Pressable, StyleSheet, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { GlimButton, NotchCard } from './glim';
import { Cross } from './IconBadge';
import { NoArrival } from './Moving';
import { Text } from './Text';

/**
 * The confirmation window, GlimStone's ConfirmDialog in React Native, in place
 * of Alert.alert. The system dialog follows none of this app's engines and its
 * buttons cannot carry a glyph, while a window is a window whoever draws it
 * (rule 15): a card, its title as a badge, the question, and a row at the
 * bottom.
 *
 * The way out comes first and the button that goes ahead ends the row, mirrored
 * in a right-to-left language, and each carries its words and its glyph
 * (GlimStone 2.6.0). Neither takes a status colour or the accent: the question
 * states the stakes, and somebody who read it has been told. No corner X, since
 * the row already answers; the back gesture and a press on the scrim cancel.
 */
export function ConfirmDialog({
  visible,
  title,
  message,
  cancelLabel,
  confirmLabel,
  confirmIcon,
  onCancel,
  onConfirm,
}: {
  visible: boolean;
  title: string;
  message: string;
  cancelLabel: string;
  confirmLabel: string;
  /** The glyph of the act being confirmed, which no fixed key can name. */
  confirmIcon: (ink: string) => ReactNode;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { c } = useAppearance();
  const { motion } = useMotion();
  return (
    <Modal visible={visible} transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={onCancel}>
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={onCancel}>
          {/* Swallows the press so a tap on the card itself cancels nothing. */}
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title={title} style={styles.flush}>
              <Text style={[styles.message, { color: c.textSub }]}>{message}</Text>
              <View style={styles.actions}>
                <GlimButton tone="quiet" grow label={cancelLabel} icon={(ink) => <Cross color={ink} />} onPress={onCancel} />
                <GlimButton tone="quiet" grow label={confirmLabel} icon={confirmIcon} onPress={onConfirm} />
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
  message: { fontSize: TYPE.body, lineHeight: 20, marginTop: 4 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 20 },
});
