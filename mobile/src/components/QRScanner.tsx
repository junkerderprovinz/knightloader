import { useEffect, useRef, useState } from 'react';
import { AppState, Linking, Modal, PermissionsAndroid, StyleSheet, View } from 'react-native';
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
// modules/qr-scanner reads the live frames with ZXing. expo-camera stays out
// of the app: even with its scanner switched off its code refers to Google's
// ML Kit, and F-Droid's scanner rejects the APK for those references.
const CAMERA = PermissionsAndroid.PERMISSIONS.CAMERA;

export default function QRScanner({ visible, onScanned, onClose, hint }: { visible: boolean; onScanned: (data: string) => void; onClose: () => void; hint: string }) {
  const { t } = useT();
  const { c, accent, corners } = useAppearance();
  const { motion } = useMotion();
  const [granted, setGranted] = useState<boolean | null>(null);
  // The component stays mounted across opens and closes, rendering null, so a
  // code reported once must not lock out the next opening.
  const reported = useRef(false);
  useEffect(() => {
    if (!visible) return;
    reported.current = false;
    const look = () => void PermissionsAndroid.check(CAMERA).then(setGranted);
    look();
    // The camera may have been allowed in Android's settings meanwhile.
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'active') look();
    });
    return () => sub.remove();
  }, [visible]);

  // After a second refusal Android stops asking, so only its settings page can
  // grant the camera. The page opens on the tap after that refusal rather than
  // with it, which would answer "Don't allow" with a settings screen; the
  // button says where it leads instead.
  const [blocked, setBlocked] = useState(false);
  const requestPermission = async () => {
    if (blocked) {
      void Linking.openSettings();
      return;
    }
    const result = await PermissionsAndroid.request(CAMERA);
    setBlocked(result === PermissionsAndroid.RESULTS.NEVER_ASK_AGAIN);
    setGranted(result === PermissionsAndroid.RESULTS.GRANTED);
  };

  if (!visible) return null;

  return (
    <Modal visible={visible} animationType={motion === 'off' ? 'none' : 'slide'} onRequestClose={onClose}>
      <View style={[styles.container, { backgroundColor: c.bg }]}>
        {granted === null ? (
          <View style={styles.center} />
        ) : !granted ? (
          <View style={styles.center}>
            <Text style={[styles.hint, { color: c.text }]}>{t('qr.cameraPermissionHint')}</Text>
            <GlimButton hue={0} label={t(blocked ? 'qr.openSettings' : 'qr.grantAccess')} onPress={requestPermission} />
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
