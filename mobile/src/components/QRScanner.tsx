import { useEffect, useState } from 'react';
import { Modal, StyleSheet, View } from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton } from './glim';
import { Cross } from './IconBadge';
import { Text } from './Text';

// A full-screen modal scanner rather than a screen of its own: a caller that
// wants a QR code needs one decoded string back rather than a spot in the
// navigation stack.
export default function QRScanner({ visible, onScanned, onClose, hint }: { visible: boolean; onScanned: (data: string) => void; onClose: () => void; hint: string }) {
  const { t } = useT();
  const { c, accent, radii } = useAppearance();
  const [permission, requestPermission] = useCameraPermissions();
  const [locked, setLocked] = useState(false);

  // The component stays mounted across opens and closes, rendering null, so
  // `locked` from an earlier scan would still be true the next time it opens
  // and every scan after the first would do nothing.
  useEffect(() => {
    if (visible) setLocked(false);
  }, [visible]);

  if (!visible) return null;

  const handleScanned = (data: string) => {
    if (locked) return;
    setLocked(true);
    onScanned(data);
  };

  return (
    <Modal visible={visible} animationType="slide" onRequestClose={onClose}>
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
            <CameraView
              style={StyleSheet.absoluteFill}
              facing="back"
              barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
              onBarcodeScanned={(result) => handleScanned(result.data)}
            />
            <View style={styles.overlay} pointerEvents="none">
              <View style={[styles.frame, { borderColor: accent, borderRadius: radii.card }]} />
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
