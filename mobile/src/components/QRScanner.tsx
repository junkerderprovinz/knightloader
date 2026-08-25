import { useEffect, useState } from 'react';
import { Modal, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';

// A full-screen modal scanner, not a screen of its own: every place that
// wants a QR code (the address card on ConnectScreen, a pairing code on
// InstancesScreen) just needs "hand me one decoded string back", not a spot
// in the navigation stack.
export default function QRScanner({ visible, onScanned, onClose, hint }: { visible: boolean; onScanned: (data: string) => void; onClose: () => void; hint: string }) {
  const { t } = useT();
  const { c, accent, accentContrast, radii } = useAppearance();
  const [permission, requestPermission] = useCameraPermissions();
  const [locked, setLocked] = useState(false);

  // The component stays mounted (it only renders null below) across opens
  // and closes, so `locked` from a PREVIOUS scan would otherwise still be
  // true the next time this reopens - every scan after the first silently
  // doing nothing.
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
            <TouchableOpacity
              style={[styles.button, { backgroundColor: accent, borderRadius: radii.control }]}
              onPress={requestPermission}
            >
              <Text style={[styles.buttonText, { color: accentContrast }]}>{t('qr.grantAccess')}</Text>
            </TouchableOpacity>
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
        <TouchableOpacity
          style={[styles.close, { backgroundColor: c.surface, borderRadius: radii.pill }]}
          onPress={onClose}
        >
          <Text style={[styles.closeText, { color: c.text }]}>{t('qr.cancel')}</Text>
        </TouchableOpacity>
      </View>
    </Modal>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
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
  button: { paddingVertical: 12, paddingHorizontal: 20 },
  buttonText: { fontSize: 15, fontWeight: '600' },
  close: {
    position: 'absolute',
    top: 56,
    right: 20,
    paddingVertical: 8,
    paddingHorizontal: 16,
  },
  closeText: { fontSize: TYPE.body },
});
