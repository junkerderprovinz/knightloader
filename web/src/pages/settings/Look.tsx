import { useEffect, useRef, useState } from 'react';
import { Card, InfoBubble, SectionTitle, Toggle } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { LanguagePicker } from '../../components/LanguagePicker';
import { IconMoon, IconSun } from '../../lib/icons';
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
  rainbowFromSettings,
} from '../../lib/appearance';
import { useDraft } from './context';

export function Look() {
  const { t } = useT();
  const { cfg, patch, patchNow } = useDraft();
  const { toast } = useToast();
  const [saveError, setSaveError] = useState('');

  // Sprache and Hell/Dunkel are per-browser preferences (localStorage, not
  // this draft's server document - see lib/theme.ts and LanguagePicker.tsx),
  // so they already save themselves the instant they change with nothing
  // further to do here. They live ONLY here now, not in the sidebar too
  // (jdp: "Sprach und hell dunkel ist in der sidebar immer noch vorhanden" -
  // one control, one home, not two copies of the same switch).
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

  // Saves the instant anything on this page changes (jdp: "Wenn
  // einstellungen geändert werden, zb die badge form, dann soll man das
  // nicht speichern müssen. das soll automatisch sofort gespeichert
  // werden") - every other settings page still goes through the shared
  // draft + the sticky Save bar (a rule set or a resolver's API key is not
  // something a stray click should silently persist), but this page is
  // nothing but instant visual feedback already, via the effect above. A
  // Save button on top of a change already visible on screen only adds a
  // step nobody asked for. Debounced on the same schedule as Advanced's
  // search box, so dragging a colour sends one PATCH once it settles rather
  // than one per input event; the very first run is skipped, since that one
  // fires on mount with the value just loaded from the server, not a change
  // to save.
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
      {/* Corners has no BombVault equivalent (GlimStone-only, KnightLoader/
          TrickWork feature) - its own card rather than folded into Aussehen
          below, so it does not pretend to be part of the section that DOES
          have to match BV's exact shape. */}
      <Card className="flex flex-col gap-3">
        <SectionTitle hint={t('settings.shapeHint')}>{t('settings.shape')}</SectionTitle>
        {/* Plain text options, no per-item glyph (jdp: "die Auswahlfelder der
            Ecken soll ein horizontaler Selektor werden, Auswahlflächen ohne
            icon") - the outline-square glyph this used to carry per option
            is gone; the label alone says which corner radius each option
            picks. */}
        <Tabs
          label={t('settings.shape')}
          size="sm"
          className="w-fit"
          active={cfg.shape}
          onSelect={(id) => patch({ shape: id as Shape })}
          items={SHAPES.map((s) => ({ id: s, label: t(`settings.shape.${s}` as never) }))}
        />
      </Card>

      {/* Aussehen - a literal structural port of BombVault's own Settings >
          General > Appearance card (jdp, three rounds running: "Das ganze
          Theming soll in den Settings so aussehen wie im aktuellen BV
          Testcontainer", then "nicht exakt... übernommen... es fehlen
          eigene Cards. Die Toggles falsch angeordnet. Farbwähler und
          Resetbutton fehlen"). Same card title, same accent widget (a real
          native `<input type="color">` swatch plus five preset circles plus
          a Reset text link that only appears once the accent has actually
          been changed), same border-top divider before Rainbow, same
          same-row heading+master-switch, same native-input palette circles,
          same Quiet-toasts row shape at the bottom - not GlimStone's
          popover colour picker, on purpose, because this one card's job is
          to be the BV reference implementation rather than KnightLoader's
          own idiom. */}
      <Card className="flex flex-col gap-4">
        <SectionTitle>{t('settings.nav.look')}</SectionTitle>

        <div className="flex flex-col gap-2">
          <span className="text-sm text-carbon-text">{t('settings.accent')}</span>
          <div className="flex flex-wrap items-center gap-3">
            {/* The real colour picker: a native input, its own swatch of
                colour painted behind it so the control reads as "a colour"
                rather than as an invisible hit-target over a plain box. */}
            <label
              title={t('settings.accent')}
              className="relative h-8 w-14 shrink-0 cursor-pointer overflow-hidden rounded-[var(--radius-control)] bg-carbon-surface2 p-0.5"
            >
              <span
                aria-hidden
                className="pointer-events-none block h-full w-full rounded-[calc(var(--radius-control)-2px)]"
                style={{ backgroundColor: accentLive }}
              />
              <input
                type="color"
                value={accentLive}
                onChange={(e) => patch({ accent: e.target.value })}
                className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
              />
            </label>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-carbon-textMuted">{t('settings.accentPresets')}:</span>
              {ACCENTS.map((a) => (
                <button
                  key={a.hex}
                  type="button"
                  title={a.name}
                  onClick={() => patch({ accent: a.hex })}
                  className="h-6 w-6 rounded-full border-2 transition-transform hover:scale-110"
                  style={{
                    backgroundColor: a.hex,
                    borderColor: accentLive === a.hex.toLowerCase() ? 'var(--carbon-text)' : 'var(--carbon-border)',
                  }}
                />
              ))}
              {/* Only once the accent has actually moved off the default -
                  a reset that is always there and never does anything reads
                  as furniture, and the BV reference itself only shows it
                  conditionally. */}
              {accentLive !== DEFAULT_ACCENT.toLowerCase() && (
                <button
                  type="button"
                  onClick={() => patch({ accent: '' })}
                  className="ms-1 text-xs text-carbon-textMuted transition-colors hover:text-carbon-text"
                >
                  {t('settings.accentReset')}
                </button>
              )}
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-3 border-t border-carbon-border pt-4">
          {/* The master switch shares the heading's own row (BV's own fix
              for the exact trap GlimStone's Switches rule calls out: a
              caption-less toggle stranded far from the word that names it).
              No caption of its own - the heading beside it already says
              Rainbow. */}
          <div className="flex items-start justify-between gap-4">
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
            <div className="flex items-start justify-between gap-4">
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
            <div className="flex items-start justify-between gap-4">
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

            {/* The very same job as the accent swatch above: eight native
                colour inputs, each painted with its own palette colour. */}
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-carbon-textMuted">{t('settings.rainbowPalette')}:</span>
              {palette.map((hex, i) => (
                <label
                  key={i}
                  title={`${t('settings.rainbowPalette')} ${i + 1}`}
                  className="relative h-7 w-7 shrink-0 overflow-hidden rounded-full border-2 border-carbon-border"
                  style={{ backgroundColor: hex, opacity: cfg.rainbow ? undefined : 0.5 }}
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
                onClick={() => patch({ rainbowPalette: null })}
                className="ms-1 text-xs text-carbon-textMuted transition-colors hover:text-carbon-text disabled:pointer-events-none disabled:opacity-50"
              >
                {t('settings.accentReset')}
              </button>
            </div>
          </div>
        </div>

        {saveError && <p className="text-xs text-statusFail">{t('settings.look.saveFailed', { error: saveError })}</p>}
      </Card>

      {/* Its own card now (jdp: "Stiller Modus in eigene Card") - was the
          last row inside Aussehen above. */}
      <Card className="flex flex-col gap-3">
        <SectionTitle>{t('notifications.quiet')}</SectionTitle>
        <QuietModeToggle />
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle>{t('lang.label')}</SectionTitle>
        {/* standalone: the sidebar used to mount its own copy of this
            component too, sharing lib/langPickerOpen's store by default -
            now that Sprache lives only here, this is moot for the sidebar
            itself, but OnboardingWizard.tsx still mounts a second,
            simultaneous instance of its own, so this stays standalone. */}
        <LanguagePicker
          direction="down"
          standalone
          className="glim-well flex w-fit min-w-[12rem] items-center gap-2.5 px-3 py-2 text-sm text-carbon-text"
        />
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle>{t('settings.theme')}</SectionTitle>
        <Tabs
          label={t('settings.theme')}
          size="sm"
          className="w-fit"
          active={theme}
          onSelect={(id) => setTheme(id as 'dark' | 'light')}
          items={[
            { id: 'dark', label: t('theme.dark'), icon: <IconMoon width={14} height={14} /> },
            { id: 'light', label: t('theme.light'), icon: <IconSun width={14} height={14} /> },
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
