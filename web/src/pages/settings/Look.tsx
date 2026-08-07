import { useEffect } from 'react';
import { Button, Card, FieldGroup, InfoBubble, Swatch, SwatchRow, Toggle } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
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
  const { cfg, patch } = useDraft();

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

  return (
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
          // Bare, not in a well. Everywhere else the strip sits on the page
          // ground and the well is what groups it; here it sits inside a card,
          // where a second fill reads as an inset panel bolted onto the page —
          // and one raised surface is rule 1. The segments carry themselves:
          // the chosen one is filled, the rest are quiet text.
          //
          // Tabs has no `bare` variant to ask for, so the well is overridden
          // from here — see the report.
          className="w-fit bg-transparent! p-0!"
          active={cfg.shape}
          onSelect={(id) => patch({ shape: id as Shape })}
          items={SHAPES.map((s) => ({
            id: s,
            label: t(`settings.shape.${s}` as never),
            // The glyph is the setting: it carries the radius it selects, so
            // the choice is visible before it is made. On the filled tab it
            // borrows the ink colour, since an accent swatch on an accent fill
            // is a blank space.
            icon: (
              <span
                aria-hidden
                className={`h-3.5 w-3.5 shrink-0 ${cfg.shape === s ? 'bg-current' : 'bg-accent'}`}
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

      {/* Not a Field: a Field stacks its label above its control, which is right
          for a picker that cannot name itself and wrong for a switch that
          already carries its own words. Stacked, this row read as two lines and
          two labels for one decision — the section's name, and then a switch
          under it saying something else. Beside each other they read as one
          sentence: Rainbow — use the palette. */}
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <span className="flex items-center text-xs text-carbon-textSub">
            {t('settings.rainbow')}
            <InfoBubble tip={t('settings.rainbowHint')} />
          </span>
          <Toggle checked={cfg.rainbow} onChange={(v) => patch({ rainbow: v })} label={t('settings.rainbowOn')} />
        </div>

        {/* The sub-switches belong to the mode, so they are indented under it
            and disabled rather than hidden: a control that vanishes teaches
            nobody what the mode can do. */}
        <div
          className={`flex flex-col gap-3 ps-6 transition-opacity ${
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
    </Card>
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
