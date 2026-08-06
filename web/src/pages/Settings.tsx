import { useEffect, useState } from 'react';
import {
  type AuthState,
  type Settings,
  fetchAuth,
  fetchSettings,
  saveSettings,
  setPassword,
} from '../lib/api';
import { useResource } from '../lib/useResource';
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
  type Shape,
} from '../lib/appearance';
import { useT } from '../lib/i18n';
import {
  PageHeader,
  Card,
  Button,
  Field,
  NumberInput,
  TextInput,
  TextArea,
  Toggle,
  SectionTitle,
  LoadingCard,
  ErrorCard,
  segBase,
  segOn,
  segOff,
} from '../components/ui';

export function SettingsPage() {
  const { t } = useT();
  const { data: cfg, setData: setCfg, loading, failed, reload } = useResource<Settings>(fetchSettings);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  // What the swatch row edits: the saved palette when it is complete, the
  // built-in hues otherwise. Either way the row shows eight editable colours,
  // so "reset" and "never customised" look the same and behave the same.
  const palette =
    cfg?.rainbowPalette && cfg.rainbowPalette.length === RAINBOW.length ? cfg.rainbowPalette : RAINBOW;

  // The look follows the settings as soon as they arrive, and again on every
  // pick below, so what is on screen is always what is selected. It is applied
  // here for the preview and at the app root for everything else — see
  // Layout.tsx; a look that only existed while this page was mounted would be
  // no look at all.
  useEffect(() => {
    if (!cfg) return;
    const rainbow = rainbowFromSettings(cfg);
    applyShape(cfg.shape);
    applyAccent(cfg.accent);
    applyRainbow(rainbow);
    cacheAppearance(cfg.shape, cfg.accent, rainbow);
  }, [
    cfg?.shape,
    cfg?.accent,
    cfg?.rainbow,
    cfg?.rainbowReactive,
    cfg?.rainbowRotate,
    cfg?.rainbowSeed,
    // The palette is an array, so the effect has to depend on its contents;
    // the identity changes on every keystroke of the colour picker anyway.
    cfg?.rainbowPalette?.join(),
  ]);

  async function onSave() {
    if (!cfg) return;
    setError('');
    try {
      const applied = await saveSettings(cfg);
      setCfg(applied);
      setSaved(true);
      setTimeout(() => setSaved(false), 1800);
    } catch (e) {
      setError(String(e));
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />

      {loading && <LoadingCard label={t('common.loading')} />}
      {failed && <ErrorCard message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />}

      {cfg && (
        <>
          <SectionTitle>{t('settings.sectionDownloads')}</SectionTitle>
          <Card className="flex flex-col gap-5">
            <Field
              label={t('settings.downloadDir')}
              hint={`${t('settings.downloadDirHint')} ${t('settings.pathVars')}`}
            >
              <TextInput
                dir="ltr"
                value={cfg.downloadDir}
                placeholder="/downloads"
                spellCheck={false}
                onChange={(e) => setCfg({ ...cfg, downloadDir: e.target.value })}
              />
            </Field>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <Field label={t('settings.maxConcurrent')}>
                <NumberInput
                  value={cfg.maxConcurrent}
                  min={1}
                  max={64}
                  onValue={(v) => setCfg({ ...cfg, maxConcurrent: v })}
                />
              </Field>
              <Field label={t('settings.maxPerHost')}>
                <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => setCfg({ ...cfg, maxPerHost: v })} />
              </Field>
              <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
                <NumberInput
                  value={Math.round(cfg.speedLimit / 1024)}
                  min={0}
                  step={256}
                  onValue={(v) => setCfg({ ...cfg, speedLimit: Math.max(0, v) * 1024 })}
                />
              </Field>
            </div>
            <Field label={t('settings.maxRetries')} hint={t('settings.maxRetriesHint')}>
              <NumberInput
                value={cfg.maxRetries}
                min={0}
                max={20}
                onValue={(v) => setCfg({ ...cfg, maxRetries: v })}
              />
            </Field>
            <div className="flex flex-col gap-3">
              <Toggle
                checked={cfg.subfolderByPackage}
                onChange={(v) => setCfg({ ...cfg, subfolderByPackage: v })}
                label={t('settings.subfolderByPackage')}
              />
              <Toggle
                checked={cfg.autoStart}
                onChange={(v) => setCfg({ ...cfg, autoStart: v })}
                label={t('settings.autoStart')}
              />
              <Toggle
                checked={cfg.crawl}
                onChange={(v) => setCfg({ ...cfg, crawl: v })}
                label={t('settings.crawl')}
              />
              <Toggle
                checked={cfg.verifyChecksums}
                onChange={(v) => setCfg({ ...cfg, verifyChecksums: v })}
                label={t('settings.verifyChecksums')}
              />
            </div>
            <Field label={t('settings.watchDir')} hint={t('settings.watchDirHint')}>
              <TextInput
                dir="ltr"
                value={cfg.watchDir}
                placeholder="/watch"
                spellCheck={false}
                onChange={(e) => setCfg({ ...cfg, watchDir: e.target.value })}
              />
            </Field>
          </Card>

          <SectionTitle>{t('settings.sectionArchives')}</SectionTitle>
          <Card className="flex flex-col gap-5">
            <div className="flex flex-col gap-3">
              <Toggle
                checked={cfg.extract}
                onChange={(v) => setCfg({ ...cfg, extract: v })}
                label={t('settings.extract')}
              />
              <Toggle
                checked={cfg.deleteArchive}
                onChange={(v) => setCfg({ ...cfg, deleteArchive: v })}
                label={t('settings.deleteArchive')}
              />
            </div>
            <Field label={t('settings.archivePasswords')} hint={t('settings.archivePasswordsHint')}>
              <TextArea
                rows={4}
                spellCheck={false}
                value={(cfg.archivePasswords ?? []).join('\n')}
                onChange={(e) =>
                  setCfg({ ...cfg, archivePasswords: e.target.value.split('\n').filter((p) => p.trim() !== '') })
                }
              />
            </Field>
          </Card>

          <div className="flex items-center gap-3">
            <Button onClick={onSave}>{t('settings.save')}</Button>
            {saved && <span className="text-statusOk text-sm">{t('settings.saved')}</span>}
            {error && <span className="text-statusFail text-sm">{error}</span>}
          </div>

          <SectionTitle>{t('settings.sectionLook')}</SectionTitle>
          <Card className="flex flex-col gap-5">
            <Field label={t('settings.shape')} hint={t('settings.shapeHint')}>
              <div className="glim-well flex w-fit items-center gap-0.5 p-1">
                {SHAPES.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => setCfg({ ...cfg, shape: s })}
                    className={`${segBase} px-3 py-1.5 text-xs ${cfg.shape === s ? segOn : segOff}`}
                  >
                    <span className="flex items-center gap-2">
                      {/* The swatch is the setting: it carries the radius it
                          selects, so the choice is visible before it is made.
                          On the filled segment it borrows the ink colour, since
                          an accent swatch on an accent fill is a blank space. */}
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
                      onClick={() => setCfg({ ...cfg, accent: a.hex })}
                      className={`h-7 w-7 rounded-[var(--radius-control)] transition-transform motion-safe:hover:scale-110 ${
                        active ? 'shadow-[0_0_0_2px_var(--carbon-bg),0_0_0_4px_currentColor]' : ''
                      }`}
                      style={{ backgroundColor: a.hex, color: a.hex }}
                    />
                  );
                })}
                {/* A free colour beside the presets: the list is a shortcut,
                    not a fence. */}
                <input
                  type="color"
                  aria-label={t('settings.accent')}
                  value={cfg.accent || DEFAULT_ACCENT}
                  onChange={(e) => setCfg({ ...cfg, accent: e.target.value })}
                  className="h-7 w-9 cursor-pointer rounded-[var(--radius-control)] bg-carbon-surface2 p-1"
                />
                <Button kind="ghost" className="px-2.5 text-xs" onClick={() => setCfg({ ...cfg, accent: '' })}>
                  {t('settings.accentReset')}
                </Button>
              </div>
            </Field>

            <Field label={t('settings.rainbow')} hint={t('settings.rainbowHint')}>
              <div className="flex flex-col gap-3">
                <Toggle
                  checked={cfg.rainbow}
                  onChange={(v) => setCfg({ ...cfg, rainbow: v })}
                  label={t('settings.rainbowOn')}
                />

                {/* The sub-switches belong to the mode, so they are indented
                    under it and disabled rather than hidden: a control that
                    vanishes teaches nobody what the mode can do. */}
                <div
                  className={`flex flex-col gap-3 ps-6 transition-opacity ${
                    cfg.rainbow ? '' : 'pointer-events-none opacity-40'
                  }`}
                >
                  <Toggle
                    checked={cfg.rainbowReactive}
                    onChange={(v) => setCfg({ ...cfg, rainbowReactive: v })}
                    label={t('settings.rainbowReactive')}
                  />
                  <Toggle
                    checked={cfg.rainbowRotate}
                    onChange={(v) =>
                      // Turning rotation on draws a fresh offset, so the switch
                      // does something visible instead of re-applying the
                      // rotation the palette already had.
                      setCfg({
                        ...cfg,
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
                          setCfg({ ...cfg, rainbowPalette: next });
                        }}
                        className="h-7 w-9 cursor-pointer rounded-[var(--radius-control)] bg-carbon-surface2 p-1"
                      />
                    ))}
                    <Button
                      kind="ghost"
                      className="px-2.5 text-xs"
                      onClick={() => setCfg({ ...cfg, rainbowPalette: null })}
                    >
                      {t('settings.accentReset')}
                    </Button>
                  </div>
                </div>
              </div>
            </Field>
          </Card>

          <SectionTitle>{t('settings.sectionSecurity')}</SectionTitle>
          <PasswordCard />
        </>
      )}
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button rather than
// with the rest of the settings: a password is not a preference you change by
// accident while adjusting the speed limit.
function PasswordCard() {
  const { t } = useT();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetchAuth().then(setAuth).catch(() => setAuth(null));
  }, []);

  async function onApply() {
    setError('');
    try {
      setAuth(await setPassword(current, next));
      setCurrent('');
      setNext('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
    } catch (e) {
      setError(String(e).replace(/^Error:\s*/, ''));
    }
  }

  const locked = auth?.enabled ?? false;

  return (
    <Card className="flex flex-col gap-5">
      <p className={`text-sm ${locked ? 'text-statusOk' : 'text-carbon-textSub'}`}>
        {locked ? t('settings.lockOn') : t('settings.lockOff')}
      </p>
      {locked && (
        <Field label={t('settings.passwordCurrent')}>
          <TextInput type="password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </Field>
      )}
      <Field label={t('settings.passwordNew')} hint={t('settings.passwordHint')}>
        <TextInput type="password" value={next} onChange={(e) => setNext(e.target.value)} />
      </Field>
      <div className="flex items-center gap-3">
        <Button kind="secondary" onClick={onApply} disabled={locked ? current === '' : next === ''}>
          {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
        </Button>
        {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
        {error && <span className="text-statusFail text-sm">{error}</span>}
      </div>
    </Card>
  );
}
