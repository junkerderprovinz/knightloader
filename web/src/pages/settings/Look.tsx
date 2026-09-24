import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import { Button, Card, ErrorCard, InfoBubble, Modal, SectionTitle, Toggle, ToggleRow, useTooltip } from '../../components/ui';
import { About } from './Help';
import { Tabs } from '../../components/Tabs';
import { openColorPickerPopover } from '../../lib/colorPicker';
import { LanguagePicker } from '../../components/LanguagePicker';
import {
  type DeploymentInfo,
  fetchDeploymentInfo,
  fetchUpdateCheck,
  installUpdate,
  requestQuit,
  requestRestart,
  type UpdateCheck as UpdateCheckT,
} from '../../lib/api';
import { IconClose, IconMoon, IconRetry, IconSignOut, IconSun } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { MUTABLE_DIALOGS, useDialogMute } from '../../lib/dialogmute';
import { getTheme, onThemeChange, setTheme } from '../../lib/theme';
import { asNavLabelMode, setNavLabels, useNavLabels } from '../../lib/navLabels';
import { useT } from '../../lib/i18n';
import { useResource } from '../../lib/useResource';
import {
  ACCENTS,
  DEFAULT_ACCENT,
  MOTION_LEVELS,
  RAINBOW,
  SHAPES,
  type Motion,
  type Shape,
  applyAccent,
  applyDisco,
  applyMotion,
  applyRainbow,
  applyShape,
  cacheAppearance,
  cacheDisco,
  cacheMotionIntensity,
  discoTap,
  hueVars,
  rainbowAt,
  rainbowFromSettings,
  readCachedDisco,
  readCachedMotionIntensity,
  stormTap,
} from '../../lib/appearance';
import { useRainbow } from '../../lib/useRainbow';
import { useDraft } from './context';
import { same } from './paths';
import { NotificationsCard } from './look/Notifications';
import { SettingsTransfer } from './look/SettingsTransfer';

/**
 * accentSlot returns which of the eight preset positions a colour belongs to:
 * its own when it is a preset, otherwise the nearest by squared RGB distance.
 * It matches the extension's accentSlot (src/appearance.js), so a colour lands
 * in the same slot in both.
 */
function accentSlot(hex: string): number {
  const p = (h: string) => {
    const n = parseInt(h.slice(1), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  };
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return 0;
  const [r, g, b] = p(hex);
  let best = 0;
  let bestD = Infinity;
  ACCENTS.forEach((a, i) => {
    const [pr, pg, pb] = p(a.hex);
    const d = (r - pr) ** 2 + (g - pg) ** 2 + (b - pb) ** 2;
    if (d < bestD) {
      bestD = d;
      best = i;
    }
  });
  return best;
}

/**
 * The accent row's own memory: which circle was pressed and what each circle
 * was mixed to. The settings document carries only the accent in force, so
 * this stays in localStorage like the app's local override layer; another
 * browser adopts the accent into its nearest circle.
 */
const SLOTS_KEY = 'kl-accent-slots';

/** The appearance fields this page saves on every change. */
const LOOK_KEYS = ['shape', 'accent', 'rainbow', 'rainbowReactive', 'rainbowRotate', 'rainbowSeed', 'rainbowPalette'] as const;

interface SlotMemory {
  /**
   * The chosen slot, stored rather than derived from the colour, since two
   * circles mixed to the same colour would otherwise both be marked. Undefined
   * until a circle is pressed in this browser.
   */
  slot?: number;
  /** Mixed colours by slot index, as strings because that is what JSON returns. */
  customs: Record<string, string>;
}

/**
 * readSlotMemory keeps only six-digit hex values against real slot numbers,
 * since other code on this origin may use the same key.
 */
function readSlotMemory(): SlotMemory {
  try {
    const raw = localStorage.getItem(SLOTS_KEY);
    if (!raw) return { customs: {} };
    const parsed = JSON.parse(raw) as { slot?: unknown; customs?: unknown };
    const customs: Record<string, string> = {};
    if (parsed.customs && typeof parsed.customs === 'object') {
      for (const [k, v] of Object.entries(parsed.customs as Record<string, unknown>)) {
        const i = Number(k);
        if (!Number.isInteger(i) || i < 0 || i >= ACCENTS.length) continue;
        if (typeof v === 'string' && /^#[0-9a-fA-F]{6}$/.test(v)) customs[String(i)] = v;
      }
    }
    const slot = parsed.slot;
    const chosen =
      typeof slot === 'number' && Number.isInteger(slot) && slot >= 0 && slot < ACCENTS.length ? slot : undefined;
    return { slot: chosen, customs };
  } catch {
    // An unreadable memory falls back to the presets, like a fresh browser.
    return { customs: {} };
  }
}

function writeSlotMemory(m: SlotMemory): void {
  try {
    localStorage.setItem(SLOTS_KEY, JSON.stringify(m));
  } catch {
    // Storage may be unavailable; the colour is applied and saved regardless.
  }
}

/**
 * RingSwatch is a round colour button: a click selects it, and a click on the
 * selected one opens the picker. The selected state is a shadow rather than a
 * ring, matching the extension's .glim-swatch.
 */
function RingSwatch({
  color,
  label,
  selected,
  dim,
  onPick,
  onEdit,
}: {
  color: string;
  label: string;
  selected: boolean;
  /** Dims the circle only, so the caption and its (i) keep full strength. */
  dim?: boolean;
  onPick: () => void;
  onEdit?: (el: HTMLElement) => void;
}) {
  // The house tooltip rather than a native title.
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={label}
        aria-pressed={selected}
        onClick={(e) => (selected && onEdit ? onEdit(e.currentTarget) : onPick())}
        // `--btn-h`, the app's one square size, shared with the palette row and
        // the reset badges.
        className={`h-[var(--btn-h)] w-[var(--btn-h)] shrink-0 cursor-pointer rounded-[var(--radius-pill)] transition-transform hover:scale-110 ${
          selected ? 'shadow-[0_0_0_2px_var(--carbon-surface),0_0_0_4px_var(--carbon-text)]' : ''
        } ${dim ? 'opacity-45' : ''}`}
        style={{ backgroundColor: color }}
        {...tipHoverProps}
      />
      {tip.node}
    </>
  );
}

/**
 * PaletteSwatch is one rainbow palette position, opening the picker on its
 * colour. A component, because the tooltip is a hook.
 */
function PaletteSwatch({ color, name, onEdit }: { color: string; name: string; onEdit: (el: HTMLElement) => void }) {
  // `name`, not `label`: an accessible name, not a caption the settings search
  // jumps to.
  const tip = useTooltip<HTMLButtonElement>(name);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={name}
        // The app's one square size, as in RingSwatch.
        className="relative h-[var(--btn-h)] w-[var(--btn-h)] shrink-0 cursor-pointer overflow-hidden rounded-[var(--radius-pill)]"
        style={{ backgroundColor: color }}
        onClick={(e) => onEdit(e.currentTarget)}
        {...tipHoverProps}
      />
      {tip.node}
    </>
  );
}

/** ResetBadge puts a colour row back; both rows use it so they stay alike. */
function ResetBadge({ label, dim, onClick }: { label: string; dim?: boolean; onClick: () => void }) {
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={label}
        onClick={onClick}
        className={`inline-flex h-[var(--btn-h)] w-[var(--btn-h)] shrink-0 items-center justify-center rounded-[var(--radius-pill)] bg-carbon-surface2 text-carbon-textSub transition-colors hover:text-carbon-text ${
          dim ? 'opacity-45' : ''
        }`}
        {...tipHoverProps}
      >
        <IconRetry width={16} height={16} />
      </button>
      {tip.node}
    </>
  );
}

/**
 * Which half of the page is drawn: the General tab or the Aussehen tab. One
 * component, because both halves share the draft, the accent memory, the
 * palette and the save error.
 */
type LookSection = 'general' | 'appearance';

export function Look({ section = 'general' }: { section?: LookSection } = {}) {
  const appearance = section === 'appearance';
  const general = section === 'general';
  const { t } = useT();
  const { cfg, saved, patch, patchNow } = useDraft();
  const { toast } = useToast();

  // Language and light/dark are per-browser (lib/theme.ts, LanguagePicker.tsx)
  // and save themselves.
  const [theme, setThemeState] = useState(getTheme);
  useEffect(() => onThemeChange(setThemeState), []);

  // From the store, so the selector shows what the rails draw.
  const navLabels = useNavLabels();

  // Motion intensity is per-browser too.
  const [motion, setMotion] = useState<Motion>(readCachedMotionIntensity);

  // The hidden fourth level (lib/appearance.ts's stormTap). Finding it is state
  // of this screen and never stored, so leaving with another level selected
  // hides it again; the chosen value is stored like any other. It starts found
  // while it is the level in force.
  const [stormFound, setStormFound] = useState(() => motion === 'storm');
  // Counting, not rendering; any tap off the top level resets it.
  const stormTaps = useRef({ taps: 0 });

  // Disco, the colour engine's egg (lib/appearance.ts's discoTap), found by
  // turning rainbow mode on five times in quick succession. The switch is
  // stored per browser; having found it is state of this screen, like the
  // storm, and it starts found while disco is on.
  const [disco, setDisco] = useState(readCachedDisco);
  const [discoFound, setDiscoFound] = useState(disco);
  const discoTaps = useRef({ taps: 0, last: 0 });

  // The rainbow rows below paint their positions here, and disco moves them.
  useRainbow();

  // The saved palette when complete, else the built-in hues, so "reset" and
  // "never customised" look alike.
  const palette =
    cfg.rainbowPalette && cfg.rainbowPalette.length === RAINBOW.length ? cfg.rainbowPalette : RAINBOW;

  // Every pick is applied to the document root at once as a live preview;
  // Layout.tsx applies the saved look at boot. Disco comes after the rainbow,
  // because applying a stored state resets whatever step the walk had reached.
  useEffect(() => {
    const rainbow = rainbowFromSettings(cfg);
    applyShape(cfg.shape);
    applyAccent(cfg.accent);
    applyRainbow(rainbow);
    applyDisco(disco, rainbow);
    applyMotion(motion);
    cacheAppearance(cfg.shape, cfg.accent, rainbow);
  }, [
    cfg.shape,
    cfg.accent,
    cfg.rainbow,
    cfg.rainbowReactive,
    cfg.rainbowRotate,
    cfg.rainbowSeed,
    // The palette is an array, so the effect depends on its contents.
    cfg.rainbowPalette?.join(),
    motion,
    disco,
  ]);

  // This page saves every change at once, debounced like Advanced's search, so
  // dragging a colour sends one PATCH. The first run is skipped, since it only
  // sees the value loaded from the server.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    // A value another tab saved arrives in both the draft and `saved`; sending
    // it back would only echo it, and could land after a newer one.
    if (LOOK_KEYS.every((k) => same(cfg[k], saved[k]))) return;
    const id = setTimeout(() => {
      patchNow({
        shape: cfg.shape,
        accent: cfg.accent,
        rainbow: cfg.rainbow,
        rainbowReactive: cfg.rainbowReactive,
        rainbowRotate: cfg.rainbowRotate,
        rainbowSeed: cfg.rainbowSeed,
        rainbowPalette: cfg.rainbowPalette,
      })
        .then(() => toast(t('settings.saved'), 'ok'))
        // Only a toast: the debounced save has no button to shake.
        .catch((e) =>
          toast(t('settings.look.saveFailed', { error: String(e).replace(/^(Error|ApiError):\s*/, '') }), 'fail'),
        );
    }, 400);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    cfg.shape,
    cfg.accent,
    cfg.rainbow,
    cfg.rainbowReactive,
    cfg.rainbowRotate,
    cfg.rainbowSeed,
    cfg.rainbowPalette?.join(),
  ]);

  const accentLive = live(cfg.accent);

  // Mirrored into a ref so each writer sees the last write at once; the picker
  // fires on every drag frame.
  const [slots, setSlots] = useState<SlotMemory>(readSlotMemory);
  const liveSlots = useRef(slots);
  liveSlots.current = slots;
  const persistSlots = useCallback((update: (prev: SlotMemory) => SlotMemory) => {
    const next = update(liveSlots.current);
    // The ref first, so a second call in the same tick builds on this one.
    liveSlots.current = next;
    setSlots(next);
    writeSlotMemory(next);
  }, []);

  /** shownAt is what circle `i` shows: its mixed colour or its preset. */
  const shownAt = (i: number) => slots.customs[String(i)] ?? ACCENTS[i].hex;
  const wearsAccent = (i: number) => shownAt(i).toLowerCase() === accentLive;

  // An accent that no circle holds, such as one mixed in another browser, is
  // adopted by its nearest circle when that one is empty; a mixed colour is
  // never overwritten.
  useEffect(() => {
    if (!cfg.accent) return;
    // Through the ref, so this does not re-run for its own writes.
    const memory = liveSlots.current;
    const held = (i: number) => memory.customs[String(i)] ?? ACCENTS[i].hex;
    if (ACCENTS.some((_, i) => held(i).toLowerCase() === accentLive)) return;
    const i = accentSlot(accentLive);
    if (memory.customs[String(i)] !== undefined) return;
    persistSlots((p) => ({ slot: i, customs: { ...p.customs, [String(i)]: accentLive } }));
  }, [cfg.accent, accentLive, persistSlots]);

  // The marked circle: the stored choice while it still wears the accent, else
  // any circle wearing the accent exactly, else the nearest preset. The last
  // case can mark a circle mixed to another colour rather than overwrite it.
  const worn = ACCENTS.findIndex((_, i) => wearsAccent(i));
  const markedSlot =
    slots.slot !== undefined && wearsAccent(slots.slot)
      ? slots.slot
      : worn >= 0
        ? worn
        : accentSlot(accentLive);

  /** chooseSlot makes a circle the chosen one and wears what it shows. */
  const chooseSlot = (i: number, hex: string) => {
    persistSlots((p) => ({ ...p, slot: i }));
    patch({ accent: hex });
  };

  /** mixSlot stores a picked colour in its slot and wears it at once. */
  const mixSlot = (i: number, hex: string) => {
    persistSlots((p) => ({ ...p, slot: i, customs: { ...p.customs, [String(i)]: hex } }));
    patch({ accent: hex });
  };

  return (
    <div className="flex flex-col gap-10">
      {/* Each card title is a notch badge with its own rainbow position. */}
      {appearance && (
      <Card hue={0} className="flex flex-col gap-3">
        <SectionTitle hint={t('settings.shapeHint')}>
          {t('settings.shape')}
        </SectionTitle>
        {/* The well variant of Tabs: one padded track, equal segments, no glyphs. */}
        <Tabs
          label={t('settings.shape')}
          variant="well"
          active={cfg.shape}
          onSelect={(id) => patch({ shape: id as Shape })}
          items={SHAPES.map((s) => ({ id: s, label: t(`settings.shape.${s}` as never) }))}
        />
      </Card>
      )}

      {/* How navigation entries are drawn, for the sidebar and the settings rail
          at once. It writes through the store as well as the draft, since the
          sidebar renders outside this page's provider (lib/navLabels.ts). */}
      {appearance && (
      <Card hue={9} className="flex flex-col gap-3">
        <SectionTitle hint={t('settings.navLabels.titleHint')}>
          {t('settings.navLabels.title')}
        </SectionTitle>
        <Tabs
          label={t('settings.navLabels.title')}
          variant="well"
          active={navLabels}
          onSelect={(id) => {
            const next = asNavLabelMode(id);
            setNavLabels(next);
            patch({ navLabels: next });
          }}
          items={[
            { id: 'both', label: t('settings.navLabels.both') },
            { id: 'glyph', label: t('settings.navLabels.glyph') },
            { id: 'text', label: t('settings.navLabels.text') },
            { id: 'hover', label: t('settings.navLabels.hover') },
          ]}
        />
      </Card>
      )}

      {/* Motion intensity (index.css, lib/appearance.ts). The hidden fourth
          level joins the list only while it has just been found or is in
          force. */}
      {appearance && (
      <Card hue={8} className="flex flex-col gap-3">
        <SectionTitle hint={t('settings.motion.hint')}>
          {t('settings.motion.title')}
        </SectionTitle>
        <Tabs
          label={t('settings.motion.title')}
          variant="well"
          active={motion}
          onSelect={(id) => {
            // A tap on the active segment, which a picker would otherwise
            // swallow, counts toward the hidden level.
            const found = stormTap(stormTaps.current, id, motion);
            const next = found ?? (id as Motion);
            if (found) setStormFound(true);
            setMotion(next);
            applyMotion(next);
            cacheMotionIntensity(next);
          }}
          items={(stormFound || motion === 'storm' ? [...MOTION_LEVELS, 'storm' as Motion] : MOTION_LEVELS).map((m) => ({
            id: m,
            label: t(`settings.motion.${m}` as never),
          }))}
        />
      </Card>
      )}

      {appearance && (
      <Card hue={1} className="flex flex-col gap-4">
        <SectionTitle>{t('settings.colours')}</SectionTitle>

        {/* Label on the left, circles on the right. While rainbow mode is on
            the accent still paints controls outside any palette position, so
            the row stays and the circles dim but stay pressable; the dimming
            sits on the circles so the (i), which then explains who is in
            charge, keeps full strength. */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="flex shrink-0 items-center gap-1.5 text-sm text-carbon-text">
            {t('settings.accent')}
            <InfoBubble
              tip={
                cfg.rainbow
                  ? `${t('settings.accentHint')} ${t('settings.accentRainbowOwns')}`
                  : t('settings.accentHint')
              }
            />
          </span>
          {/* Eight circles and a reset, like the palette row. Every circle opens
              the picker once selected. */}
          <div className="flex flex-wrap items-center gap-2">
            {ACCENTS.map((a, i) => {
              // Each circle wears what it was last mixed to (SlotMemory).
              const shown = shownAt(i);
              const mine = i === markedSlot;
              return (
                <RingSwatch
                  // Keyed by the preset: a key that followed the colour would
                  // replace the node on the first drag frame and break the
                  // picker's outside-click handling (lib/colorPicker.ts).
                  key={a.hex}
                  color={shown}
                  // The preset's name while it wears it, else its hex.
                  label={shown.toLowerCase() !== a.hex.toLowerCase() ? shown.toUpperCase() : a.name}
                  selected={mine}
                  dim={cfg.rainbow}
                  onPick={() => chooseSlot(i, shown)}
                  // Opens on the circle's own colour.
                  onEdit={(el) => openColorPickerPopover(el, shown, (hex) => mixSlot(i, hex))}
                />
              );
            })}
            {/* Always shown. It forgets every mixed colour and the accent
                with them. */}
            <ResetBadge
              label={t('settings.accentReset')}
              dim={cfg.rainbow}
              onClick={() => {
                persistSlots(() => ({ customs: {} }));
                patch({ accent: '' });
              }}
            />
          </div>
        </div>

        <div className="flex flex-col gap-3">
          {/* The master switch and its two sub-switches form their own hue
              sequence. */}
          <div className="glim-hue flex items-start justify-between gap-4" style={hueVars(rainbowAt(0)) as CSSProperties}>
            <span className="flex items-center gap-1.5 text-sm text-carbon-text">
              {t('settings.rainbow')}
              <InfoBubble tip={t('settings.rainbowHint')} />
            </span>
            <Toggle
              hideLabel
              label={t('settings.rainbowOn')}
              checked={cfg.rainbow}
              onChange={(v) => {
                patch({ rainbow: v });
                // The fifth quick turn-on unlocks disco and starts it, since
                // the gesture ends with the rainbow on and the walk visible.
                if (discoTap(discoTaps.current, v, { now: Date.now() })) {
                  setDiscoFound(true);
                  setDisco(true);
                  cacheDisco(true);
                }
              }}
            />
          </div>

          {/* Absent while rainbow mode is off, since the palette does nothing
              then. */}
          {cfg.rainbow && (
          <div className="flex flex-col gap-3">
            <div className="glim-hue flex items-start justify-between gap-4" style={hueVars(rainbowAt(1)) as CSSProperties}>
              <span className="flex items-center gap-1.5 text-sm text-carbon-text">
                {t('settings.rainbowReactive')}
                <InfoBubble tip={t('settings.rainbowReactiveHint')} />
              </span>
              <Toggle
                hideLabel
                label={t('settings.rainbowReactive')}
                checked={cfg.rainbowReactive}
                onChange={(v) => patch({ rainbowReactive: v })}
              />
            </div>
            <div className="glim-hue flex items-start justify-between gap-4" style={hueVars(rainbowAt(2)) as CSSProperties}>
              <span className="flex items-center gap-1.5 text-sm text-carbon-text">
                {t('settings.rainbowRotate')}
                <InfoBubble tip={t('settings.rainbowRotateHint')} />
              </span>
              <Toggle
                hideLabel
                label={t('settings.rainbowRotate')}
                checked={cfg.rainbowRotate}
                onChange={(v) =>
                  // A fresh offset, so switching rotation on changes something.
                  patch({
                    rainbowRotate: v,
                    rainbowSeed: v ? 1 + Math.floor(Math.random() * (RAINBOW.length - 1)) : 0,
                  })
                }
              />
            </div>
            {/* Offered while it is on or has just been found, and gone once
                this screen closes with it off. */}
            {(discoFound || disco) && (
              <div className="glim-hue flex items-start justify-between gap-4" style={hueVars(rainbowAt(3)) as CSSProperties}>
                <span className="flex items-center gap-1.5 text-sm text-carbon-text">
                  {t('settings.rainbowDisco')}
                  <InfoBubble tip={t('settings.rainbowDiscoHint')} />
                </span>
                <Toggle
                  hideLabel
                  label={t('settings.rainbowDisco')}
                  checked={disco}
                  onChange={(v) => {
                    setDisco(v);
                    cacheDisco(v);
                  }}
                />
              </div>
            )}

            {/* Label on the left, eight squares and a reset on the right, like
                the accent row. */}
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="flex shrink-0 items-center gap-1.5 text-sm text-carbon-text">
                {t('settings.rainbowPaletteLabel')}
                <InfoBubble tip={t('settings.rainbowPaletteHint')} />
              </span>
              <div className="flex flex-wrap items-center gap-2">
                {palette.map((hex, i) => (
                  // A button with its own accessible name, opening the app's picker.
                  <PaletteSwatch
                    key={i}
                    color={hex}
                    name={`${t('settings.rainbowPalette')} ${i + 1}`}
                    onEdit={(el) =>
                      openColorPickerPopover(el, hex, (next) => {
                        const list = palette.slice();
                        list[i] = next;
                        patch({ rainbowPalette: list });
                      })
                    }
                  />
                ))}
                <ResetBadge label={t('settings.accentReset')} onClick={() => patch({ rainbowPalette: null })} />
              </div>
            </div>
          </div>
          )}
        </div>
      </Card>
      )}

      {general && <NotificationsCard hue={0} />}

      {appearance && (
      <Card hue={4} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.theme')}</SectionTitle>
        <Tabs
          label={t('settings.theme')}
          variant="well"
          active={theme}
          onSelect={(id) => setTheme(id as 'dark' | 'light')}
          items={[
            { id: 'dark', label: t('theme.dark'), icon: <IconMoon width={16} height={16} /> },
            { id: 'light', label: t('theme.light'), icon: <IconSun width={16} height={16} /> },
          ]}
        />
      </Card>
      )}

      {appearance && (
      <Card hue={3} className="flex flex-col gap-3">
        <SectionTitle>{t('lang.label')}</SectionTitle>
        {/* standalone: OnboardingWizard mounts a second instance at the same
            time (see LanguagePicker.tsx). */}
        <LanguagePicker
          direction="down"
          standalone
          className="glim-well flex w-fit min-w-[12rem] items-center gap-2.5 px-3 py-2 text-sm text-carbon-text"
        />
      </Card>
      )}

      {general && <MutedDialogsCard hue={1} />}
      {general && <UpdateCard hue={2} />}
      {general && <SystemCards hue={3} />}
      {/* Last on the General tab, where a version and a contact are looked for. */}
      {general && <About hue={5} />}
    </div>
  );
}

/**
 * MutedDialogsCard brings back dialogs silenced with "do not show this again",
 * one switch each. It is absent while nothing is silenced.
 */
function MutedDialogsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const dialogs = useDialogMute();
  if (dialogs.muted.length === 0) return null;

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={t('settings.dialogs.hint')}>{t('settings.dialogs.title')}</SectionTitle>
      {MUTABLE_DIALOGS.filter((d) => dialogs.isMuted(d.id)).map((d) => (
        <ToggleRow
          key={d.id}
          hue={0}
          label={t(d.label)}
          checked={false}
          onChange={() => dialogs.setMuted(d.id, false)}
        />
      ))}
    </Card>
  );
}

/**
 * SystemCards holds quit and restart and the transfer card, on `hue` and the
 * one after it. LifecycleCard does the deployment fetch alone, so a slow
 * /api/deployment never hides the transfer card; both share `shuttingDown`,
 * since a restore can restart the server.
 */
function SystemCards({ hue }: { hue: number }) {
  const [shuttingDown, setShuttingDown] = useState(false);
  return (
    <>
      <LifecycleCard hue={hue} shuttingDown={shuttingDown} onShutdown={() => setShuttingDown(true)} />
      <SettingsTransfer hue={hue + 1} onShutdown={() => setShuttingDown(true)} />
    </>
  );
}

/** LifecycleCard loads on its own, so a failed deployment fetch drops only this card. */
function LifecycleCard({ hue, shuttingDown, onShutdown }: { hue: number; shuttingDown: boolean; onShutdown: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const { data, failed, loading, reload } = useResource<DeploymentInfo>(fetchDeploymentInfo);

  const [confirmAction, setConfirmAction] = useState<'quit' | 'restart' | null>(null);
  const [acting, setActing] = useState(false);

  // Keyed onto the confirm button so a repeated refusal shakes again.
  const [actShake, setActShake] = useState(0);

  async function confirmLifecycle() {
    if (!confirmAction) return;
    setActing(true);
    try {
      const res = confirmAction === 'quit' ? await requestQuit() : await requestRestart();
      onShutdown();
      setConfirmAction(null);
      void res;
    } catch (e) {
      // The window stays open so the pressed button can shake.
      toast(t('settings.system.actionFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setActShake((n) => n + 1);
    } finally {
      setActing(false);
    }
  }

  // Nothing while loading, like UpdateCard.
  if (loading) return null;
  if (failed || !data) {
    return <ErrorCard message={t('settings.system.loadFailed')} retry={reload} retryLabel="↻" />;
  }

  if (shuttingDown) {
    return (
      <Card hue={hue} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.system.shuttingDownTitle')}</SectionTitle>
        <p className="text-sm text-carbon-text">{t('settings.system.shuttingDown')}</p>
      </Card>
    );
  }

  return (
    <>
      <Card hue={hue} className="flex flex-col gap-3">
        {/* The note comes from translated keys by deployment, since the server
            sends it in English; an unavailable reason wins. */}
        <SectionTitle
          hint={
            !data.canQuit || !data.canRestart
              ? t('settings.system.unavailable')
              : t(data.deployment === 'desktop' ? 'settings.system.lifecycleNoteDesktop' : 'settings.system.lifecycleNoteContainer')
          }
        >
          {t('settings.system.lifecycleTitle')}
        </SectionTitle>
        <div className="flex flex-wrap items-center gap-3">
          {/* hue overrides kind's colour, so both buttons look alike. */}
          <Button
            hue={hue}
            kind="primary"
            icon={<IconSignOut width={16} height={16} />}
            disabled={!data.canQuit || acting}
            onClick={() => setConfirmAction('quit')}
          >
            {t('settings.system.quit')}
          </Button>
          <Button
            hue={hue}
            kind="primary"
            icon={<IconRetry width={16} height={16} />}
            disabled={!data.canRestart || acting}
            onClick={() => setConfirmAction('restart')}
          >
            {t('settings.system.restart')}
          </Button>
        </div>
      </Card>

      {confirmAction && (
        <Modal
          title={t(confirmAction === 'quit' ? 'settings.system.quitConfirmTitle' : 'settings.system.restartConfirmTitle')}
          onClose={() => (acting ? undefined : setConfirmAction(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('settings.system.confirmCancel')}
                onClick={() => setConfirmAction(null)}
                disabled={acting}
              />
              <Button
                key={actShake}
                className={actShake > 0 ? 'glim-shake' : ''}
                kind="ghost"
                onClick={() => void confirmLifecycle()}
                disabled={acting}
              >
                {acting ? t('settings.system.acting') : t('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('settings.system.quitConfirmBody', { note: data.note })}</p>
        </Modal>
      )}
    </>
  );
}

/**
 * UpdateCard checks GitHub for a newer release on both deployments, on request
 * or once on mount when the auto-check switch is on (off by default). A
 * container is pointed at the release; the desktop build can also install
 * through internal/update, which does the download, swap and relaunch.
 */
function UpdateCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { toast } = useToast();
  const [deployment, setDeployment] = useState<string | null>(null);
  const [check, setCheck] = useState<UpdateCheckT | null>(null);
  const [checking, setChecking] = useState(false);
  const [installing, setInstalling] = useState(false);
  // Keyed onto the button so a repeated refusal shakes again.
  const [installShake, setInstallShake] = useState(0);
  // Once true, stays true: the process is on its way to relaunch.
  const [installed, setInstalled] = useState(false);

  useEffect(() => {
    void fetchDeploymentInfo()
      .then((d) => setDeployment(d.deployment))
      .catch(() => {});
  }, []);

  const onCheck = useCallback(async () => {
    setChecking(true);
    try {
      setCheck(await fetchUpdateCheck());
    } catch {
      setCheck({ checked: false, available: false, current: '' });
    } finally {
      setChecking(false);
    }
  }, []);

  const onInstall = useCallback(async () => {
    setInstalling(true);
    try {
      await installUpdate();
      setInstalled(true);
    } catch (e) {
      // A network error is ambiguous, since the process may already be
      // exiting, but a retryable failure beats a spinner that never ends.
      toast(t('settings.look.updatesInstallFailed', { error: String(e).replace(/^(Error|ApiError):\s*/, '') }), 'fail');
      setInstallShake((n) => n + 1);
      setInstalling(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- toast/t are stable for this card's lifetime
  }, []);

  // Auto-check once, when the switch's value arrives.
  useEffect(() => {
    if (cfg.autoUpdateCheck) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fires once
    // when deployment/autoUpdateCheck first resolve, not on every cfg change.
  }, [deployment, cfg.autoUpdateCheck]);

  // Auto-install once, when a check this page ran finds an update and the
  // switch is on; turning the switch on does not act on an older result.
  useEffect(() => {
    if (deployment === 'desktop' && cfg.autoUpdateInstall && check?.checked && check.available && !installing && !installed) {
      toast(t('settings.look.updatesAutoInstalling', { version: check.latest ?? '' }), 'info');
      void onInstall();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reacts only to a fresh check result
  }, [check]);

  // Wait for the deployment rather than flash the wrong copy.
  if (deployment === null) return null;
  const isDesktop = deployment === 'desktop';
  const canInstallNow = isDesktop && !installed && check?.checked && check.available;

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={t('settings.look.updatesHint')}>
        {t('settings.look.updatesTitle')}
      </SectionTitle>
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm text-carbon-text">{t('settings.look.updatesAuto')}</span>
        <Toggle checked={cfg.autoUpdateCheck} onChange={(v) => patch({ autoUpdateCheck: v })} label={t('settings.look.updatesAuto')} hideLabel />
      </div>
      {/* Shown on both builds; a container cannot install from here (the route
          answers 501), so the row is disabled and says why. */}
      <ToggleRow
        label={t('settings.look.updatesAutoInstall')}
        hint={isDesktop ? t('settings.look.updatesAutoInstallHint') : t('settings.look.updatesAutoInstallContainerHint')}
        checked={isDesktop && cfg.autoUpdateInstall}
        disabled={!isDesktop}
        onChange={(v) => patch({ autoUpdateInstall: v })}
      />
      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" onClick={() => void onCheck()} disabled={checking || installing}>
          {checking ? t('settings.look.updatesChecking') : t('settings.look.updatesCheck')}
        </Button>
        {canInstallNow && (
          <Button
            key={installShake}
            className={installShake > 0 ? 'glim-shake' : ''}
            kind="primary"
            onClick={() => void onInstall()}
            disabled={installing}
          >
            {installing ? t('settings.look.updatesInstalling') : t('settings.look.updatesInstallNow')}
          </Button>
        )}
        {check && !check.checked && <span className="text-sm text-statusFail">{t('settings.look.updatesFailed')}</span>}
        {check && check.checked && !check.available && (
          <span className="text-sm text-statusOk">{t('settings.look.updatesCurrent', { version: check.current })}</span>
        )}
        {check && check.checked && check.available && check.url && (
          <a href={check.url} target="_blank" rel="noopener noreferrer" className="text-sm font-medium text-accentInk hover:underline">
            {t(isDesktop ? 'settings.look.updatesAvailable' : 'settings.look.updatesAvailableContainer', { version: check.latest ?? '' })}
          </a>
        )}
      </div>
      {installed && <p className="text-sm text-statusOk">{t('settings.look.updatesInstalled')}</p>}
    </Card>
  );
}

/**
 * live returns the accent in force, lower-cased, with an empty setting meaning
 * the built-in one. It is the fallback when no stored slot choice applies.
 */
function live(accent: string | undefined): string {
  return (accent || DEFAULT_ACCENT).toLowerCase();
}
