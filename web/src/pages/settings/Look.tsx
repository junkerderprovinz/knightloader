import { useEffect } from 'react';
import { Button, Card, Field, Toggle, segBase, segOn, segOff } from '../../components/ui';
import { useT } from '../../lib/i18n';
import {
  ACCENTS,
  DEFAULT_ACCENT,
  RAINBOW,
  SHAPES,
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
      <Field label={t('settings.shape')} hint={t('settings.shapeHint')}>
        <div className="glim-well flex w-fit items-center gap-0.5 p-1">
          {SHAPES.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => patch({ shape: s })}
              className={`${segBase} px-3 py-1.5 text-xs ${cfg.shape === s ? segOn : segOff}`}
            >
              <span className="flex items-center gap-2">
                {/* The swatch is the setting: it carries the radius it selects,
                    so the choice is visible before it is made. On the filled
                    segment it borrows the ink colour, since an accent swatch on
                    an accent fill is a blank space. */}
                <span
                  className={`h-3.5 w-3.5 ${cfg.shape === s ? 'bg-current' : 'bg-accent'}`}
                  style={{ borderRadius: s === 'round' ? '6px' : s === 'soft' ? '2px' : '0' }}
                />
                {t(`settings.shape.${s}` as never)}
              </span>
            </button>
          ))}
        </div>
      </Field>

      <Field label={t('settings.accent')} hint={t('settings.accentHint')}>
        <div className="flex flex-wrap items-center gap-2">
          {ACCENTS.map((a) => {
            const active = (cfg.accent || DEFAULT_ACCENT).toLowerCase() === a.hex.toLowerCase();
            return (
              <button
                key={a.hex}
                type="button"
                title={a.name}
                aria-label={a.name}
                aria-pressed={active}
                onClick={() => patch({ accent: a.hex })}
                className={`h-7 w-7 rounded-[var(--radius-control)] transition-transform motion-safe:hover:scale-110 ${
                  active ? 'shadow-[0_0_0_2px_var(--carbon-bg),0_0_0_4px_currentColor]' : ''
                }`}
                style={{ backgroundColor: a.hex, color: a.hex }}
              />
            );
          })}
          {/* A free colour beside the presets: the list is a shortcut, not a fence. */}
          <input
            type="color"
            aria-label={t('settings.accent')}
            value={cfg.accent || DEFAULT_ACCENT}
            onChange={(e) => patch({ accent: e.target.value })}
            className="h-7 w-9 cursor-pointer rounded-[var(--radius-control)] bg-carbon-surface2 p-1"
          />
          <Button kind="ghost" className="px-2.5 text-xs" onClick={() => patch({ accent: '' })}>
            {t('settings.accentReset')}
          </Button>
        </div>
      </Field>

      <Field label={t('settings.rainbow')} hint={t('settings.rainbowHint')}>
        <div className="flex flex-col gap-3">
          <Toggle checked={cfg.rainbow} onChange={(v) => patch({ rainbow: v })} label={t('settings.rainbowOn')} />

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

            <div className="flex flex-wrap items-center gap-2">
              {palette.map((hex, i) => (
                <input
                  key={i}
                  type="color"
                  aria-label={`${t('settings.rainbowPalette')} ${i + 1}`}
                  value={hex}
                  onChange={(e) => {
                    const next = palette.slice();
                    next[i] = e.target.value;
                    patch({ rainbowPalette: next });
                  }}
                  className="h-7 w-9 cursor-pointer rounded-[var(--radius-control)] bg-carbon-surface2 p-1"
                />
              ))}
              <Button kind="ghost" className="px-2.5 text-xs" onClick={() => patch({ rainbowPalette: null })}>
                {t('settings.accentReset')}
              </Button>
            </div>
          </div>
        </div>
      </Field>
    </Card>
  );
}
