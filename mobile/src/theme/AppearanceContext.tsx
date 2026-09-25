import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useColorScheme } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  ACCENTS,
  DEFAULT_ACCENT,
  DEFAULT_SHAPE,
  DISCO_FRAME_MS,
  DISCO_TICK_MS,
  asShape,
  contrastOn,
  rainbowColor,
  rainbowFromSettings,
  softOn,
  valid,
  walkedColour,
  type InstanceAppearance,
  type RainbowState,
  type Shape,
} from './appearance';
import { buildLoop } from './discoLoop';
import { useMotion } from './MotionContext';
import { DARK, LIGHT, TYPE, cornersFor, inkFor, type Corners, type Palette } from './tokens';

// Where the app's look comes from, and in which order.
//
// The instance leads and the app may override. GlimStone keeps the rainbow seed
// on the instance rather than the client, because two clients of one server
// must not disagree about the colour of a download, and the same argument
// covers the accent and the shape: the web UI and the app open side by side on
// one instance and should be the same product.
//
// Three layers, first hit wins:
//
//   1. a local override, where somebody has set one here
//   2. whatever the active instance reports
//   3. GlimStone's defaults, Sunflower and soft
//
// Light and dark have only layers 1 and 3: an instance carries no theme
// setting, on the web either, where it follows the device through
// prefers-color-scheme.
//
// Layer 3 is what the connect screen uses, since there is no instance yet, so a
// fresh install of every app in the family opens in the same colour.

/** What a component reads. Nothing here says which layer produced a value. */
export interface Appearance {
  dark: boolean;
  c: Palette;
  /** The activity colour, never one of the four state colours: those are fixed
   *  per theme and stay out of the rainbow, because green has to mean
   *  "finished" everywhere. */
  accent: string;
  /** Ink to put on the accent, computed rather than configured. */
  accentContrast: string;
  /** The accent used as ink, on the page's own ground. The same colour in dark
   *  mode, darkened until readable in light mode. See tokens.inkFor. */
  accentInk: string;
  /** The accent at low opacity, for a fill behind it. */
  accentSoft: string;
  /** The shape in force, for the picker that marks it. */
  shape: Shape;
  /** Each radius as a style to spread, since the leaf needs more than one
   *  number per element. */
  corners: Corners;
  type: typeof TYPE;
  /** The colour for one list position, or undefined when the mode is off and
   *  the single accent applies. While disco walks, a new function every frame
   *  of the walk, so a list that keeps it in its extraData redraws with the
   *  palette. */
  hueAt: (i: number) => string | undefined;
  /** The rainbow as set, never the step disco has walked it to. */
  rainbow: RainbowState;
  /** Whether disco is switched on. It walks only while the rainbow is on. */
  disco: boolean;
  setDisco: (on: boolean) => void;

  /** True when a local override is in force for that axis. */
  overridden: { accent: boolean; shape: boolean; theme: boolean; rainbow: boolean };
  setAccent: (hex: string | undefined) => void;
  /** What each preset slot has been mixed to, empty for the ones nobody
   *  touched. The accent row reads this so an edited swatch keeps its colour
   *  when another one is chosen. */
  accentCustoms: Record<string, string>;
  /** Which preset slot is chosen, or undefined while nobody has picked one and
   *  the nearest-preset arithmetic still answers. */
  accentSlotChosen: number | undefined;
  /** Choose slot `i` and wear the colour it shows. */
  chooseAccentSlot: (i: number, hex: string) => void;
  /** Remember `hex` against preset slot `i`, and wear it. Passing undefined
   *  gives that one slot its preset back. */
  setAccentCustom: (i: number, hex: string | undefined) => void;
  /** Every mixed colour forgotten, and the accent with them: the row's reset. */
  clearAccentCustoms: () => void;
  setShape: (s: Shape | undefined) => void;
  setTheme: (t: 'light' | 'dark' | undefined) => void;
  /** Turn the rainbow on or off for this app; undefined follows the instance
   *  again. The palette and the seed are never local, see below. */
  setRainbow: (on: boolean | undefined) => void;
  /** Drop every override and follow the instance again. */
  followInstance: () => void;
  /** The opposite direction: write the resolved look into the override layer,
   *  so switching "follow the instance" off changes nothing on screen and the
   *  values become yours to edit. The counterpart of the browser extension's
   *  stash-before-adopt. */
  snapshotAsLocal: () => void;
  /** Called by whatever knows the active connection. */
  setInstanceAppearance: (a: InstanceAppearance | undefined) => void;
}

interface Override {
  accent?: string;
  shape?: Shape;
  theme?: 'light' | 'dark';
  rainbow?: boolean;
  /**
   * Which slot is chosen, as its own fact rather than derived from the accent
   * by nearest preset. Mix two swatches to the same red and the arithmetic
   * marks both, so the row shows two chosen swatches and a press on either
   * opens the picker instead of choosing.
   */
  slot?: number;
  /**
   * A colour somebody mixed, remembered against the preset slot they mixed it
   * in. Keyed by slot index as a string, because that is what JSON gives back.
   *
   * Without it the row holds one mixed colour, the accent itself drawn over
   * whichever preset it sits nearest, so choosing another swatch throws the
   * mixed one away.
   */
  customs?: Record<string, string>;
}

const STORE_KEY = 'glim-appearance-override';

/** Disco's switch. Its own key rather than a field of the override: it is
 *  this phone's alone, like the motion level, and following the instance must
 *  not clear it. */
const DISCO_KEY = 'glim-disco';

/**
 * Where a local look waits while the instance's own is being worn.
 *
 * A switch has to be reversible, and reversible means the thing it put away
 * comes back rather than something that resembles it. Without the shelf, going
 * on discards the override and coming back off snapshots whatever the instance
 * looks like, so a round trip replaces the user's colour and corner shape with
 * the server's.
 *
 * Persisted rather than kept in memory, because the app being closed while
 * following is the ordinary case.
 */
const SHELF_KEY = 'glim-appearance-shelf';

/** The light theme's accent from before GlimStone 1.5.0 split fill from ink.
 *  Nothing produces it and the picker does not offer it, so a stored copy can
 *  only have come from a snapshot of the old resolved look. */
const RETIRED_LIGHT_ACCENT = '#8E6A00';

/**
 * What a stored override is allowed to say, read on the way in, so nothing
 * downstream has to defend itself against a value the picker cannot produce.
 *
 * RETIRED_LIGHT_ACCENT is dropped rather than honoured. Nobody picked #8E6A00
 * from the swatch row: it was the light theme's resolved accent back when one
 * token did both jobs, and turning "follow the instance" off snapshots the
 * resolved look into the override layer, so it could be frozen into storage as
 * if it were a choice. A stored value the picker cannot produce is a leftover,
 * not a preference.
 */
function sanitise(p: Override): Override {
  return {
    accent: valid(p.accent) && p.accent.toUpperCase() !== RETIRED_LIGHT_ACCENT ? p.accent : undefined,
    shape: asShape(p.shape),
    theme: p.theme === 'light' || p.theme === 'dark' ? p.theme : undefined,
    rainbow: typeof p.rainbow === 'boolean' ? p.rainbow : undefined,
    slot: Number.isInteger(p.slot) && p.slot! >= 0 && p.slot! < ACCENTS.length ? p.slot : undefined,
    customs: sanitiseCustoms(p.customs),
  };
}

/** Only real hex against a real slot number survives a read, so the row never
 *  has to defend itself against a value the picker could not have produced. */
function sanitiseCustoms(raw: unknown): Record<string, string> | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
    const i = Number(k);
    if (!Number.isInteger(i) || i < 0 || i >= ACCENTS.length) continue;
    if (typeof v === 'string' && valid(v)) out[String(i)] = v;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

/** Whether an override says anything at all. An empty one is already "follow
 *  the instance", so there is nothing to put away when the switch goes on. */
function isEmpty(o: Override): boolean {
  return (
    o.accent === undefined &&
    o.shape === undefined &&
    o.theme === undefined &&
    o.rainbow === undefined &&
    o.slot === undefined &&
    o.customs === undefined
  );
}

const AppearanceCtx = createContext<Appearance | null>(null);

export function AppearanceProvider({ children }: { children: ReactNode }) {
  const system = useColorScheme();
  const [override, setOverride] = useState<Override>({});
  const [shelf, setShelf] = useState<Override | null>(null);
  const [instance, setInstance] = useState<InstanceAppearance | undefined>(undefined);
  const [disco, setDiscoState] = useState(false);
  /** How much of a full turn disco has walked, from 0 up to 1, and only ever
   *  a whole colour's worth while it steps. Never stored and never written
   *  into the rainbow state, so stopping hands back the palette as it was set. */
  const [walk, setWalk] = useState(0);
  const travelled = useRef(0);
  // The glide is motion, so the level and the system setting decide it: at
  // "off", or with reduced motion, disco steps one colour at a time.
  const { chosen: motionChosen, reduced } = useMotion();
  const steps = motionChosen === 'off' || reduced;

  // Read once at start. Not awaited before the first paint: the defaults are
  // GlimStone's own, so the worst case is one frame in Sunflower before a
  // chosen colour arrives, which costs less than a blank screen.
  useEffect(() => {
    AsyncStorage.getItem(STORE_KEY)
      .then((raw) => {
        if (!raw) return;
        setOverride(sanitise(JSON.parse(raw) as Override));
      })
      .catch(() => {
        /* an unreadable override is no override, never a crash */
      });
    // The shelf reads through the same sanitiser as the live override, being
    // the same thing one switch-flip earlier.
    AsyncStorage.getItem(SHELF_KEY)
      .then((raw) => raw && setShelf(sanitise(JSON.parse(raw) as Override)))
      .catch(() => {
        /* an unreadable shelf is an empty shelf */
      });
    AsyncStorage.getItem(DISCO_KEY)
      .then((raw) => setDiscoState(raw === 'on'))
      .catch(() => {
        /* an unreadable switch is off */
      });
  }, []);

  const setDisco = useCallback((on: boolean) => {
    setDiscoState(on);
    void AsyncStorage.setItem(DISCO_KEY, on ? 'on' : 'off').catch(() => {});
  }, []);

  /**
   * The live override, mirrored into a ref so a writer does not have to wait
   * for a render to see what the last writer did. A setter that spread the
   * `override` its closure was built with would lose the first of two writes in
   * the same tick, which is what mixing one swatch and then the next does.
   */
  const liveOverride = useRef(override);
  liveOverride.current = override;

  /**
   * Persist an override. Pass a function to derive it from what is stored right
   * now, which is what anything spreading the previous value has to do.
   */
  const persist = useCallback((update: Override | ((prev: Override) => Override)) => {
    const next = typeof update === 'function' ? update(liveOverride.current) : update;
    // Written to the ref first, so a second call in the same tick builds on this
    // one rather than on the render that is yet to happen.
    liveOverride.current = next;
    setOverride(next);
    // Fire and forget: the value is already in state and on screen, and a
    // failed write costs the choice at next launch, not now.
    void AsyncStorage.setItem(STORE_KEY, JSON.stringify(next)).catch(() => {});
  }, []);

  const shelve = useCallback((next: Override | null) => {
    setShelf(next);
    if (next === null) void AsyncStorage.removeItem(SHELF_KEY).catch(() => {});
    else void AsyncStorage.setItem(SHELF_KEY, JSON.stringify(next)).catch(() => {});
  }, []);

  // On or off is a local choice like every other axis on this screen, while
  // the palette and the seed belong to the instance, so two clients of one
  // server cannot disagree about the colour of a download. Switching the mode
  // on locally therefore asks the instance's settings for its colours, as if
  // it had the mode on itself.
  const rainbow = useMemo<RainbowState>(() => {
    const instRainbow = rainbowFromSettings(instance);
    if (override.rainbow === undefined) return instRainbow;
    return override.rainbow ? rainbowFromSettings({ ...instance, rainbow: true }) : { ...instRainbow, on: false };
  }, [override.rainbow, instance]);

  // The walk runs here, above every screen, because a hue reaches a view as a
  // value computed at render: only a provider every screen reads can make them
  // all move together. With the rainbow off nothing hued is on screen, so it
  // waits, and it starts by itself when the rainbow comes back. The distance
  // comes from the clock rather than from counting ticks, so a late timer
  // never slows the walk; while stepping, only a whole colour reaches state,
  // and the ticks in between render nothing.
  const walking = disco && rainbow.on;
  const loop = useMemo(() => buildLoop(rainbow.palette), [rainbow.palette]);
  useEffect(() => {
    if (!walking) {
      travelled.current = 0;
      setWalk(0);
      return;
    }
    const n = loop.palette.length;
    const turnMs = DISCO_TICK_MS * n;
    let last = Date.now();
    const timer = setInterval(() => {
      const now = Date.now();
      travelled.current = (travelled.current + (now - last) / turnMs) % 1;
      last = now;
      setWalk(steps ? Math.floor(travelled.current * n) / n : travelled.current);
    }, DISCO_FRAME_MS);
    return () => clearInterval(timer);
  }, [walking, steps, loop]);

  const value = useMemo<Appearance>(() => {
    // No instance carries a theme, on the web either: light and dark follow the
    // device through prefers-color-scheme, and an app that opens dark on a
    // light system has made a decision nobody asked for. So this axis has two
    // layers: a local choice, or the system.
    const dark = override.theme ? override.theme === 'dark' : system !== 'light';
    const c = dark ? DARK : LIGHT;

    // The accent is used as given, in both themes. Darkening belongs to the
    // ink rather than to the colour; see accentInk below and tokens.inkFor.
    const chosen = override.accent ?? (valid(instance?.accent) ? instance?.accent : undefined);
    const accent = chosen ?? DEFAULT_ACCENT;

    const shape = override.shape ?? asShape(instance?.shape) ?? DEFAULT_SHAPE;

    // What disco draws: each position walked along the loop from the colour it
    // has at rest, rotation included, so switching it on moves nothing until
    // the walk does.
    const start = rainbow.rotate ? rainbow.seed : 0;
    const hueAt = walking
      ? (i: number) => walkedColour(loop, start, walk, i, steps)
      : (i: number) => rainbowColor(rainbow, i);

    return {
      dark,
      c,
      accent,
      accentContrast: contrastOn(accent),
      accentInk: dark ? accent : inkFor(accent),
      accentSoft: softOn(accent),
      shape,
      corners: cornersFor(shape),
      type: TYPE,
      hueAt,
      rainbow,
      disco,
      setDisco,
      overridden: {
        accent: override.accent !== undefined,
        shape: override.shape !== undefined,
        theme: override.theme !== undefined,
        rainbow: override.rainbow !== undefined,
      },
      setAccent: (hex) => persist((p) => ({ ...p, accent: valid(hex) ? hex : undefined })),
      accentCustoms: override.customs ?? {},
      accentSlotChosen: override.slot,
      chooseAccentSlot: (i, hex) =>
        persist((p) => ({ ...p, slot: i, accent: valid(hex) ? hex : p.accent })),
      setAccentCustom: (i, hex) =>
        persist((p) => {
          const next = { ...(p.customs ?? {}) };
          if (valid(hex)) next[String(i)] = hex;
          else delete next[String(i)];
          return {
            ...p,
            slot: i,
            accent: valid(hex) ? hex : p.accent,
            customs: Object.keys(next).length > 0 ? next : undefined,
          };
        }),
      clearAccentCustoms: () =>
        persist((p) => ({ ...p, accent: undefined, slot: undefined, customs: undefined })),
      setShape: (s) => persist({ ...override, shape: s }),
      setTheme: (t) => persist({ ...override, theme: t }),
      setRainbow: (on) => persist({ ...override, rainbow: on }),
      // The two ends of one switch: whatever going on puts away, going off
      // brings back.
      followInstance: () => {
        // An empty override is already following, so there is nothing to put
        // away, and shelving it would overwrite a real look waiting there from
        // an earlier flip.
        if (!isEmpty(override)) shelve(override);
        persist({});
      },
      snapshotAsLocal: () => {
        if (shelf && !isEmpty(shelf)) {
          persist(shelf);
          // Taken off the shelf rather than copied off it, or the next round
          // trip would restore this same look instead of whatever the user has
          // chosen since.
          shelve(null);
          return;
        }
        // Nothing was ever put away, so the resolved look is snapshotted: it
        // hands over what is on screen, so turning the switch off changes
        // nothing visible and leaves something to edit from.
        persist({ accent, shape, theme: dark ? 'dark' : 'light', rainbow: rainbow.on });
      },
      setInstanceAppearance: setInstance,
    };
  }, [override, shelf, instance, system, persist, shelve, rainbow, walking, walk, steps, loop, disco, setDisco]);

  return <AppearanceCtx.Provider value={value}>{children}</AppearanceCtx.Provider>;
}

export function useAppearance(): Appearance {
  const v = useContext(AppearanceCtx);
  if (!v) throw new Error('useAppearance must be used inside AppearanceProvider');
  return v;
}
