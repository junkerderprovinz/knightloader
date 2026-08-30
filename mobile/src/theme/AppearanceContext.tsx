import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useColorScheme } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  DEFAULT_ACCENT,
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
import { DARK, LIGHT, RADII, TYPE, inkFor, type Palette, type Radii } from './tokens';

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
  /** The accent used AS ink, on the page's own ground. Same colour in dark
   *  mode; darkened until it is readable in light mode. See tokens.inkFor. */
  accentInk: string;
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
  overridden: { accent: boolean; shape: boolean; theme: boolean; rainbow: boolean };
  setAccent: (hex: string | undefined) => void;
  setShape: (s: Shape | undefined) => void;
  setTheme: (t: 'light' | 'dark' | undefined) => void;
  /** Turn the rainbow on or off for THIS app; undefined follows the instance
   *  again. The palette and the seed are never local - see below. */
  setRainbow: (on: boolean | undefined) => void;
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
  rainbow?: boolean;
}

const STORE_KEY = 'glim-appearance-override';

/** The light theme's accent before GlimStone 1.5.0 split fill from ink. It is
 *  no longer produced anywhere, and it is not in the picker, so a stored copy
 *  can only have come from a snapshot of the old resolved look. See the load
 *  effect below for why that matters. */
const RETIRED_LIGHT_ACCENT = '#8E6A00';

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
          // RETIRED_LIGHT_ACCENT is dropped rather than honoured (jdp,
          // 2026-08-30: "in der app ist noch die dunkle gelbe farbe
          // standardmäßig eingestellt", after 1.6.1 had already stopped
          // producing it). Nobody ever picked #8E6A00 from the swatch row -
          // it was the light theme's own resolved accent back when ONE token
          // did both jobs, and "Aussehen übernehmen" off snapshots the
          // RESOLVED look into the override layer. So the retired default got
          // frozen into storage as if it were a choice, and every later
          // launch faithfully restored it. A stored value that the picker
          // cannot produce is not a preference, it is a leftover.
          accent: valid(p.accent) && p.accent.toUpperCase() !== RETIRED_LIGHT_ACCENT ? p.accent : undefined,
          shape: asShape(p.shape),
          theme: p.theme === 'light' || p.theme === 'dark' ? p.theme : undefined,
          rainbow: typeof p.rainbow === 'boolean' ? p.rainbow : undefined,
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

    // The accent is used as given, in both themes. It used to be swapped for
    // a fixed dark yellow in light mode, which painted every badge and switch
    // olive-brown (jdp: "Die gelbe akzentfarbe ist im hellen modus ganz
    // dunkel"); the darkening belongs to the INK, not to the colour - see
    // accentInk just below, and tokens.inkFor for why one token could never
    // do both jobs.
    const chosen = override.accent ?? (valid(instance?.accent) ? instance?.accent : undefined);
    const accent = chosen ?? DEFAULT_ACCENT;

    const shape = override.shape ?? asShape(instance?.shape) ?? 'round';

    // The rainbow: on or off is a local choice like every other axis on this
    // screen (jdp, 2026-08-30: "Regenbogenmodus hat kein toggle und kann
    // nicht aktiviert werden"), but the PALETTE and the SEED are not, and
    // that is the part of the old rule worth keeping - they belong to the
    // instance so that two clients of one server cannot disagree about the
    // colour of a download. Switching the mode on locally therefore asks the
    // instance's own settings for its colours, as if it had the mode on
    // itself; rainbowFromSettings only builds them in that case.
    const instRainbow = rainbowFromSettings(instance);
    const rainbow =
      override.rainbow === undefined
        ? instRainbow
        : override.rainbow
          ? rainbowFromSettings({ ...instance, rainbow: true })
          : { ...instRainbow, on: false };

    return {
      dark,
      c,
      accent,
      accentContrast: contrastOn(accent),
      accentInk: dark ? accent : inkFor(accent),
      accentSoft: softOn(accent),
      radii: RADII[shape] ?? RADII.round,
      type: TYPE,
      hueAt: (i: number) => rainbowColor(rainbow, i),
      rainbow,
      overridden: {
        accent: override.accent !== undefined,
        shape: override.shape !== undefined,
        theme: override.theme !== undefined,
        rainbow: override.rainbow !== undefined,
      },
      setAccent: (hex) => persist({ ...override, accent: valid(hex) ? hex : undefined }),
      setShape: (s) => persist({ ...override, shape: s }),
      setTheme: (t) => persist({ ...override, theme: t }),
      setRainbow: (on) => persist({ ...override, rainbow: on }),
      followInstance: () => persist({}),
      snapshotAsLocal: () => persist({ accent, shape, theme: dark ? 'dark' : 'light', rainbow: rainbow.on }),
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
