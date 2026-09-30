import { useEffect, useRef } from 'react';
import { Modal, StyleSheet, View } from 'react-native';
import { useCameraPermissions } from 'expo-camera';
import { QrScannerView } from '../../modules/qr-scanner';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton } from './glim';
import { Cross } from './IconBadge';
import { Text } from './Text';

// A full-screen modal scanner rather than a screen of its own: a caller that
// wants a QR code needs one decoded string back rather than a spot in the
// navigation stack.
//
// The camera's own barcode scanner is Google's ML Kit, which is not free
// software and keeps the app out of F-Droid. modules/qr-scanner reads the live
// frames with ZXing instead.
export default function QRScanner({ visible, onScanned, onClose, hint }: { visible: boolean; onScanned: (data: string) => void; onClose: () => void; hint: string }) {
  const { t } = useT();
  const { c, accent, corners } = useAppearance();
  const { motion } = useMotion();
  const [permission, requestPermission] = useCameraPermissions();
  // The component stays mounted across opens and closes, rendering null, so a
  // code reported once must not lock out the next opening.
  const reported = useRef(false);
  useEffect(() => {
    if (visible) reported.current = false;
  }, [visible]);

  if (!visible) return null;

  return (
    <Modal visible={visible} animationType={motion === 'off' ? 'none' : 'slide'} onRequestClose={onClose}>
      <View style={[styles.container, { backgroundColor: c.bg }]}>
        {!permission ? (
          <View style={styles.center} />
        ) : !permission.granted ? (
          <View style={styles.center}>
            <Text style={[styles.hint, { color: c.text }]}>{t('qr.cameraPermissionHint')}</Text>
            <GlimButton hue={0} label={t('qr.grantAccess')} onPress={requestPermission} />
          </View>
        ) : (
          <>
            <QrScannerView
              style={StyleSheet.absoluteFill}
              onCode={({ nativeEvent }) => {
                if (reported.current) return;
                reported.current = true;
                onScanned(nativeEvent.data);
              }}
            />
            <View style={styles.overlay} pointerEvents="none">
              <View style={[styles.frame, { borderColor: accent, ...corners.card }]} />
              <Text style={[styles.hint, { color: c.text }]}>{hint}</Text>
            </View>
          </>
        )}
        {/* The way out is a button in the window's bottom row, with its words
            and its glyph like every other button (GlimStone 2.6.0), rather
            than a pill in the corner. Quiet, because leaving is not what this
            window is for. */}
        <View style={styles.footer}>
          <GlimButton tone="quiet" label={t('qr.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
        </View>
      </View>
    </Modal>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 16 },
  overlay: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 24 },
  frame: {
    width: 240,
    height: 240,
    borderWidth: 2,
  },
  hint: { fontSize: TYPE.body, textAlign: 'center', paddingHorizontal: 32 },
  // Over the camera, clear of the gesture bar, ending the row like every
  // window's footer.
  footer: { position: 'absolute', start: 24, end: 24, bottom: 40, flexDirection: 'row', justifyContent: 'flex-end' },
});
