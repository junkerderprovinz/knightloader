import { useEffect, useRef, useState } from 'react';
import { Modal, StyleSheet, View } from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { qrFromJpeg } from './qrDecode';
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
export default function QRScanner({ visible, onScanned, onClose, hint }: { visible: boolean; onScanned: (data: string) => void; onClose: () => void; hint: string }) {
  const { t } = useT();
  const { c, accent, corners } = useAppearance();
  const { motion } = useMotion();
  const [permission, requestPermission] = useCameraPermissions();
  const [locked, setLocked] = useState(false);
  const [ready, setReady] = useState(false);
  const [size, setSize] = useState<string>();
  const camera = useRef<CameraView>(null);
  const report = useRef(onScanned);
  report.current = onScanned;
  // Set by Cancel, so a frame already being decoded is thrown away and the
  // loop takes no further one.
  const closing = useRef(false);

  // The component stays mounted across opens and closes, rendering null, so
  // `locked` from an earlier scan would still be true the next time it opens
  // and every scan after the first would do nothing.
  useEffect(() => {
    if (visible) {
      closing.current = false;
      setLocked(false);
    } else setReady(false);
  }, [visible]);

  // The camera's own barcode scanner is Google's ML Kit, which is not free
  // software and keeps the app out of F-Droid, so the frame is photographed
  // small, several times a second, and decoded here.
  useEffect(() => {
    if (!visible || !ready || locked) return;
    let stopped = false;
    (async () => {
      while (!stopped && !closing.current) {
        try {
          const shot = await camera.current?.takePictureAsync({ base64: true, quality: 0.5, skipProcessing: true, shutterSound: false });
          const data = shot?.base64 ? qrFromJpeg(shot.base64) : null;
          if (data && !stopped && !closing.current) {
            setLocked(true);
            report.current(data);
            return;
          }
        } catch {
          // A frame the camera could not take or decode; the next one tries.
        }
        await new Promise((resolve) => setTimeout(resolve, 150));
      }
    })();
    return () => {
      stopped = true;
    };
  }, [visible, ready, locked]);

  // A full-resolution photo takes seconds to decode in JavaScript, and every
  // decode holds up the taps on this window. The smallest size with 480 lines
  // still resolves a code in the frame at five pixels a module.
  const onCameraReady = async () => {
    try {
      const sizes = (await camera.current?.getAvailablePictureSizesAsync()) ?? [];
      const lines = (s: string) => Math.min(...s.split('x').map(Number));
      const usable = sizes.filter((s) => lines(s) >= 480).sort((a, b) => lines(a) - lines(b));
      if (usable[0]) setSize(usable[0]);
    } catch {
      // Without the list the camera keeps its default size, only slower.
    }
    setReady(true);
  };

  const cancel = () => {
    closing.current = true;
    onClose();
  };

  if (!visible) return null;

  return (
    <Modal visible={visible} animationType={motion === 'off' ? 'none' : 'slide'} onRequestClose={cancel}>
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
              ref={camera}
              style={StyleSheet.absoluteFill}
              facing="back"
              animateShutter={false}
              pictureSize={size}
              onCameraReady={onCameraReady}
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
          <GlimButton tone="quiet" label={t('qr.cancel')} icon={(ink) => <Cross color={ink} />} onPress={cancel} />
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
