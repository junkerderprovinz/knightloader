import { requireOptionalNativeModule, type NativeModule } from 'expo';

/** Material 3's primary for the light and the dark look, as ARGB ints. */
export interface AccentTones {
  light: number;
  dark: number;
}

declare class KnightAccentModule extends NativeModule<{ onChange: () => void }> {
  /** Null below Android 12, which has no palette to read. */
  tones(): AccentTones | null;
}

/** Null where the module does not exist, which is everywhere but Android. */
export const KnightAccent = requireOptionalNativeModule<KnightAccentModule>('KnightAccent');
