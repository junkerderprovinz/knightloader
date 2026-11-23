import { requireOptionalNativeModule, type NativeModule } from 'expo';

declare class KnightInstallSourceModule extends NativeModule {
  /** The package that installed this app, or null for a file installed by hand. */
  installer(): string | null;
}

/** Null where the module does not exist, which is everywhere but Android. */
export const KnightInstallSource = requireOptionalNativeModule<KnightInstallSourceModule>('KnightInstallSource');
