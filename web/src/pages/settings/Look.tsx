import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import { Button, Card, ErrorCard, InfoBubble, Modal, SectionTitle, Toggle, ToggleRow } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { LanguagePicker } from '../../components/LanguagePicker';
import {
  type DeploymentInfo,
  BACKUP_DOWNLOAD_URL,
  fetchDeploymentInfo,
  fetchUpdateCheck,
  installUpdate,
  requestQuit,
  requestRestart,
  uploadRestore,
  type UpdateCheck as UpdateCheckT,
} from '../../lib/api';
import { IconDownloads, IconMoon, IconRetry, IconSignOut, IconSun, IconUpload } from '../../lib/icons';
import { QuietModeToggle, useToast } from '../../lib/toast';
import { getTheme, onThemeChange, setTheme } from '../../lib/theme';
import { useT } from '../../lib/i18n';
import { useResource } from '../../lib/useResource';
import {
  ACCENTS,
  DEFAULT_ACCENT,
  RAINBOW,
  SHAPES,
  type MotionIntensity,
  type Shape,
  applyAccent,
  applyMotionIntensity,
  applyRainbow,
  applyShape,
  cacheAppearance,
  cacheMotionIntensity,
  hueVars,
  rainbowAt,
  rainbowFromSettings,
  readCachedMotionIntensity,
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

  // Motion intensity is client-only too, same reasoning as shape/accent/
  // rainbow just below: a single-operator tool has no second viewer who
  // needs to agree on how much animation there is.
  const [motion, setMotion] = useState<MotionIntensity>(readCachedMotionIntensity);

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
    applyMotionIntensity(motion);
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
    motion,
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
    <div className="flex flex-col gap-10">
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

      {/* Motion intensity - the settings-UI half of a separate parallel piece
          of work (the keyframes/data-motion mechanism lives in index.css and
          lib/appearance.ts). hue=8 reuses the slot the Backup/Restore merge
          below just freed, rather than renumbering every other card's own
          fixed position in the sequence for one new row. */}
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={8} hint={t('settings.motion.hint')}>
          {t('settings.motion.title')}
        </SectionTitle>
        <Tabs
          label={t('settings.motion.title')}
          variant="well"
          className="w-fit"
          active={motion}
          onSelect={(id) => {
            const next = id as MotionIntensity;
            setMotion(next);
            applyMotionIntensity(next);
            cacheMotionIntensity(next);
          }}
          items={[
            { id: 'off', label: t('settings.motion.off') },
            { id: 'subtle', label: t('settings.motion.subtle') },
            { id: 'full', label: t('settings.motion.full') },
          ]}
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
        <div className="flex flex-wrap items-center justify-end gap-3">
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
            <div className="flex flex-wrap items-center justify-end gap-2">
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
                    // The label wrapping this input carries no text content
                    // (its `title` is a hover-only tooltip), so without an
                    // accessible name of its own the actually-focusable
                    // element here - this input, not the label - announced
                    // nothing to a screen reader.
                    aria-label={`${t('settings.rainbowPalette')} ${i + 1}`}
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

      <UpdateCard />
      <SystemCards />
    </div>
  );
}

/**
 * Overview, quit/restart, backup and restore — formerly their own "System"
 * tab (build-plan.md's Wave 10/10D), merged in here (jdp, 2026-08-24: "Alles
 * was im Systemtab ist in den Allgemein-Tab mergen") since none of the four
 * needed a dedicated tab of their own any more than Updates above already
 * didn't. Self-contained and independently loading, same as UpdateCard: a
 * slow or failed /api/deployment fetch delays or drops only this section,
 * never the appearance controls above it.
 */
function SystemCards() {
  const { t } = useT();
  const { data, failed, loading, reload } = useResource<DeploymentInfo>(fetchDeploymentInfo);

  const [confirmAction, setConfirmAction] = useState<'quit' | 'restart' | null>(null);
  const [acting, setActing] = useState(false);
  const [actionError, setActionError] = useState('');
  const [shuttingDown, setShuttingDown] = useState(false);

  const fileInput = useRef<HTMLInputElement>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreError, setRestoreError] = useState('');
  const [restoreStatus, setRestoreStatus] = useState('');

  async function confirmLifecycle() {
    if (!confirmAction) return;
    setActing(true);
    setActionError('');
    try {
      const res = confirmAction === 'quit' ? await requestQuit() : await requestRestart();
      setShuttingDown(true);
      void res;
    } catch (e) {
      setActionError(t('settings.system.actionFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setActing(false);
      setConfirmAction(null);
    }
  }

  async function confirmRestore() {
    if (!pendingFile) return;
    setRestoring(true);
    setRestoreError('');
    try {
      const res = await uploadRestore(pendingFile);
      setRestoreStatus(res.status);
      if (res.restarting) setShuttingDown(true);
    } catch (e) {
      setRestoreError(t('settings.system.restoreFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setRestoring(false);
      setPendingFile(null);
    }
  }

  // Same choice as UpdateCard just above: render nothing while this
  // section's own fetch is in flight rather than a separate loading card
  // popping into an otherwise-instant page.
  if (loading) return null;
  if (failed || !data) {
    return <ErrorCard message={t('settings.system.loadFailed')} retry={reload} retryLabel="↻" />;
  }

  if (shuttingDown) {
    return (
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={6}>{t('settings.system.shuttingDownTitle')}</SectionTitle>
        <p className="text-sm text-carbon-text">{t('settings.system.shuttingDown')}</p>
      </Card>
    );
  }

  return (
    <>
      {/* The former standalone "Übersicht" card (deployment badge + the
          same intro sentence this whole section already opens with once)
          removed outright (jdp, 2026-08-24: "Übersicht card entfernen") -
          the one fact worth keeping, what quit/restart actually do on THIS
          deployment, still shows right below, now as the card's own hint
          rather than data.note's raw English. */}
      <Card className="flex flex-col gap-3">
        {/* What quit/restart actually do here used to print data.note
            straight from the wire - internal/api/routes_lifecycle.go's own
            deploymentInfo() hardcodes that sentence in English regardless of
            the browser's locale, so it never went through this app's i18n at
            all. Told through the two translated lifecycleNote* keys instead,
            keyed off the same data.deployment this page already has -
            the unavailable reason still wins when there is one, since a
            control that cannot be used at all is the more urgent fact. */}
        <SectionTitle
          hue={6}
          hint={
            !data.canQuit || !data.canRestart
              ? t('settings.system.unavailable')
              : t(data.deployment === 'desktop' ? 'settings.system.lifecycleNoteDesktop' : 'settings.system.lifecycleNoteContainer')
          }
        >
          {t('settings.system.lifecycleTitle')}
        </SectionTitle>
        <div className="flex flex-wrap items-center gap-3">
          {/* hue on both, and both `kind="primary"` now (jdp: "Der beenden
              button soll nicht extra anders eingefärbt sein") - hue already
              overrides kind's own colour entirely (see Button's own doc
              comment in ui.tsx), so the two read identically styled, the
              only difference their label/icon and which confirm-modal opens. */}
          <Button
            hue={6}
            kind="primary"
            icon={<IconSignOut width={16} height={16} />}
            disabled={!data.canQuit || acting}
            onClick={() => setConfirmAction('quit')}
          >
            {t('settings.system.quit')}
          </Button>
          <Button
            hue={6}
            kind="primary"
            icon={<IconRetry width={16} height={16} />}
            disabled={!data.canRestart || acting}
            onClick={() => setConfirmAction('restart')}
          >
            {t('settings.system.restart')}
          </Button>
        </div>
        {actionError && <span className="text-sm text-statusFail">{actionError}</span>}
      </Card>

      {/* Backup and Restore, one card (jdp, 2026-08-24: "Sicherung und
          wiederherstellungscard in eine zusammenfassen") - two former
          standalone Cards (hue 7 and hue 8) merged into hue 7 alone; nothing
          after this needed hue 8 any more than the Motion card above needed a
          fresh one of its own. */}
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={7} hint={t('settings.system.backupRestoreHint')}>
          {t('settings.system.backupRestoreTitle')}
        </SectionTitle>
        <input
          ref={fileInput}
          type="file"
          accept="application/zip,.zip"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            // Cleared straight away, or picking the same file twice in a row
            // raises no change event and a second restore attempt after a
            // failed one silently does nothing - the same reason Rules.tsx's
            // import input already does this.
            e.target.value = '';
            if (f) setPendingFile(f);
          }}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={7}
            kind="secondary"
            icon={<IconDownloads width={16} height={16} />}
            onClick={() => {
              window.location.href = BACKUP_DOWNLOAD_URL;
            }}
          >
            {t('settings.system.backupButton')}
          </Button>
          <Button
            hue={7}
            kind="secondary"
            icon={<IconUpload width={16} height={16} />}
            onClick={() => fileInput.current?.click()}
            disabled={restoring}
          >
            {t('settings.system.restoreButton')}
          </Button>
        </div>
        {restoreError && <span className="text-sm text-statusFail">{restoreError}</span>}
        {restoreStatus && !restoreError && (
          <span className="text-sm text-statusOk">{t('settings.system.restoreStaged', { status: restoreStatus })}</span>
        )}
      </Card>

      {confirmAction && (
        <Modal
          title={t(confirmAction === 'quit' ? 'settings.system.quitConfirmTitle' : 'settings.system.restartConfirmTitle')}
          onClose={() => (acting ? undefined : setConfirmAction(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setConfirmAction(null)} disabled={acting}>
                {t('settings.system.confirmCancel')}
              </Button>
              <Button kind="danger" onClick={() => void confirmLifecycle()} disabled={acting}>
                {acting ? t('settings.system.acting') : t('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('settings.system.quitConfirmBody', { note: data.note })}</p>
        </Modal>
      )}

      {pendingFile && (
        <Modal
          title={t('settings.system.restoreConfirmTitle')}
          onClose={() => (restoring ? undefined : setPendingFile(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setPendingFile(null)} disabled={restoring}>
                {t('settings.system.confirmCancel')}
              </Button>
              <Button kind="danger" onClick={() => void confirmRestore()} disabled={restoring}>
                {restoring ? t('settings.system.restoring') : t('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('settings.system.restoreConfirmBody', { name: pendingFile.name })}</p>
        </Modal>
      )}
    </>
  );
}

/**
 * Both deployments now (jdp, 2026-08-23: "#19 bauen"; 2026-08-24: "warum
 * machen wir da nicht irgendwo ein toggle um auto update zu aktivieren? Am
 * besten im allgemein-Tab", which first landed desktop-only; then, once a
 * container user hit the same hard `deployment !== 'desktop'` gate this
 * card used to have and asked where the card and toggle had gone: shown on
 * BOTH builds, because checking GitHub and telling someone a newer release
 * exists is equally harmless either way - a GET request and a notification,
 * not a self-update). The manual "Check for updates" button always works on
 * both; the toggle additionally makes this card check once as soon as it
 * mounts with the toggle already on, without asking - the setting
 * round-trips to the server the same as every other field on this page,
 * autosaved by the shared draft. Off by default: an outbound call to GitHub
 * every time this page is opened is an opt-in, not something a fresh
 * install does before being asked.
 *
 * What still differs by deployment is only what "update available" tells
 * you to do about it by default - a container cannot replace itself from
 * the inside, so its "update available" state points at the release
 * instead (routes_features.go's updaterReason) - never whether a check can
 * run, and on desktop specifically not whether an install can now be
 * triggered from here either: internal/update's Download/Apply/Relaunch
 * (jdp, 2026-08-24: "kannst du bei updates auch ein toggle machen für
 * updates automatisch installieren?", after weighing the security tradeoff
 * explicitly and choosing the real thing over the safer "check only"
 * default this card shipped with first) do the actual download, atomic
 * swap and relaunch; this card only offers the toggle and the manual
 * button, same "settings page does not own the mechanism" split every
 * other control here already follows.
 */
function UpdateCard() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { toast } = useToast();
  const [deployment, setDeployment] = useState<string | null>(null);
  const [check, setCheck] = useState<UpdateCheckT | null>(null);
  const [checking, setChecking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [installError, setInstallError] = useState('');
  // Once true, stays true: a successful POST /api/system/update-install
  // means the process is already on its way out to relaunch, so there is
  // no "installing" state to return to and nothing further this card
  // should let a click do.
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
    setInstallError('');
    setInstalling(true);
    try {
      await installUpdate();
      setInstalled(true);
    } catch (e) {
      // A network error here is genuinely ambiguous (routes_lifecycle.go's
      // own comment: the process may already be exiting to relaunch by the
      // time this rejects) - but showing a plausible failure and letting
      // someone press the button again is still better than a spinner that
      // never resolves if the install truly did fail before ever swapping
      // anything.
      setInstallError(String(e).replace(/^(Error|ApiError):\s*/, ''));
      setInstalling(false);
    }
  }, []);

  // Auto-check once, right after the toggle's own current value arrives -
  // not on every render. Both deployments reach this now; the check itself
  // (internal/update.Check) has never cared which one is asking.
  useEffect(() => {
    if (cfg.autoUpdateCheck) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fires once
    // when deployment/autoUpdateCheck first resolve, not on every cfg change.
  }, [deployment, cfg.autoUpdateCheck]);

  // Auto-install, once, the moment a check this page itself ran (manual or
  // automatic - both flow through the same `check` state) finds something
  // available and the toggle is on. Deliberately NOT re-checked whenever
  // cfg.autoUpdateInstall flips true on its own - toggling it on does not
  // reach back into a `check` result from before the toggle existed in
  // this session, matching autoUpdateCheck's own "acts on what happens
  // from here, not on stale state" behaviour above.
  useEffect(() => {
    if (deployment === 'desktop' && cfg.autoUpdateInstall && check?.checked && check.available && !installing && !installed) {
      toast(t('settings.look.updatesAutoInstalling', { version: check.latest ?? '' }), 'info');
      void onInstall();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reacts only to a fresh check result
  }, [check]);

  // Wait for deployment to resolve rather than guessing - one flash of the
  // wrong copy (desktop's install-oriented link, briefly, on a container) is
  // exactly the overpromise this card exists to avoid.
  if (deployment === null) return null;
  const isDesktop = deployment === 'desktop';
  const canInstallNow = isDesktop && !installed && check?.checked && check.available;

  return (
    <Card className="flex flex-col gap-3">
      <SectionTitle hue={5} hint={t('settings.look.updatesHint')}>
        {t('settings.look.updatesTitle')}
      </SectionTitle>
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm text-carbon-text">{t('settings.look.updatesAuto')}</span>
        <Toggle checked={cfg.autoUpdateCheck} onChange={(v) => patch({ autoUpdateCheck: v })} label={t('settings.look.updatesAuto')} hideLabel />
      </div>
      {/* Always shown, same reasoning as the Auto-Check row above and the
          card's own doc comment: shown on both, only what it explains
          differs. A container cannot ever install an update from here
          (routes_lifecycle.go's own POST /api/system/update-install stays
          501 there), so this reads-only-disables + explains rather than
          disappearing - jdp hit exactly the "disappeared instead of
          disabled" version of this on the auto-check toggle earlier this
          same campaign (see updater doc comment above) and it was the
          wrong call there too. */}
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
          <Button kind="primary" onClick={() => void onInstall()} disabled={installing}>
            {installing ? t('settings.look.updatesInstalling') : t('settings.look.updatesInstallNow')}
          </Button>
        )}
        {check && !check.checked && <span className="text-sm text-statusFail">{t('settings.look.updatesFailed')}</span>}
        {check && check.checked && !check.available && (
          <span className="text-sm text-statusOk">{t('settings.look.updatesCurrent', { version: check.current })}</span>
        )}
        {check && check.checked && check.available && check.url && (
          <a href={check.url} target="_blank" rel="noopener noreferrer" className="text-sm font-medium text-accent hover:underline">
            {t(isDesktop ? 'settings.look.updatesAvailable' : 'settings.look.updatesAvailableContainer', { version: check.latest ?? '' })}
          </a>
        )}
      </div>
      {installed && <p className="text-sm text-statusOk">{t('settings.look.updatesInstalled')}</p>}
      {installError && <p className="text-sm text-statusFail">{t('settings.look.updatesInstallFailed', { error: installError })}</p>}
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
