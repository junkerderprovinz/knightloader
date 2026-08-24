import { useEffect, useState } from 'react';
import { Modal, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { colors } from '../theme';
import { useT } from '../i18n/I18nContext';

// A full-screen modal scanner, not a screen of its own: every place that
// wants a QR code (the address card on ConnectScreen, a pairing code on
// InstancesScreen) just needs "hand me one decoded string back", not a spot
// in the navigation stack.
export default function QRScanner({ visible, onScanned, onClose, hint }: { visible: boolean; onScanned: (data: string) => void; onClose: () => void; hint: string }) {
  const { t } = useT();
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
      <View style={styles.container}>
        {!permission ? (
          <View style={styles.center} />
        ) : !permission.granted ? (
          <View style={styles.center}>
            <Text style={styles.hint}>{t('qr.cameraPermissionHint')}</Text>
            <TouchableOpacity style={styles.button} onPress={requestPermission}>
              <Text style={styles.buttonText}>{t('qr.grantAccess')}</Text>
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
              <View style={styles.frame} />
              <Text style={styles.hint}>{hint}</Text>
            </View>
          </>
        )}
        <TouchableOpacity style={styles.close} onPress={onClose}>
          <Text style={styles.closeText}>{t('qr.cancel')}</Text>
        </TouchableOpacity>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 16 },
  overlay: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 24 },
  frame: {
    width: 240,
    height: 240,
    borderRadius: 16,
    borderWidth: 2,
    borderColor: colors.accent,
  },
  hint: { color: colors.text, fontSize: 14, textAlign: 'center', paddingHorizontal: 32 },
  button: { backgroundColor: colors.accent, borderRadius: 8, paddingVertical: 12, paddingHorizontal: 20 },
  buttonText: { color: colors.text, fontSize: 15, fontWeight: '600' },
  close: {
    position: 'absolute',
    top: 56,
    right: 20,
    backgroundColor: colors.surface,
    borderRadius: 20,
    paddingVertical: 8,
    paddingHorizontal: 16,
  },
  closeText: { color: colors.text, fontSize: 14 },
});
