import { useEffect, useRef, useState } from 'react';
import { Button, Card, FieldGroup, InfoBubble, SectionTitle, Swatch, SwatchRow, Toggle } from '../../components/ui';
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
  // further to do here; this page just gives the sidebar's own two controls
  // a second, equally reachable home (jdp: "Auch Sprachen und Hell/dunkel
  // kommen als eigene cards in den aussehen tab").
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
  // search box, so dragging the palette's hex field sends one PATCH once
  // it settles rather than one per keystroke; the very first run is
  // skipped, since that one fires on mount with the value just loaded
  // from the server, not a change to save.
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

  return (
    <div className="flex flex-col gap-5">
      <Card className="flex flex-col gap-5">
      {/* FieldGroup, not Field: a Field is a `<label>`, and a label around three
          tabs hands its clicks to the first of them — clicking the word
          "Corners" set the app back to round. See ui.tsx. */}
      <FieldGroup label={t('settings.shape')} hint={t('settings.shapeHint')}>
        {/* The same strip the filters and the settings pages use. A corner
            picker is a tab set with three entries and a preview for a glyph,
            and building it out of its own buttons is how the two drifted apart
            the last time. */}
        <Tabs
          label={t('settings.shape')}
          size="sm"
          className="w-fit"
          active={cfg.shape}
          onSelect={(id) => patch({ shape: id as Shape })}
          items={SHAPES.map((s) => ({
            id: s,
            label: t(`settings.shape.${s}` as never),
            // The glyph is the setting: it carries the radius it selects, so
            // the choice is visible before it is made. It is an OUTLINE, not a
            // filled square — a coloured dot beside each of three labels is
            // three dots of decoration, and the accent is supposed to mean
            // activity. Drawn in the label's own colour, it says what it has to
            // say with the shape alone.
            icon: (
              <span
                aria-hidden
                className="h-3.5 w-3.5 shrink-0 border-[1.5px] border-current"
                style={{ borderRadius: s === 'round' ? '6px' : s === 'soft' ? '2px' : '0' }}
              />
            ),
          }))}
        />
      </FieldGroup>

      <FieldGroup label={t('settings.accent')} hint={t('settings.accentHint')}>
        <SwatchRow
          label={t('settings.accent')}
          after={
            <Button kind="ghost" className="px-2.5 text-xs" onClick={() => patch({ accent: '' })}>
              {t('settings.accentReset')}
            </Button>
          }
        >
          {ACCENTS.map((a) => (
            <Swatch
              key={a.hex}
              color={a.hex}
              label={a.name}
              selected={live(cfg.accent) === a.hex.toLowerCase()}
              onPick={() => patch({ accent: a.hex })}
            />
          ))}
          {/* A free colour beside the presets: the list is a shortcut, not a
              fence. It wears the ring whenever the live accent is not one of
              the five, so exactly one swatch in the row is always marked and
              the free colour is never a square that looks switched off while
              being the one in use. */}
          <Swatch
            color={cfg.accent || DEFAULT_ACCENT}
            label={t('settings.accent')}
            selected={!ACCENTS.some((a) => a.hex.toLowerCase() === live(cfg.accent))}
            onColor={(hex) => patch({ accent: hex })}
          />
        </SwatchRow>
      </FieldGroup>

      {/* Three switches in one column, all starting at the same edge.
          The master used to carry a second label ("use the palette") beside the
          section's own name, which said the same thing twice, and the two
          sub-switches were indented under it. The indent was meant to show they
          belong to the mode — but they are already dimmed when it is off, which
          says that better, and the step left three switch tracks starting at two
          different x positions. Flush, the column reads down in one line. */}
      <div className="flex flex-col gap-3">
        <span className="flex items-center text-xs text-carbon-textSub">
          {t('settings.rainbow')}
          <InfoBubble tip={t('settings.rainbowHint')} />
        </span>

        {/* No visible words: the heading directly above already says Rainbow,
            and a switch captioned "use the palette" under it said the same
            decision twice. The label survives as the accessible name. */}
        <Toggle
          checked={cfg.rainbow}
          onChange={(v) => patch({ rainbow: v })}
          label={t('settings.rainbowOn')}
          hideLabel
        />

        {/* Disabled rather than hidden: a control that vanishes teaches nobody
            what the mode can do. */}
        <div
          className={`flex flex-col gap-3 transition-opacity ${
            cfg.rainbow ? '' : 'pointer-events-none opacity-40'
          }`}
        >
          <Toggle
            checked={cfg.rainbowReactive}
            onChange={(v) => patch({ rainbowReactive: v })}
            label={t('settings.rainbowReactive')}
          />
          <Toggle
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
            label={t('settings.rainbowRotate')}
          />

          {/* The very same row as the accent above it, because it is the very
              same job: pick colours. It was a row of raw <input type="color">
              boxes with the browser's chrome around each one, four rows under
              a row of clean squares — two controls, one idea, on one page. */}
          <SwatchRow
            label={t('settings.rainbowPalette')}
            after={
              <Button kind="ghost" className="px-2.5 text-xs" onClick={() => patch({ rainbowPalette: null })}>
                {t('settings.accentReset')}
              </Button>
            }
          >
            {palette.map((hex, i) => (
              <Swatch
                key={i}
                color={hex}
                label={`${t('settings.rainbowPalette')} ${i + 1}`}
                onColor={(v) => {
                  const next = palette.slice();
                  next[i] = v;
                  patch({ rainbowPalette: next });
                }}
              />
            ))}
          </SwatchRow>
        </div>
      </div>

      {saveError && <p className="text-xs text-statusFail">{t('settings.look.saveFailed', { error: saveError })}</p>}

      {/* Not appearance, but there is no better-fitting page registered yet
          (settings/registry.tsx's PAGES/ICONS only draw a tab the server's
          own GET /api/features already lists - adding a dedicated
          Notifications page is a real feature, not a fix). This used to be
          a small panel pinned over the bottom-right corner of every route,
          permanently, in English, regardless of which page was open; an
          ordinary row here is reachable exactly once, on purpose, like
          every other switch on this page. No extra heading above it - the
          toggle's own label already says "Quiet mode", and the Rainbow
          section above already established that a heading repeating what
          the switch beneath it says is the same decision said twice. */}
      <QuietModeToggle />
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle>{t('lang.label')}</SectionTitle>
        {/* standalone: the sidebar mounts its own copy of this component at
            the same time, sharing lib/langPickerOpen's store by default -
            without this, opening either one opens both (the exact bug
            LanguagePicker.tsx's own doc comment describes for
            OnboardingWizard, which is why that instance carries the same
            prop). */}
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
