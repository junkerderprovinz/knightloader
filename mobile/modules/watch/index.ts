import { requireOptionalNativeModule, type NativeModule } from 'expo';

export type NoticeChannel = 'captcha' | 'finished' | 'failed';

export interface Notice {
  /** The same id replaces the notification posted under it. */
  id: number;
  channel: NoticeChannel;
  title: string;
  text: string;
  /** The longer text the expanded notification shows instead of `text`. */
  detail?: string;
  /** The instance's name, in the notification's header. */
  sub?: string;
  /** Updates the notification without a sound. */
  silent?: boolean;
  /** What a tap opens, for the saved connection `connection`. */
  open: 'captcha' | 'downloads';
  connection: string;
}

/** What a tapped notification asks the app to show. */
export interface OpenRequest {
  open: 'captcha' | 'downloads';
  /** Empty for the service's own notification, which belongs to no instance. */
  connection: string;
}

declare class KnightWatchModule extends NativeModule<{ onOpen: () => void }> {
  channels(captcha: string, finished: string, failed: string, watch: string): void;
  /** False when Android refuses, which it does for an app that is not in front. */
  start(title: string, text: string): boolean;
  stop(): void;
  running(): boolean;
  /** When the service runs the next pass, in milliseconds from the end of this one. */
  next(delayMs: number): void;
  notify(notice: Notice): void;
  cancel(id: number): void;
  /** Whether Android shows this app's notifications at all. */
  enabled(): boolean;
  takeOpen(): OpenRequest | null;
}

/** Null where the module does not exist, which is everywhere but Android. */
export const KnightWatch = requireOptionalNativeModule<KnightWatchModule>('KnightWatch');
