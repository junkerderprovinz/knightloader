import { requireNativeView } from 'expo';
import type { ViewProps } from 'react-native';

export type QrScannerViewProps = ViewProps & {
  /** The text of the first QR code the camera reads. */
  onCode: (event: { nativeEvent: { data: string } }) => void;
};

export const QrScannerView = requireNativeView<QrScannerViewProps>('QrScanner');
