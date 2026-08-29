import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useColorScheme } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  DEFAULT_ACCENT,
  RAINBOW_OFF,
  asShape,
  contrastOn,
  rainbowColor,
  rainbowFromSettings,
  softOn,
  valid,
  type InstanceAppearance,
  type RainbowState,
  type Shape,
} from './appearance';
import { DARK, LIGHT, LIGHT_DEFAULT_ACCENT, RADII, TYPE, type Palette, type Radii } from './tokens';

// Where the app's look comes from, and in which order.
//
// jdp chose "instance leads, app may override" (2026-08-25), and the reason it
// is not simply "app decides" is in GlimStone's own rule for the rainbow seed:
// it lives on the INSTANCE, not the client, because two clients of one server
// must not disagree about the colour of a download. The same argument covers
// the accent and the shape - open the web UI and the app side by side on one
// instance and they should be the same product.
//
// So three layers, first hit wins:
//
//   1. a local override, when somebody has set one here on purpose
//   2. whatever the ACTIVE instance reports
//   3. GlimStone's own defaults - Sunflower, round
//
// Light/dark is the exception and has only layers 1 and 3: an instance carries
// no theme setting, on the web either. There it follows the device through
// prefers-color-scheme, and it follows the device here for the same reason.
//
// Layer 3 is what the connect screen uses, because there is no instance yet at
// that point. That is not a gap: a fresh install of every app in the family
// opens in the same colour, before anyone has chosen anything.

/** What a component reads. Nothing here says which layer produced a value. */
export interface Appearance {
  dark: boolean;
  c: Palette;
  /** The activity colour. Never one of the four state colours - those are
   *  fixed per theme and never rainbow-coloured, because green has to mean
   *  "finished" everywhere. */
  accent: string;
  /** Ink to put on the accent, computed rather than configured. */
  accentContrast: string;
  /** The accent at low opacity, for a fill behind it. */
  accentSoft: string;
  radii: Radii;
  type: typeof TYPE;
  /** The colour for one list POSITION, or undefined when the mode is off and
   *  the single accent applies. */
  hueAt: (i: number) => string | undefined;
  rainbow: RainbowState;

  // --- the override layer, for the settings screen ---
  /** True when a local override is in force for that axis. */
  overridden: { accent: boolean; shape: boolean; theme: boolean };
  setAccent: (hex: string | undefined) => void;
  setShape: (s: Shape | undefined) => void;
  setTheme: (t: 'light' | 'dark' | undefined) => void;
  /** Drop every override and follow the instance again. */
  followInstance: () => void;
  /** The opposite direction: write the RESOLVED look into the override layer,
   *  so switching "follow the instance" off changes nothing on screen - the
   *  values simply become yours to edit. The counterpart of the browser
   *  extension's stash-before-adopt. */
  snapshotAsLocal: () => void;
  /** Called by whatever knows the active connection. */
  setInstanceAppearance: (a: InstanceAppearance | undefined) => void;
}

interface Override {
  accent?: string;
  shape?: Shape;
  theme?: 'light' | 'dark';
}

const STORE_KEY = 'glim-appearance-override';

const AppearanceCtx = createContext<Appearance | null>(null);

export function AppearanceProvider({ children }: { children: ReactNode }) {
  const system = useColorScheme();
  const [override, setOverride] = useState<Override>({});
  const [instance, setInstance] = useState<InstanceAppearance | undefined>(undefined);

  // Read once at start. Not awaited before the first paint: the defaults are
  // GlimStone's own, so the worst case is one frame in Sunflower before a
  // chosen colour arrives - which is a far smaller cost than a blank screen.
  useEffect(() => {
    AsyncStorage.getItem(STORE_KEY)
      .then((raw) => {
        if (!raw) return;
        const p = JSON.parse(raw) as Override;
        setOverride({
          accent: valid(p.accent) ? p.accent : undefined,
          shape: asShape(p.shape),
          theme: p.theme === 'light' || p.theme === 'dark' ? p.theme : undefined,
        });
      })
      .catch(() => {
        /* an unreadable override is no override, never a crash */
      });
  }, []);

  const persist = useCallback((next: Override) => {
    setOverride(next);
    // Fire and forget: the value is already in state and on screen, and a
    // failed write costs the choice at next launch, not now.
    void AsyncStorage.setItem(STORE_KEY, JSON.stringify(next)).catch(() => {});
  }, []);

  const value = useMemo<Appearance>(() => {
    // Theme is NOT one of the things an instance carries - it never has been,
    // on the web either: light/dark follows the device through
    // prefers-color-scheme, and an app that opens dark on a light system has
    // made a decision nobody asked for. So this axis has two layers, not
    // three: a local choice, or the system.
    const dark = override.theme ? override.theme === 'dark' : system !== 'light';
    const c = dark ? DARK : LIGHT;

    // The chosen accent is used as given. Only the DEFAULT is darkened for the
    // light theme - second-guessing a colour somebody picked on purpose is how
    // a picker stops meaning anything.
    const chosen = override.accent ?? (valid(instance?.accent) ? instance?.accent : undefined);
    const accent = chosen ?? (dark ? DEFAULT_ACCENT : LIGHT_DEFAULT_ACCENT);

    const shape = override.shape ?? asShape(instance?.shape) ?? 'round';
    // The rainbow is never overridden locally: its seed belongs to the
    // instance by design, and a client that picked its own would be the exact
    // disagreement that rule exists to prevent.
    const rainbow = instance ? rainbowFromSettings(instance) : RAINBOW_OFF;

    return {
      dark,
      c,
      accent,
      accentContrast: contrastOn(accent),
      accentSoft: softOn(accent),
      radii: RADII[shape] ?? RADII.round,
      type: TYPE,
      hueAt: (i: number) => rainbowColor(rainbow, i),
      rainbow,
      overridden: {
        accent: override.accent !== undefined,
        shape: override.shape !== undefined,
        theme: override.theme !== undefined,
      },
      setAccent: (hex) => persist({ ...override, accent: valid(hex) ? hex : undefined }),
      setShape: (s) => persist({ ...override, shape: s }),
      setTheme: (t) => persist({ ...override, theme: t }),
      followInstance: () => persist({}),
      snapshotAsLocal: () => persist({ accent, shape, theme: dark ? 'dark' : 'light' }),
      setInstanceAppearance: setInstance,
    };
  }, [override, instance, system, persist]);

  return <AppearanceCtx.Provider value={value}>{children}</AppearanceCtx.Provider>;
}

export function useAppearance(): Appearance {
  const v = useContext(AppearanceCtx);
  if (!v) throw new Error('useAppearance must be used inside AppearanceProvider');
  return v;
}
