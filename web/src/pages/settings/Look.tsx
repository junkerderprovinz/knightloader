import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { Card, InfoBubble, SectionTitle, Toggle } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { LanguagePicker } from '../../components/LanguagePicker';
import { IconMoon, IconRetry, IconSun } from '../../lib/icons';
import { QuietModeToggle, useToast } from '../../lib/toast';
import { getTheme, onThemeChange, setTheme } from '../../lib/theme';
import { useT } from '../../lib/i18n';
import {
  ACCENTS,
  DEFAULT_ACCENT,
  RAINBOW,
  SHAPES,
  type Shape,
  applyAccent,
  applyRainbow,
  applyShape,
  cacheAppearance,
  hueVars,
  rainbowAt,
  rainbowFromSettings,
} from '../../lib/appearance';
import { useDraft } from './context';

/**
 * A pill-shaped colour swatch wrapped in its own selection ring — a second
 * span whose border-colour is the only thing that changes between picked
 * and idle. Ported verbatim from the real BombVault test container's own
 * accent/palette widget (read off its live DOM, not the repo or the docs —
 * jdp: "Nein das ist falsch! Hier ist der Testcontainer erreichbar..."),
 * which is a different visual language than ui.tsx's own `Swatch`
 * (control-radius, halo-shadow selection) — that component styles
 * something else now and this one is deliberately local to this page
 * rather than a third variant bolted onto the shared one.
 */
function RingSwatch({
  color,
  label,
  selected,
  onPick,
}: {
  color: string;
  label: string;
  selected: boolean;
  onPick: () => void;
}) {
  return (
    <span
      className="inline-flex rounded-[var(--radius-pill)] border-2 transition-transform hover:scale-110"
      style={{ borderColor: selected ? 'var(--carbon-text)' : 'var(--carbon-border)' }}
    >
      <button
        type="button"
        title={label}
        aria-label={label}
        onClick={onPick}
        className="h-6 w-6 rounded-[var(--radius-pill)]"
        style={{ backgroundColor: color }}
      />
    </span>
  );
}

export function Look() {
  const { t } = useT();
  const { cfg, patch, patchNow } = useDraft();
  const { toast } = useToast();
  const [saveError, setSaveError] = useState('');

  // Sprache and Hell/Dunkel are per-browser preferences (localStorage, not
  // this draft's server document - see lib/theme.ts and LanguagePicker.tsx),
  // so they already save themselves the instant they change with nothing
  // further to do here. They live ONLY here now, not in the sidebar too.
  const [theme, setThemeState] = useState(getTheme);
  useEffect(() => onThemeChange(setThemeState), []);

  // What the swatch row edits: the saved palette when it is complete, the
  // built-in hues otherwise. Either way the row shows eight editable colours, so
  // "reset" and "never customised" look the same and behave the same.
  const palette =
    cfg.rainbowPalette && cfg.rainbowPalette.length === RAINBOW.length ? cfg.rainbowPalette : RAINBOW;

  // The look follows every pick here so what is on screen is what is selected.
  // It is applied to the document root and not to this page: a look that only
  // existed while this page was mounted would be no look at all, which is why
  // Layout.tsx does the same thing at boot. This is the live preview half.
  useEffect(() => {
    const rainbow = rainbowFromSettings(cfg);
    applyShape(cfg.shape);
    applyAccent(cfg.accent);
    applyRainbow(rainbow);
    cacheAppearance(cfg.shape, cfg.accent, rainbow);
  }, [
    cfg.shape,
    cfg.accent,
    cfg.rainbow,
    cfg.rainbowReactive,
    cfg.rainbowRotate,
    cfg.rainbowSeed,
    // The palette is an array, so the effect has to depend on its contents; the
    // identity changes on every keystroke of the colour picker anyway.
    cfg.rainbowPalette?.join(),
  ]);

  // Saves the instant anything on this page changes - every other settings
  // page still goes through the shared draft + the sticky Save bar (a rule
  // set or a resolver's API key is not something a stray click should
  // silently persist), but this page is nothing but instant visual feedback
  // already, via the effect above. Debounced on the same schedule as
  // Advanced's search box, so dragging a colour sends one PATCH once it
  // settles rather than one per input event; the very first run is skipped,
  // since that one fires on mount with the value just loaded from the
  // server, not a change to save.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    const id = setTimeout(() => {
      setSaveError('');
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
        .catch((e) => setSaveError(String(e).replace(/^(Error|ApiError):\s*/, '')));
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

  return (
    <div className="flex flex-col gap-5">
      {/* Every card title below is a "notch" badge - a filled pill sitting
          half over the card's own top edge - and carries its own rainbow
          position (hue 0-4, this page's own sequence), exactly like the
          real BombVault test container's five Aussehen-tab cards. Read
          directly off that container's DOM, not the repo or GlimStone's
          docs, both of which turned out to describe something not actually
          live anywhere jdp was looking (jdp: "Bitte orientiere dich am
          Bombvault-Testcontainer!!!", then, when the WRONG container had
          been the reference all along: "Nein das ist falsch! Hier ist der
          Testcontainer erreichbar..."). */}
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={0} hint={t('settings.shapeHint')}>
          {t('settings.shape')}
        </SectionTitle>
        {/* The well variant (Tabs.tsx) - one shared padded track, equal
            segments, no per-item glyph (jdp: "die Auswahlfelder der Ecken
            soll ein horizontaler Selektor werden, Auswahlflächen ohne
            icon") - ported from the real container's own Corners picker,
            not the bare-badge default this used before. */}
        <Tabs
          label={t('settings.shape')}
          variant="well"
          className="w-fit"
          active={cfg.shape}
          onSelect={(id) => patch({ shape: id as Shape })}
          items={SHAPES.map((s) => ({ id: s, label: t(`settings.shape.${s}` as never) }))}
        />
      </Card>

      <Card className="flex flex-col gap-4">
        <SectionTitle hue={1}>{t('settings.colours')}</SectionTitle>

        {/* One row, not a label above a row of its own (jdp: "Akzentfarbe:
            die Farbfelder nicht in eine neue Zeile sondern rechts von dem
            Text Akzentfarbe verschieben") - exactly how the real BombVault
            test container lays this row out: "Akzentfarbe:" then the swatch
            then "Voreinstellungen:" then all eight presets, one flex-wrap
            line. */}
        <div className="flex flex-wrap items-center gap-3">
          {/* me-2 on top of the row's own gap-3, not just a bigger gap-*:
              the label needs more breathing room before the colour fields
              start than the fields need between each other (jdp: "der
              Abstand zwischen Akzentfarbe [...] zu den Farbfeldern
              vergrößern") - widening the row's shared gap would also push
              every swatch further apart from its neighbour, which nobody
              asked for. */}
          <span className="me-2 flex shrink-0 items-center gap-1.5 text-sm text-carbon-text">
            {t('settings.accent')}
            <InfoBubble tip={t('settings.accentHint')} />
          </span>
          {/* The current/custom accent trigger - a plain circle, no
              selection ring of its own (the real container doesn't give it
              one either), a native colour input as the invisible click
              target. Not GlimStone's documented popover picker
              (reference/colorPicker.ts) - that has no KnightLoader port
              yet, and building one is a separate, dedicated piece of work
              rather than something to fold into a visual-parity pass. */}
          <span
            className="relative inline-flex h-6 w-6 shrink-0 overflow-hidden rounded-[var(--radius-pill)] border-2 border-carbon-border"
            title={t('settings.accent')}
          >
            <span aria-hidden className="pointer-events-none absolute inset-0" style={{ backgroundColor: accentLive }} />
            <input
              type="color"
              value={accentLive}
              onChange={(e) => patch({ accent: e.target.value })}
              aria-label={t('settings.accent')}
              className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
            />
          </span>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-carbon-textMuted">{t('settings.accentPresets')}:</span>
            {ACCENTS.map((a) => (
              <RingSwatch
                key={a.hex}
                color={a.hex}
                label={a.name}
                selected={accentLive === a.hex.toLowerCase()}
                onPick={() => patch({ accent: a.hex })}
              />
            ))}
            {/* Icon badge, not a text link (rule 13: "a small, single-purpose
                action badge carries an icon"). Only once the accent has
                actually moved off the default. */}
            {accentLive !== DEFAULT_ACCENT.toLowerCase() && (
              <button
                type="button"
                title={t('settings.accentReset')}
                aria-label={t('settings.accentReset')}
                onClick={() => patch({ accent: '' })}
                className="inline-flex h-6 w-6 items-center justify-center rounded-[var(--radius-pill)] bg-carbon-surface2 text-carbon-textSub transition-colors hover:text-carbon-text"
              >
                <IconRetry width={13} height={13} />
              </button>
            )}
          </div>
        </div>

        <div className="flex flex-col gap-3">
          {/* The master switch, and each of the two sub-switches below it,
              carries its OWN rainbow position - a separate 0-based sequence
              from the card titles above, exactly as measured live: this
              set of three rows is its own equal-member set. */}
          <div className="glim-hue flex items-start justify-between gap-4" style={hueVars(rainbowAt(0)) as CSSProperties}>
            <span className="flex items-center gap-1.5 text-sm text-carbon-text">
              {t('settings.rainbow')}
              <InfoBubble tip={t('settings.rainbowHint')} />
            </span>
            <Toggle
              hideLabel
              label={t('settings.rainbowOn')}
              checked={cfg.rainbow}
              onChange={(v) => patch({ rainbow: v })}
            />
          </div>

          {/* Disabled rather than hidden: a control that vanishes teaches
              nobody what the mode can do. */}
          <div className={`flex flex-col gap-3 transition-opacity ${cfg.rainbow ? '' : 'pointer-events-none opacity-50'}`}>
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
                  // Turning rotation on draws a fresh offset, so the switch does
                  // something visible instead of re-applying the rotation the
                  // palette already had.
                  patch({
                    rainbowRotate: v,
                    rainbowSeed: v ? 1 + Math.floor(Math.random() * (RAINBOW.length - 1)) : 0,
                  })
                }
              />
            </div>

            {/* The very same job as the accent swatches above, and the same
                one-row treatment (jdp: "Bei der Zeile der Regenbogen
                Farbpalette auch Farbpalette davor, ... rechts von dem Text
                verschieben"): eight native colour inputs, plus an icon-only
                reset badge - always rendered, disabled rather than hidden
                along with the rest of this sub-section. */}
            <div className="flex flex-wrap items-center gap-2">
              {/* Same reasoning as the Akzentfarbe row above: extra room
                  after the label specifically, not a bigger shared gap. */}
              <span className="me-2 flex shrink-0 items-center gap-1.5 text-sm text-carbon-text">
                {t('settings.rainbowPaletteLabel')}
                <InfoBubble tip={t('settings.rainbowPaletteHint')} />
              </span>
              {palette.map((hex, i) => (
                <label
                  key={i}
                  title={`${t('settings.rainbowPalette')} ${i + 1}`}
                  className="relative h-7 w-7 shrink-0 overflow-hidden rounded-[var(--radius-pill)] border-2 border-carbon-border"
                  style={{ backgroundColor: hex }}
                >
                  <input
                    type="color"
                    value={hex}
                    disabled={!cfg.rainbow}
                    onChange={(e) => {
                      const next = palette.slice();
                      next[i] = e.target.value;
                      patch({ rainbowPalette: next });
                    }}
                    className="absolute inset-0 h-full w-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
                  />
                </label>
              ))}
              <button
                type="button"
                disabled={!cfg.rainbow}
                title={t('settings.accentReset')}
                aria-label={t('settings.accentReset')}
                onClick={() => patch({ rainbowPalette: null })}
                className="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-pill)] bg-carbon-surface2 text-carbon-textSub transition-colors hover:text-carbon-text"
              >
                <IconRetry width={14} height={14} />
              </button>
            </div>
          </div>
        </div>

        {saveError && <p className="text-xs text-statusFail">{t('settings.look.saveFailed', { error: saveError })}</p>}
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={2}>{t('notifications.quiet')}</SectionTitle>
        <QuietModeToggle />
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={3}>{t('lang.label')}</SectionTitle>
        {/* standalone: OnboardingWizard.tsx mounts a second, simultaneous
            instance of this same component - see LanguagePicker.tsx's own
            doc comment for why that needs its own local open/closed state
            rather than the shared store every other instance reads. */}
        <LanguagePicker
          direction="down"
          standalone
          className="glim-well flex w-fit min-w-[12rem] items-center gap-2.5 px-3 py-2 text-sm text-carbon-text"
        />
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={4}>{t('settings.theme')}</SectionTitle>
        <Tabs
          label={t('settings.theme')}
          variant="well"
          className="w-fit"
          active={theme}
          onSelect={(id) => setTheme(id as 'dark' | 'light')}
          items={[
            { id: 'dark', label: t('theme.dark'), icon: <IconMoon width={16} height={16} /> },
            { id: 'light', label: t('theme.light'), icon: <IconSun width={16} height={16} /> },
          ]}
        />
      </Card>
    </div>
  );
}

/**
 * The accent actually in force, lower-cased for comparison. An empty setting
 * means the built-in one, so "nothing chosen" and "the default chosen by hand"
 * put the ring on the same swatch — which is the truth, and the alternative is
 * a row where the live colour is unmarked until you click it.
 */
function live(accent: string | undefined): string {
  return (accent || DEFAULT_ACCENT).toLowerCase();
}
