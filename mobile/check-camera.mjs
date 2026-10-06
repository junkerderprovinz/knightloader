// The QR scanner follows the camera permission back from Android's settings.
//
// After a second refusal Android stops asking, and only its settings page can
// grant the camera. The tap that meets that refusal turns the button into the
// way to those settings, so it is not a tap that seems to do nothing, and the
// scanner opens as soon as the app is back with the camera allowed, rather
// than after it is closed and opened again.
//
// The check renders src/components/QRScanner.tsx on stand-ins for React, React
// Native and the scanner module.
//
// Run by hand and by CI, from mobile/: `node check-camera.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const phone = { allowed: false, answer: 'never_ask_again', asked: 0, settings: 0, listeners: new Set() };
const { default: QRScanner } = compile(join(here, 'src', 'components', 'QRScanner.tsx'), {
  'react-native': {
    AppState: {
      addEventListener(_, f) {
        phone.listeners.add(f);
        return { remove: () => phone.listeners.delete(f) };
      },
    },
    Linking: { openSettings: () => phone.settings++ },
    Modal: 'Modal',
    PermissionsAndroid: {
      PERMISSIONS: { CAMERA: 'android.permission.CAMERA' },
      RESULTS: { GRANTED: 'granted', DENIED: 'denied', NEVER_ASK_AGAIN: 'never_ask_again' },
      check: async () => phone.allowed,
      request: async () => {
        phone.asked++;
        return phone.answer;
      },
    },
    StyleSheet: { create: (s) => s, absoluteFill: {} },
    View: 'View',
  },
  '../../modules/qr-scanner': { QrScannerView: 'QrScannerView' },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {}, accent: '', corners: {} }) },
  '../theme/MotionContext': { useMotion: () => ({ motion: 'on' }) },
  '../theme/tokens': { TYPE: {} },
  '../i18n/I18nContext': { useT: () => ({ t: (key) => key }) },
  './glim': { GlimButton: 'GlimButton' },
  './IconBadge': { Cross: 'Cross' },
  './Text': { Text: 'Text' },
});

const scanner = mount(() => QRScanner({ visible: true, onScanned() {}, onClose() {}, hint: '' }));
const button = (label) => scanner.shown().elements.find((e) => e.type === 'GlimButton' && e.props.label === label)?.props;
const scanning = () => scanner.shown().elements.some((e) => e.type === 'QrScannerView');
const comeBack = async () => {
  for (const f of phone.listeners) f('active');
  await scanner.settle();
};

await scanner.settle();
expect('without the camera the scanner asks for it', [scanning(), !!button('qr.grantAccess')], [false, true]);
button('qr.grantAccess')?.onPress();
await scanner.settle();
expect('a refusal Android will not ask again turns the button into the way to settings', !!button('qr.openSettings'), true);
expect('the tap that meets that refusal does not open settings yet', phone.settings, 0);
button('qr.openSettings')?.onPress();
await scanner.settle();
expect('the next tap opens settings', [phone.settings, phone.asked], [1, 1]);
await comeBack();
expect('back from settings without the camera, the button still leads there', !!button('qr.openSettings'), true);
phone.allowed = true;
await comeBack();
expect('back from settings with the camera allowed, the scanner opens', scanning(), true);
scanner.unmount();
expect('a closed scanner stops listening for the app coming back', phone.listeners.size, 0);

if (problems.length) {
  console.error(`check-camera: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: the scanner leads to settings after a final refusal and opens once the camera is allowed there');
