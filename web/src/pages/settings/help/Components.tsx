import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Card, ErrorCard, InfoBubble, SectionTitle, ToggleRow, type ButtonVerdict } from '../../../components/ui';
import {
  fetchComponentUpdate,
  fetchDeploymentInfo,
  fetchHealth,
  fetchJDStatus,
  fetchMediaTools,
  fetchUpdateCheck,
  fetchUpdateProgress,
  installUpdate,
  retryJDSetup,
  startComponentUpdate,
  type ComponentResult,
  type ComponentUpdate,
  type JDStatus,
  type MediaTool,
  type MediaToolsStatus,
  type UpdateCheck,
  type UpdateProgress,
} from '../../../lib/api';
import { followExternal } from '../../../lib/external';
import { GLIMSTONE_VERSION } from '../../../lib/glimstoneVersion';
import {
  IconClose,
  IconInstall,
  IconRetry,
  IconSync,
  IconUpdateAvailable,
  IconUpToDate,
} from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { openWhatsNew, useWhatsNew } from '../../../lib/useWhatsNew';
import { NewsGlyph } from '../../../components/whatsnew/WhatsNew';
import { useDraft } from '../context';

// The Components card holds what KnightLoader updates or follows, one line
// each: the name, a short status where there is one, a button where there is
// something to do, and at the end the version linked to its release or project
// page. KnightLoader updates itself and keeps its switches under its name. The
// programs it starts, yt-dlp, ffmpeg, Deno, Java and the JDownloader backend,
// share the one switch below them, and Update now at the foot of the card
// updates them all at once and asks after a newer KnightLoader too.

type Translate = ReturnType<typeof useT>['t'];

/** A short status on a row, and behind its (i) what went wrong where something needs doing. */
type Status = { text: string; detail?: string; tone?: 'fail' | 'warn' | 'ok' };

const TONE: Record<NonNullable<Status['tone']>, string> = {
  fail: 'text-statusFail',
  warn: 'text-statusWarn',
  ok: 'text-statusOk',
};

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';

/**
 * releaseTag derives the release tag from a version string the way GlimStone's
 * AboutCard does: it drops semver build metadata and adds a leading 'v' only
 * where the stamp lacks one, since release builds are stamped with it.
 */
export function releaseTag(version: string): string {
  const bare = version.split('+')[0].trim();
  if (!bare) return '';
  return bare.startsWith('v') ? bare : `v${bare}`;
}

/** Only a published release earns a link; a preview or branch stamp has no page. */
const RELEASE_TAG = /^v\d+\.\d+\.\d+$/;

/** releasePage is the release page behind one of this house's versions, or '' for a stamp without one. */
function releasePage(repo: string, version: string): string {
  const tag = releaseTag(version);
  return RELEASE_TAG.test(tag) ? `${repo}/releases/tag/${encodeURIComponent(tag)}` : '';
}

/** What KnightLoader's check came to. */
type Found = 'current' | 'newer' | 'failed';

/** KnightLoader's check, which its row hands the card so Update now asks it too. */
type Check = () => Promise<Found>;
type Register = (check: Check | null) => void;

type Face = { label: string; icon: ReactNode; verdict?: ButtonVerdict };

/** Update now's face for what the last run came to, or for none yet. */
function updateFace(t: Translate, app: Found | null, run: ComponentUpdate | null): Face {
  if (app === null && run === null) return { label: t('settings.info.updateNow'), icon: <IconSync /> };
  const results = Object.values(run?.results ?? {});
  if (app === 'failed' || results.some((r) => r.state === 'failed')) {
    return { label: t('settings.info.updateFailed'), icon: <IconClose />, verdict: 'fail' };
  }
  const waiting = Number(app === 'newer') + results.filter((r) => r.state === 'waits').length;
  if (waiting > 0) return { label: t('settings.info.updatesFound', { n: waiting }), icon: <IconUpdateAvailable />, verdict: 'warn' };
  return { label: t('settings.info.upToDate'), icon: <IconUpToDate />, verdict: 'ok' };
}

/** What a row says about its part of the last Update now, null where there is nothing to say. */
function runStatus(t: Translate, r: ComponentResult | undefined): Status | null {
  switch (r?.state) {
    case 'checking':
      return { text: t('settings.look.updatesChecking') };
    case 'updated':
      return { text: t('settings.info.statusDone'), tone: 'ok' };
    case 'waits':
      return { text: t('settings.info.statusWaits'), detail: t('settings.info.statusWaitsHint'), tone: 'warn' };
    case 'failed':
      return { text: t('settings.info.statusFailed'), detail: t('settings.resolvers.toolsFetchFailed', { error: r.detail ?? '' }), tone: 'fail' };
  }
  return null;
}

/** GlyphButton is one action at the end of a row, its name in its bubble. */
function GlyphButton({
  icon,
  title,
  verdict,
  shake,
  disabled,
  onClick,
}: {
  icon: ReactNode;
  title: string;
  verdict?: ButtonVerdict;
  shake?: number;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      kind="secondary"
      icon={icon}
      title={title}
      aria-label={title}
      verdict={verdict}
      shake={shake}
      disabled={disabled}
      onClick={onClick}
    />
  );
}

/**
 * ComponentRow is one component on one line: its name, a short status, its
 * buttons and its version linked to its release or project page. The version
 * stands last in a column of its own width, so the numbers line up at the
 * right edge. A component's switches stand under its name.
 */
function ComponentRow({
  name,
  version,
  href,
  status,
  actions,
  settings,
  'data-new': mark,
}: {
  name: string;
  version?: string;
  href?: string;
  status?: Status | null;
  actions?: ReactNode;
  settings?: ReactNode;
  'data-new'?: string;
}) {
  const shown = version?.replace(/^v/, '');
  return (
    <section
      data-new={mark}
      className="grid min-h-11 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 py-1.5 @xl:grid-cols-[minmax(0,1fr)_auto_auto_8rem]"
    >
      <h3 className="min-w-0 text-sm text-carbon-text">{name}</h3>
      <span
        role="status"
        className={`order-3 flex items-center text-sm @xl:order-none @xl:justify-end ${status?.tone ? TONE[status.tone] : 'text-carbon-textSub'}`}
      >
        {status && (
          <>
            {status.text}
            {status.detail && <InfoBubble tip={status.detail} />}
          </>
        )}
      </span>
      <span className="order-4 flex items-center justify-end gap-2 @xl:order-none">{actions}</span>
      <span dir="ltr" className="glim-num order-2 truncate text-end text-sm text-carbon-textSub @xl:order-none">
        {shown && href ? (
          <a
            href={href}
            target="_blank"
            rel="noreferrer noopener"
            onClick={followExternal}
            className="underline decoration-carbon-border underline-offset-2 hover:text-carbon-text hover:decoration-current"
          >
            {shown}
          </a>
        ) : (
          shown
        )}
      </span>
      {settings && <div className="order-5 col-span-full flex flex-col gap-3 pt-1">{settings}</div>}
    </section>
  );
}

/**
 * DeploymentError stands in a card whose rows wait for how this instance runs
 * when the server did not say, with a way to ask again.
 */
function DeploymentError({ retry }: { retry: () => void }) {
  const { t } = useT();
  return <ErrorCard nested message={t('common.loadFailed')} retry={retry} retryLabel={t('common.retry')} />;
}

/** How often the card asks how far Update now has come, and for JDownloader while it restarts. */
const RUN_POLL = 1_000;
const JD_POLL = 2_000;
const JD_BACK_WITHIN = 5 * 60_000;
const JD_SETUP_POLL = 1_000;

const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

const errorText = (e: unknown) => String(e).replace(/^(Error|ApiError):\s*/, '');

/**
 * ComponentsCard lists every component on `hue`, one line each, then the one
 * switch for the components and the buttons for updating and for what the
 * updates brought.
 */
export function ComponentsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { data: deployment, failed, reload } = useResource(fetchDeploymentInfo);
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [jd, setJd] = useState<JDStatus | null>(null);
  const [run, setRun] = useState<ComponentUpdate | null>(null);
  const [app, setApp] = useState<Found | null>(null);
  const [running, setRunning] = useState(false);
  const [shake, setShake] = useState(0);
  // Ends every poll once the page is left.
  const mounted = useRef(true);
  const appCheck = useRef<Check | null>(null);
  const register = useCallback<Register>((check) => {
    appCheck.current = check;
  }, []);

  const loadTools = useCallback(async () => {
    try {
      const next = await fetchMediaTools();
      if (mounted.current) setTools(next);
    } catch {
      // The server is unreachable, which the page already shows.
    }
  }, []);

  // Follows the desktop build's own JDownloader while it is being set up, so
  // the download of its Java runtime shows.
  const followJD = useCallback(async (first: JDStatus) => {
    let st = first;
    setJd(st);
    while (mounted.current && !st.configured && st.setup === 'starting') {
      await pause(JD_SETUP_POLL);
      st = await fetchJDStatus().catch(() => st);
      if (mounted.current) setJd(st);
    }
  }, []);

  // JDownloader restarts to install its update and shows its new revision
  // once it answers again.
  const awaitJD = useCallback(async () => {
    const until = Date.now() + JD_BACK_WITHIN;
    while (mounted.current && Date.now() < until) {
      await pause(JD_POLL);
      const now = await fetchJDStatus().catch(() => null);
      if (now?.reachable) {
        if (mounted.current) setJd(now);
        return;
      }
    }
  }, []);

  // Follows Update now until every component has come to something, then
  // reads the versions it left.
  const followRun = useCallback(
    async (first: ComponentUpdate) => {
      let st = first;
      setRun(st);
      while (mounted.current && st.running) {
        await pause(RUN_POLL);
        st = await fetchComponentUpdate().catch(() => st);
        if (mounted.current) setRun(st);
      }
      if (!mounted.current) return st;
      await loadTools();
      if (st.results?.jdownloader?.state === 'updated') await awaitJD();
      else await fetchJDStatus().then(followJD, () => {});
      return st;
    },
    [loadTools, awaitJD, followJD],
  );

  useEffect(() => {
    mounted.current = true;
    void loadTools();
    void fetchJDStatus().then(followJD, () => {});
    // An Update now started before the page opened is still shown.
    void fetchComponentUpdate()
      .then((st) => {
        if (!st.running) return;
        setRunning(true);
        return followRun(st).finally(() => mounted.current && setRunning(false));
      })
      .catch(() => {});
    return () => {
      mounted.current = false;
    };
  }, [loadTools, followJD, followRun]);

  const onRetrySetup = useCallback(async () => {
    try {
      await followJD(await retryJDSetup());
    } catch (e) {
      setJd((prev) => prev && { ...prev, setup: 'failed', detail: errorText(e) });
    }
  }, [followJD]);

  const onUpdate = async () => {
    setRunning(true);
    setApp(null);
    setRun(null);
    const asked = appCheck.current?.();
    let st: ComponentUpdate;
    try {
      st = await followRun(await startComponentUpdate());
    } catch (e) {
      st = { running: false, results: { all: { state: 'failed', detail: errorText(e) } } };
      setRun(st);
    }
    const found = asked ? await asked : null;
    if (!mounted.current) return;
    setApp(found);
    if (found === 'failed' || Object.values(st.results ?? {}).some((r) => r.state === 'failed')) setShake((n) => n + 1);
    setRunning(false);
  };

  const face = updateFace(t, app, run);
  const results = run?.results ?? {};

  return (
    <Card hue={hue} data-new="updates-card" className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.info.componentsIntro')}>{t('settings.info.componentsTitle')}</SectionTitle>
      {/* Wait for the deployment rather than flash the wrong copy. */}
      {failed && <DeploymentError retry={reload} />}
      {deployment && (
        <div data-new="built-in" className="@container flex flex-col divide-y divide-carbon-border/40">
          <AppUpdates deployment={deployment.deployment} fromStore={!!deployment.updatesFromStore} register={register} />
          <YtdlpRow tools={tools} result={results['yt-dlp']} />
          <ToolRow
            name="FFmpeg"
            tool={tools?.ffmpeg}
            href="https://ffmpeg.org/"
            result={results.ffmpeg}
          />
          <ToolRow
            name="Deno"
            tool={tools?.deno}
            href={tools?.deno?.version ? `https://github.com/denoland/deno/releases/tag/v${encodeURIComponent(tools.deno.version)}` : undefined}
            result={results.deno}
          />
          {jd?.runtime && <JavaRow jd={jd} result={results.java} />}
          <JDRow jd={jd} result={results.jdownloader} onRetry={() => void onRetrySetup()} />
          <ComponentRow
            name="GlimStone"
            version={GLIMSTONE_VERSION}
            href={releasePage(GLIMSTONE_URL, GLIMSTONE_VERSION) || GLIMSTONE_URL}
          />
        </div>
      )}
      {deployment && (
        <div data-new="components-switch">
          <ToggleRow
            label={t('settings.info.componentsAuto')}
            hint={t('settings.info.componentsAutoHint')}
            checked={cfg.componentsAutoUpdate}
            onChange={(v) => patch({ componentsAutoUpdate: v })}
          />
        </div>
      )}
      <div className="flex flex-wrap justify-end gap-3">
        {deployment && (
          <>
            <Button
              kind="secondary"
              icon={running ? <IconSync /> : face.icon}
              verdict={running ? undefined : face.verdict}
              shake={shake}
              disabled={running}
              onClick={() => void onUpdate()}
            >
              {running ? t('settings.info.updating') : face.label}
            </Button>
            <span role="status" className="sr-only">
              {face.verdict && !running ? face.label : ''}
            </span>
          </>
        )}
        <NewsButton />
      </div>
    </Card>
  );
}

/**
 * NewsButton opens the one window of what the updates brought, a card per
 * release with its notes and the changes the dots mark. Its figure is the
 * number still unseen, and once all are seen how many the window lists. Where
 * it lists none it offers the release notes alone.
 */
function NewsButton() {
  const { t } = useT();
  const news = useWhatsNew();
  const total = news?.changes.length ?? 0;
  const unseen = news?.unseen.length ?? 0;
  return (
    <Button kind="secondary" icon={<NewsGlyph unseen={unseen > 0} />} data-new="news-button" onClick={openWhatsNew}>
      {total > 0 ? t('whatsnew.newCount', { n: unseen || total }) : t('settings.look.updatesReleaseNotes')}
    </Button>
  );
}

/**
 * jdMissing says what stands in for JDownloader's revision while the desktop
 * build sets up its own, with how far the download of the Java runtime that
 * runs it has come, and why a setup failed.
 */
function jdMissing(t: Translate, lang: string, jd: JDStatus): Status {
  switch (jd.setup) {
    case 'starting': {
      const text = t('settings.resolvers.toolsJdStarting');
      const total = jd.setupTotal ?? 0;
      const done = jd.setupDone ?? 0;
      if (total <= 0 || done >= total) return { text };
      // Floored, so it never says 100 % before the last byte is in.
      const share = Math.floor((done * 100) / total) / 100;
      return { text: `${text} · ${new Intl.NumberFormat(lang, { style: 'percent' }).format(share)}` };
    }
    case 'failed':
      return {
        text: t('settings.resolvers.toolsJdFailed'),
        detail: t('settings.resolvers.toolsJdFailedHint', { error: jd.detail ?? '' }),
        tone: 'fail',
      };
    default:
      return { text: t('settings.resolvers.toolsNotConfigured') };
  }
}

/** How often the row asks how far an update it started has come. */
const INSTALL_POLL = 500;

const installing = (p: UpdateProgress | null) => p?.state === 'downloading' || p?.state === 'starting';

/** installLine is how far a download the row started has come, or why it failed. */
function installLine(t: Translate, p: UpdateProgress): string {
  if (p.state === 'failed') return t('settings.look.updatesInstallFailed', { error: p.error ?? '' });
  const percent = p.total && p.total > 0 ? Math.floor(((p.done ?? 0) * 100) / p.total) : 0;
  return t('settings.look.updatesDownloading', { percent: String(percent) });
}

/** appStatus is what KnightLoader's row says about a check or an update, null while nothing is to say. */
function appStatus(
  t: Translate,
  s: { checking: boolean; progress: UpdateProgress | null; ready?: string; available: boolean; check: UpdateCheck | null },
): Status | null {
  if (s.checking) return { text: t('settings.look.updatesChecking') };
  const p = s.progress;
  if (p?.state === 'failed') return { text: t('settings.info.statusFailed'), detail: installLine(t, p), tone: 'fail' };
  if (p?.state === 'downloading') return { text: installLine(t, p) };
  if (p?.state === 'starting' || p?.state === 'started') return { text: t('settings.look.updatesInstalling') };
  // A downloaded version outranks the offer of it: it only waits for a restart.
  if (s.ready) return { text: t('settings.info.statusReady'), tone: 'ok' };
  if (s.available) return { text: t('settings.look.updatesAvailable', { version: s.check?.latest ?? '' }), tone: 'warn' };
  if (s.check && !s.check.checked) return { text: t('settings.info.checkFailed'), tone: 'fail' };
  return null;
}

/**
 * AppUpdates checks GitHub for a newer release on request. A container is
 * updated by its container manager, so both switches are greyed out there and
 * a check points at the release. The desktop build updates itself in the
 * background (desktop/updates.go), and a check there also tells whether a
 * downloaded version waits for the next start. Once a check found a newer
 * one, Install fetches it at once: the copy installed for all users starts
 * the new installer, any other copy puts it in place for the next start. A
 * Flatpak is the desktop build, but Flatpak installs its updates, so its row
 * behaves like a container's and says how it is updated.
 */
function AppUpdates({ deployment, fromStore, register }: { deployment: string; fromStore: boolean; register: Register }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const [version, setVersion] = useState('');
  const [check, setCheck] = useState<UpdateCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [installShake, setInstallShake] = useState(0);
  // Ends the polling once the page is left.
  const mounted = useRef(true);
  const updatesItself = deployment === 'desktop' && !fromStore;

  useEffect(() => {
    void fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);

  const onCheck = useCallback(async (): Promise<Found> => {
    setChecking(true);
    let next: UpdateCheck;
    try {
      next = await fetchUpdateCheck();
    } catch {
      next = { checked: false, available: false, current: '' };
    }
    setCheck(next);
    setChecking(false);
    return !next.checked ? 'failed' : next.available ? 'newer' : 'current';
  }, []);

  // Follows an update until it is past downloading. The installer closes the
  // program, so a poll that finds nobody keeps the last answer.
  const follow = useCallback(async (first: UpdateProgress) => {
    let p = first;
    setProgress(p);
    while (mounted.current && installing(p)) {
      await pause(INSTALL_POLL);
      p = await fetchUpdateProgress().catch(() => p);
      if (mounted.current) setProgress(p);
    }
    if (p.state === 'failed') setInstallShake((n) => n + 1);
  }, []);

  // An update started before the page opened is still shown.
  useEffect(() => {
    mounted.current = true;
    if (updatesItself) {
      void fetchUpdateProgress()
        .then((p) => (p.state ? follow(p) : undefined))
        .catch(() => {});
    }
    return () => {
      mounted.current = false;
    };
  }, [updatesItself, follow]);

  const onInstall = useCallback(async () => {
    try {
      await follow(await installUpdate());
    } catch (e) {
      setProgress({ state: 'failed', error: errorText(e) });
      setInstallShake((n) => n + 1);
    }
  }, [follow]);

  const ready = check?.ready ?? (progress?.state === 'ready' ? progress.version : undefined);
  // A container or a Flatpak asks once on opening while its switch is on,
  // never before the stored switch has resolved and not on every settings
  // edit. The desktop build that updates itself checks on its own.
  useEffect(() => {
    if (!updatesItself && cfg.autoUpdateCheck) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.autoUpdateCheck]);

  const available = !!check?.checked && check.available;
  // Not while an update is being put in place.
  const handed = installing(progress) ? null : onCheck;
  useEffect(() => {
    register(handed);
    return () => register(null);
  }, [register, handed]);
  // No stamp yet, or 'dev' for an untagged build: the working title stands in.
  const stamped = version !== '' && version !== 'dev';

  return (
    <ComponentRow
      name="KnightLoader"
      version={stamped ? version : t('nav.workingTitle')}
      href={stamped ? releasePage(REPO_URL, version) || REPO_URL : undefined}
      status={appStatus(t, { checking, progress, ready, available, check })}
      actions={
        <>
          {updatesItself && available && !ready && progress?.state !== 'started' && (
            <GlyphButton
              icon={<IconInstall />}
              title={installing(progress) ? t('settings.look.updatesInstalling') : t('settings.look.updatesInstall')}
              shake={installShake}
              disabled={checking || installing(progress)}
              onClick={() => void onInstall()}
            />
          )}
        </>
      }
      settings={
        <>
          {/* The desktop build checks on its own and has no use for this
              switch. */}
          {!updatesItself && (
            <ToggleRow
              label={t('settings.look.updatesAuto')}
              hint={t('settings.look.updatesAutoCheckHint')}
              checked={cfg.autoUpdateCheck}
              onChange={(v) => patch({ autoUpdateCheck: v })}
            />
          )}
          {/* A container and a Flatpak cannot replace themselves, so there
              the row is disabled and says why. */}
          <ToggleRow
            label={t('settings.look.updatesAutoUpdate')}
            hint={
              fromStore
                ? t('settings.look.updatesAutoUpdateStoreHint')
                : updatesItself
                  ? t('settings.look.updatesAutoUpdateHint')
                  : t('settings.look.updatesAutoUpdateContainerHint')
            }
            checked={updatesItself && cfg.autoUpdate}
            disabled={!updatesItself}
            // A key press still reaches a greyed-out switch, and a container keeps
            // the stored value as it is.
            onChange={(v) => updatesItself && patch({ autoUpdate: v })}
          />
        </>
      }
    />
  );
}

/**
 * compareVersions orders two yt-dlp date versions numerically, like
 * mediatools.CompareVersions. It returns null when either is not a run of
 * numbers, so an unordered pair is never shown as equal.
 */
function compareVersions(a: string, b: string): number | null {
  const parse = (v: string): number[] | null => {
    const t = v.trim().replace(/^v/, '');
    if (!t) return null;
    const parts = t.split('.').map((p) => (/^\d+$/.test(p) ? Number(p) : NaN));
    return parts.some(Number.isNaN) ? null : parts;
  };
  const pa = parse(a);
  const pb = parse(b);
  if (!pa || !pb) return null;
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0;
    const y = pb[i] ?? 0;
    if (x !== y) return x > y ? 1 : -1;
  }
  return 0;
}

/**
 * YtdlpRow is yt-dlp, which moves faster than KnightLoader's releases. Beside
 * what Update now came to it warns when the fetched copy has fallen behind
 * the image's own (Status.shadowed), which happens while the switch is off,
 * and when the fetched copy does not start and the other yt-dlp took over.
 */
function YtdlpRow({ tools, result }: { tools: MediaToolsStatus | null; result?: ComponentResult }) {
  const { t } = useT();
  const ytdlp = tools?.ytdlp;
  const shadowed = tools?.shadowed;
  const managedRunning = ytdlp?.source === 'managed';
  // A fetched copy on record that is not what runs, for example deleted by a
  // virus scanner. The server has already gone back to the other yt-dlp.
  const managedBroken = Boolean(tools?.managed) && !managedRunning;
  const managedIsOlder =
    managedRunning &&
    Boolean(shadowed?.version) &&
    Boolean(ytdlp?.version) &&
    compareVersions(ytdlp?.version ?? '', shadowed?.version ?? '') === -1;

  // What Update now came to wins over a standing warning.
  let status = runStatus(t, result);
  if (!status && managedIsOlder)
    status = {
      text: t('settings.info.statusNote'),
      detail: t('settings.resolvers.toolsShadowedOlder', { current: ytdlp?.version ?? '', system: shadowed?.version ?? '' }),
      tone: 'warn',
    };
  else if (!status && managedBroken)
    status = {
      text: t('settings.resolvers.toolsFellBack'),
      detail: t('settings.resolvers.toolsManagedBroken', { path: tools?.managedPath ?? '' }),
      tone: 'warn',
    };
  else if (!status && tools && !ytdlp?.found) status = { text: t('settings.resolvers.toolsNotFound'), tone: 'fail' };

  return (
    <ComponentRow
      name="yt-dlp"
      version={ytdlp?.found ? ytdlp.version : undefined}
      href={ytdlp?.version ? `https://github.com/yt-dlp/yt-dlp/releases/tag/${encodeURIComponent(ytdlp.version)}` : undefined}
      status={status}
    />
  );
}

/** ToolRow is ffmpeg or Deno: its version, what Update now came to, and whether there is one at all. */
function ToolRow({
  name,
  tool,
  href,
  result,
}: {
  name: string;
  tool?: MediaTool;
  href?: string;
  result?: ComponentResult;
}) {
  const { t } = useT();
  let status = runStatus(t, result);
  if (!status && tool && !tool.found) status = { text: t('settings.resolvers.toolsNotFound'), tone: 'fail' };
  return <ComponentRow name={name} version={tool?.found ? tool.version : undefined} href={href} status={status} />;
}

/**
 * JavaRow is the Java runtime the desktop build fetched for its own
 * JDownloader, with a newer one the server found and JDownloader does not run
 * on yet.
 */
function JavaRow({ jd, result }: { jd: JDStatus; result?: ComponentResult }) {
  const { t } = useT();
  let status = runStatus(t, result);
  if (!status && jd.runtimeUpdate) status = { text: t('settings.look.updatesAvailable', { version: jd.runtimeUpdate }), tone: 'warn' };
  return <ComponentRow name="Java" version={jd.runtime?.version} href="https://adoptium.net/temurin/" status={status} />;
}

/**
 * JDRow is the headless JDownloader, which KnightLoader talks to. Update now
 * has it install a waiting update, after which the card waits until it answers
 * again and shows its new revision.
 */
function JDRow({ jd, result, onRetry }: { jd: JDStatus | null; result?: ComponentResult; onRetry: () => void }) {
  const { t, lang } = useT();
  // While the desktop build sets up its own JDownloader, or after that failed,
  // the row says so.
  let status = jd && !jd.configured ? jdMissing(t, lang, jd) : null;
  if (!status) status = runStatus(t, result);
  if (!status && jd && !jd.reachable) status = { text: t('health.state.failed'), tone: 'fail' };
  else if (!status && jd?.updateWaits) status = { text: t('settings.info.updatesFound', { n: 1 }), tone: 'warn' };
  return (
    <ComponentRow
      name={t('health.part.jd')}
      version={jd?.configured && jd.reachable && jd.version ? String(jd.version) : undefined}
      href="https://jdownloader.org/"
      status={status}
      data-new="jd-tool"
      actions={jd?.setup === 'failed' && <GlyphButton icon={<IconRetry />} title={t('common.retry')} onClick={onRetry} />}
    />
  );
}
