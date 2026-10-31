import { useCallback, useEffect, useRef, useState, type ComponentProps, type ReactNode } from 'react';
import {
  Button,
  Card,
  ErrorCard,
  HintBubble,
  InfoBubble,
  LinkBadge,
  SectionTitle,
  ToggleRow,
  type ButtonVerdict,
} from '../../../components/ui';
import {
  ApiError,
  fetchDeploymentInfo,
  fetchHealth,
  fetchJDStatus,
  fetchJDUpdate,
  fetchMediaTools,
  fetchUpdateCheck,
  fetchUpdateProgress,
  fetchYtdlpLatest,
  installUpdate,
  retryJDSetup,
  revertYtdlp,
  updateJD,
  updateYtdlp,
  type BuiltModule,
  type DeploymentInfo,
  type JDStatus,
  type MediaToolsStatus,
  type UpdateCheck,
  type UpdateProgress,
  type YtdlpLatest,
} from '../../../lib/api';
import { IconClose, IconRefresh, IconUpdate, IconUpdateAvailable, IconUpToDate } from '../../../lib/icons';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { useDraft } from '../context';
import { SubCard } from '../controls';
import { jdMissing } from '../resolvers/MediaToolsCard';

// Two cards. Updates holds what can be updated on its own, KnightLoader,
// yt-dlp and the JDownloader backend, each a sub-card with its version and a
// check button that shows its answer on itself. yt-dlp moves faster than
// KnightLoader's releases, which is why it is updated here at all, and
// JDownloader is a program of its own beside KnightLoader. The other card
// lists the rest, one row each with its version and where it comes from: they
// come with the image, the computer or the build, so there is nothing to
// press.

type Translate = ReturnType<typeof useT>['t'];

type Hint = ComponentProps<typeof HintBubble>['hint'];

type Face = { label: string; icon: ReactNode; verdict?: ButtonVerdict };

/** The check button's face for an answer that has come back, or for none yet. */
function checkFace(t: Translate, checked: boolean | null, newer: boolean, known = true): Face {
  if (checked === null || !known) return { label: t('settings.look.updatesCheck'), icon: <IconUpdate /> };
  if (!checked) return { label: t('settings.info.checkFailed'), icon: <IconClose />, verdict: 'fail' };
  if (newer) return { label: t('settings.info.updatesFound', { n: 1 }), icon: <IconUpdateAvailable />, verdict: 'warn' };
  return { label: t('settings.info.upToDate'), icon: <IconUpToDate />, verdict: 'ok' };
}

/** CheckButton asks and shows the answer on itself, and says it once more to a screen reader. */
function CheckButton({
  face,
  checking,
  disabled,
  shake,
  onCheck,
}: {
  face: Face;
  checking: boolean;
  disabled?: boolean;
  shake: number;
  onCheck: () => void;
}) {
  const { t } = useT();
  return (
    <>
      <Button
        kind="secondary"
        icon={checking ? <IconUpdate /> : face.icon}
        verdict={checking ? undefined : face.verdict}
        shake={shake}
        disabled={checking || disabled}
        onClick={onCheck}
      >
        {checking ? t('settings.look.updatesChecking') : face.label}
      </Button>
      <span role="status" className="sr-only">
        {face.verdict && !checking ? face.label : ''}
      </span>
    </>
  );
}

/**
 * ToolRow is one built-in part: its name with what it does behind an (i), its
 * version and where it comes from, in columns every row of the card shares.
 */
function ToolRow({ name, sub, version, source }: { name: string; sub?: string; version: ReactNode; source: string }) {
  return (
    <section className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-0.5 @xl:grid-cols-[minmax(0,1fr)_9rem_15rem]">
        <h3 className="flex min-w-0 items-center gap-1.5 text-sm text-carbon-text">
          {name}
          <HintBubble hint={sub} />
        </h3>
        <span dir="ltr" className="glim-num truncate text-end text-sm text-carbon-textSub @xl:text-start">
          {version}
        </span>
        <span className="col-span-2 text-subline text-carbon-textMuted @xl:col-span-1">{source}</span>
      </div>
    </section>
  );
}

/**
 * ToolCard is a part KnightLoader updates, as a sub-card: its name with what
 * it does behind an (i), its version at the heading's end, and its update
 * controls under them.
 */
function ToolCard({
  name,
  sub,
  version,
  children,
}: {
  name: string;
  sub?: Hint;
  version: ReactNode;
  children?: ReactNode;
}) {
  return (
    <SubCard
      label={name}
      hint={sub}
      className="gap-3"
      right={
        <span dir="ltr" className="glim-num text-sm text-carbon-textSub">
          {version}
        </span>
      }
    >
      {children}
    </SubCard>
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

/** Where a program on the server's path comes from: the image in a container, the computer otherwise. */
const hostSource = (t: Translate, deployment: string) =>
  t(deployment === 'desktop' ? 'settings.look.toolsSource.system' : 'settings.look.toolsSource.image');

/** A tool fetched into the data folder says so; anything else is the host's. */
const toolSource = (t: Translate, deployment: string, tool?: { source?: string }) =>
  tool?.source === 'managed' ? t('settings.resolvers.toolsFrom.managed') : hostSource(t, deployment);

/**
 * UpdatesCard holds the updates of KnightLoader, yt-dlp and the JDownloader
 * backend, each a sub-card. `children` is a further group of the card, such as
 * quit and restart.
 */
export function UpdatesCard({ hue, children }: { hue: number; children?: ReactNode }) {
  const { t } = useT();
  const { data: deployment, failed, reload } = useResource(fetchDeploymentInfo);

  return (
    <Card hue={hue} data-new="updates-card" className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.look.updatesHint')}>{t('settings.look.updatesTitle')}</SectionTitle>
      {/* Wait for the deployment rather than flash the wrong copy. */}
      {failed && <DeploymentError retry={reload} />}
      {deployment && (
        <>
          <AppUpdates deployment={deployment.deployment} />
          <YtdlpUpdates />
          <JDUpdates />
        </>
      )}
      {children}
    </Card>
  );
}

/**
 * BuiltInCard lists every part KnightLoader brings and does not update on its
 * own, each with its version and where it comes from.
 */
export function BuiltInCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { data: deployment, failed, reload } = useResource(fetchDeploymentInfo);
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [jd, setJd] = useState<JDStatus | null>(null);

  useEffect(() => {
    void fetchMediaTools()
      .then(setTools)
      .catch(() => {});
    void fetchJDStatus()
      .then(setJd)
      .catch(() => {});
  }, []);

  const found = (tool?: { found: boolean; version?: string }) =>
    tools && (tool?.found ? tool.version : t('settings.resolvers.toolsNotFound'));

  return (
    <Card hue={hue} data-new="built-in" className="flex flex-col gap-6">
      <SectionTitle hint={t('settings.look.builtInHint')}>{t('settings.look.builtInTitle')}</SectionTitle>
      {failed && <DeploymentError retry={reload} />}
      {deployment && (
        <div className="@container flex flex-col divide-y divide-carbon-border/40">
          <ToolRow
            name="ffmpeg"
            sub={t('settings.info.ffmpegSub')}
            version={found(tools?.ffmpeg)}
            source={toolSource(t, deployment.deployment, tools?.ffmpeg)}
          />
          <ToolRow
            name="ffprobe"
            sub={t('settings.look.tool.ffprobe')}
            version={found(tools?.ffprobe)}
            source={toolSource(t, deployment.deployment, tools?.ffprobe)}
          />
          {/* The desktop build fetches it for JDownloader and renews it with
              its own updates. */}
          {jd?.runtime && (
            <ToolRow
              name={`Java (${jd.runtime.vendor})`}
              sub={t('settings.look.tool.jre')}
              version={jd.runtime.version}
              source={t('settings.resolvers.toolsFrom.managed')}
            />
          )}
          {(deployment.engines ?? []).map((m) => (
            <EngineRow key={m.path} module={m} />
          ))}
        </div>
      )}
    </Card>
  );
}

/** How often the sub-card asks how far an update it started has come. */
const INSTALL_POLL = 500;

const installing = (p: UpdateProgress | null) => p?.state === 'downloading' || p?.state === 'starting';

/** installLine is what an update the sub-card started has come to, '' before one. */
function installLine(t: Translate, p: UpdateProgress): string {
  switch (p.state) {
    case 'downloading': {
      const percent = p.total && p.total > 0 ? Math.floor(((p.done ?? 0) * 100) / p.total) : 0;
      return t('settings.look.updatesDownloading', { percent: String(percent) });
    }
    case 'starting':
      return t('settings.look.updatesStarting');
    case 'started':
      return t('settings.look.updatesStarted');
    case 'ready':
      return t('settings.look.updatesReady', { version: p.version ?? '' });
    case 'failed':
      return t('settings.look.updatesInstallFailed', { error: p.error ?? '' });
    default:
      return '';
  }
}

/**
 * AppUpdates checks GitHub for a newer release on request. A container is
 * updated by its container manager, so both switches are greyed out there and
 * a check points at the release. The desktop build updates itself in the
 * background (desktop/updates.go), and a check there also tells whether a
 * downloaded version waits for the next start. Once a check found a newer
 * one, Install fetches it at once: the copy installed for all users starts
 * the new installer, any other copy puts it in place for the next start.
 */
function AppUpdates({ deployment }: { deployment: string }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const [version, setVersion] = useState('');
  const [check, setCheck] = useState<UpdateCheck | null>(null);
  const [checking, setChecking] = useState(false);
  // The failure counter of the check button, so a repeated failure shakes it again.
  const [shake, setShake] = useState(0);
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [installShake, setInstallShake] = useState(0);
  // Ends the polling once the page is left.
  const mounted = useRef(true);
  const isDesktop = deployment === 'desktop';

  useEffect(() => {
    void fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);

  const onCheck = useCallback(async () => {
    setChecking(true);
    let next: UpdateCheck;
    try {
      next = await fetchUpdateCheck();
    } catch {
      next = { checked: false, available: false, current: '' };
    }
    setCheck(next);
    if (!next.checked) setShake((n) => n + 1);
    setChecking(false);
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
    if (isDesktop) {
      void fetchUpdateProgress()
        .then((p) => (p.state ? follow(p) : undefined))
        .catch(() => {});
    }
    return () => {
      mounted.current = false;
    };
  }, [isDesktop, follow]);

  const onInstall = useCallback(async () => {
    try {
      await follow(await installUpdate());
    } catch (e) {
      setProgress({ state: 'failed', error: errorText(e) });
      setInstallShake((n) => n + 1);
    }
  }, [follow]);

  const ready = check?.ready ?? (progress?.state === 'ready' ? progress.version : undefined);
  const available = !!check?.checked && check.available;
  const face = checkFace(t, check ? check.checked : null, available);

  return (
    <ToolCard name="KnightLoader" version={!version || version === 'dev' ? t('nav.workingTitle') : version.replace(/^v/, '')}>
      {/* The desktop build checks on its own and has no use for this switch.
          A container keeps the stored value as it is, so a key press on the
          disabled switch changes nothing either. */}
      {!isDesktop && (
        <ToggleRow
          label={t('settings.look.updatesAuto')}
          hint={t('settings.look.updatesAutoContainerHint')}
          checked={false}
          disabled
          onChange={() => {}}
        />
      )}
      {/* Shown on both builds; a container cannot replace itself, so there the
          row is disabled and says why. */}
      <ToggleRow
        label={t('settings.look.updatesAutoUpdate')}
        hint={isDesktop ? t('settings.look.updatesAutoUpdateHint') : t('settings.look.updatesAutoUpdateContainerHint')}
        checked={isDesktop && cfg.autoUpdate}
        disabled={!isDesktop}
        onChange={(v) => patch({ autoUpdate: v })}
      />
      {/* A downloaded version outranks the offer of it: it only waits for a
          restart. */}
      {!checking && (ready || available) && (
        <p className="flex flex-wrap items-center gap-x-3 gap-y-2 text-sm text-carbon-textSub">
          <span className="inline-flex items-center">
            {ready
              ? t('settings.look.updatesReady', { version: ready })
              : t('settings.look.updatesAvailable', { version: check?.latest ?? '' })}
            {!ready && !isDesktop && <InfoBubble tip={t('settings.look.updatesContainerHint')} />}
          </span>
          {available && check?.url && <LinkBadge href={check.url} title={t('settings.look.updatesReleaseNotes')} />}
        </p>
      )}
      {progress && progress.state !== 'ready' && installLine(t, progress) && (
        <p
          role="status"
          className={`text-sm ${progress.state === 'failed' ? 'text-statusFail' : 'text-carbon-textSub'}`}
        >
          {installLine(t, progress)}
        </p>
      )}
      {/* Check, install: the forward action sits last. */}
      <div className="flex flex-wrap items-center justify-end gap-3">
        <CheckButton face={face} checking={checking} disabled={installing(progress)} shake={shake} onCheck={() => void onCheck()} />
        {isDesktop && available && !ready && progress?.state !== 'started' && (
          <Button
            kind="secondary"
            icon={<IconUpdate />}
            shake={installShake}
            disabled={checking || installing(progress)}
            onClick={() => void onInstall()}
          >
            {installing(progress) ? t('settings.look.updatesInstalling') : t('settings.look.updatesInstall')}
          </Button>
        )}
      </div>
    </ToolCard>
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

const errorText = (e: unknown) => String(e).replace(/^(Error|ApiError):\s*/, '');

/**
 * YtdlpUpdates fetches a newer yt-dlp than the image carries when a site
 * breaks between image builds, by a button press or once a day while
 * ytdlpAutoUpdate is on. With the daily update off a fetched copy never moves
 * again, so the sub-card warns when it has fallen behind the image's own
 * (Status.shadowed).
 */
function YtdlpUpdates() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [latest, setLatest] = useState<YtdlpLatest | null>(null);
  const [checking, setChecking] = useState(false);
  const [shake, setShake] = useState(0);
  const [fetching, setFetching] = useState(false);
  const [reverting, setReverting] = useState(false);
  // Separate outcomes, so a later check does not wipe a failed fetch.
  const [fetched, setFetched] = useState('');
  const [failed, setFailed] = useState('');
  // The path running again after a revert, '' when none is left, null before one.
  const [reverted, setReverted] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setTools(await fetchMediaTools());
    } catch {
      // The server is unreachable, which the page already shows.
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const onCheck = useCallback(async () => {
    setChecking(true);
    setFetched('');
    setFailed('');
    setReverted(null);
    let found: YtdlpLatest;
    try {
      found = await fetchYtdlpLatest();
    } catch (e) {
      // A GitHub refusal still answers 200, so this is the request failing.
      found = { checked: false, compare: 'unknown', detail: errorText(e) };
    }
    setLatest(found);
    if (!found.checked) setShake((n) => n + 1);
    setChecking(false);
  }, []);

  // Once the stored switch has resolved, never before and not on every
  // settings edit.
  useEffect(() => {
    if (cfg.ytdlpVersionCheck) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.ytdlpVersionCheck]);

  const onFetch = useCallback(async () => {
    setFetching(true);
    setFetched('');
    setFailed('');
    setReverted(null);
    try {
      const done = await updateYtdlp();
      setFetched(done.version);
      await load();
      // The check result describes the release this just installed.
      setLatest(null);
    } catch (e) {
      // The server's sentence names the asset and the failure.
      setFailed(errorText(e));
    } finally {
      setFetching(false);
    }
  }, [load]);

  const onRevert = useCallback(async () => {
    setReverting(true);
    setFetched('');
    setFailed('');
    setReverted(null);
    try {
      const after = await revertYtdlp();
      setTools(after);
      setReverted(after.ytdlp.found ? (after.ytdlp.path ?? '') : '');
      setLatest(null);
    } catch (e) {
      setFailed(errorText(e));
    } finally {
      setReverting(false);
    }
  }, []);

  const ytdlp = tools?.ytdlp;
  const managed = tools?.managed;
  const shadowed = tools?.shadowed;
  const managedRunning = ytdlp?.source === 'managed';
  // A fetched copy on record that is not what runs, for example deleted by a
  // virus scanner.
  const managedBroken = Boolean(managed) && !managedRunning;
  // The fetched copy has fallen behind the image's own.
  const managedIsOlder =
    managedRunning &&
    Boolean(shadowed?.version) &&
    Boolean(ytdlp?.version) &&
    compareVersions(ytdlp?.version ?? '', shadowed?.version ?? '') === -1;

  const busy = checking || fetching || reverting;
  // An order the server could not tell stays without a colour.
  const face = checkFace(t, latest ? latest.checked : null, latest?.compare === 'newer', latest?.compare !== 'unknown');
  const line = latest ? checkLine(t, latest, ytdlp?.version ?? '') : '';

  const found = (tool?: { found: boolean; version?: string }) =>
    tools && (tool?.found ? tool.version : t('settings.resolvers.toolsNotFound'));

  return (
    <ToolCard
      name="yt-dlp"
      sub={[t('settings.info.ytdlpSub'), t('settings.resolvers.toolsActionsHint')]}
      version={found(ytdlp)}
    >
      {managedRunning && shadowed && !managedIsOlder && (
        <p className="text-xs text-carbon-textSub">
          {t('settings.resolvers.toolsShadowed', {
            path: shadowed.path ?? '',
            version: shadowed.version || t('settings.resolvers.toolsNotFound'),
          })}
        </p>
      )}
      {managedIsOlder && (
        <p className="text-xs text-statusWarn">
          {t('settings.resolvers.toolsShadowedOlder', {
            current: ytdlp?.version ?? '',
            system: shadowed?.version ?? '',
            path: shadowed?.path ?? '',
          })}
        </p>
      )}
      {managedBroken && (
        <p className="text-xs text-statusWarn">
          {t('settings.resolvers.toolsManagedBroken', { path: tools?.managedPath ?? '' })}
        </p>
      )}

      <ToggleRow
        checked={cfg.ytdlpVersionCheck}
        onChange={(v) => patch({ ytdlpVersionCheck: v })}
        label={t('settings.look.updatesAuto')}
        hint={t('settings.resolvers.toolsAutoCheckHint')}
      />
      <ToggleRow
        checked={cfg.ytdlpAutoUpdate}
        onChange={(v) => patch({ ytdlpAutoUpdate: v })}
        label={t('settings.look.updatesAutoUpdate')}
        hint={t('settings.resolvers.toolsAutoUpdateHint')}
      />

      <div className="flex flex-col gap-1.5">
        {/* What the last action came to, above the buttons until the next one.
            The fetch came after the check, so its outcome wins. */}
        {failed && <p className="text-sm text-statusFail">{t('settings.resolvers.toolsFetchFailed', { error: failed })}</p>}
        {!failed && fetched && (
          <p className="text-sm text-statusOk">{t('settings.resolvers.toolsFetched', { version: fetched })}</p>
        )}
        {!failed && !fetched && reverted !== null && (
          <p className="text-sm text-statusOk">
            {reverted
              ? t('settings.resolvers.toolsReverted', { path: reverted })
              : t('settings.resolvers.toolsRevertedNone')}
          </p>
        )}
        {!failed && !fetched && reverted === null && line && <p className="text-sm text-carbon-textSub">{line}</p>}

        {/* Revert, check, fetch: the forward action sits last. The JSX order
            sets it, so the row mirrors in right-to-left languages. */}
        <div className="flex flex-wrap items-center justify-end gap-3">
          {managed && (
            <Button kind="ghost" icon={<IconRefresh />} disabled={busy} onClick={() => void onRevert()}>
              {reverting ? t('settings.resolvers.toolsReverting') : t('settings.resolvers.toolsRevert')}
            </Button>
          )}
          <CheckButton face={face} checking={checking} disabled={busy} shake={shake} onCheck={() => void onCheck()} />
          {/* Only after a check found a tag, so nobody fetches a version they
              were not shown. */}
          {latest?.checked && latest.tag && (
            <Button kind="secondary" icon={<IconUpdate />} disabled={busy} onClick={() => void onFetch()}>
              {fetching ? t('settings.resolvers.toolsFetching') : t('settings.resolvers.toolsFetch')}
            </Button>
          )}
        </div>
      </div>
    </ToolCard>
  );
}

/** How often, and how long, the sub-card asks for JDownloader while it restarts for an update. */
const JD_POLL = 2_000;
const JD_BACK_WITHIN = 5 * 60_000;
const JD_SETUP_POLL = 1_000;

const JD_REFUSALS: Record<string, TranslationKey> = {
  jdUnreachable: 'health.remedy.jd.unreachable',
  jdUpdateCheckFailed: 'settings.look.jdCheckFailed',
};

/** jdRefusal says why the server could not ask JDownloader, in the reader's language when it knows the code. */
function jdRefusal(t: Translate, e: unknown): string {
  const key = e instanceof ApiError && e.code ? JD_REFUSALS[e.code] : undefined;
  return key ? t(key) : errorText(e);
}

const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * JDUpdates is the headless JDownloader, which KnightLoader talks to. It also
 * updates itself; here a check asks it whether an update waits, and the update
 * has it install that and restart, after which the sub-card waits until it
 * answers again and shows its new revision. While jdAutoUpdate is on, the
 * server does the same once a day when none of JD's downloads runs.
 */
function JDUpdates() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const [jd, setJd] = useState<JDStatus | null>(null);
  // Null before a check, then whether JDownloader answered it.
  const [checked, setChecked] = useState<boolean | null>(null);
  const [available, setAvailable] = useState(false);
  const [checking, setChecking] = useState(false);
  const [shake, setShake] = useState(0);
  const [restarting, setRestarting] = useState(false);
  const [back, setBack] = useState(false);
  const [failed, setFailed] = useState('');
  // Ends the wait for the restart once the page is left.
  const mounted = useRef(true);

  // Follows the desktop build's own JDownloader while it is being set up, so
  // the download of its Java runtime shows.
  const follow = useCallback(async (first: JDStatus) => {
    let st = first;
    setJd(st);
    while (mounted.current && !st.configured && st.setup === 'starting') {
      await pause(JD_SETUP_POLL);
      st = await fetchJDStatus().catch(() => st);
      if (mounted.current) setJd(st);
    }
  }, []);

  useEffect(() => {
    mounted.current = true;
    void fetchJDStatus()
      .then(follow)
      .catch(() => {});
    return () => {
      mounted.current = false;
    };
  }, [follow]);

  const onRetry = useCallback(async () => {
    try {
      await follow(await retryJDSetup());
    } catch (e) {
      setJd((prev) => prev && { ...prev, setup: 'failed', detail: errorText(e) });
    }
  }, [follow]);

  const onCheck = useCallback(async () => {
    setChecking(true);
    setFailed('');
    setBack(false);
    try {
      const found = await fetchJDUpdate();
      setChecked(found.checked);
      setAvailable(found.available);
    } catch (e) {
      setChecked(false);
      setAvailable(false);
      setFailed(jdRefusal(t, e));
      setShake((n) => n + 1);
    }
    setChecking(false);
  }, [t]);

  const onUpdate = useCallback(async () => {
    setFailed('');
    setBack(false);
    try {
      await updateJD();
    } catch (e) {
      setFailed(jdRefusal(t, e));
      return;
    }
    setRestarting(true);
    const before = jd?.version;
    const until = Date.now() + JD_BACK_WITHIN;
    let wentDown = false;
    while (mounted.current && Date.now() < until) {
      await pause(JD_POLL);
      const now = await fetchJDStatus().catch(() => null);
      if (!now?.reachable) {
        wentDown = true;
        continue;
      }
      // JDownloader can still answer for a moment before it goes down, so the
      // old revision only counts once it has been away.
      if (wentDown || now.version !== before) {
        setJd(now);
        setChecked(null);
        setAvailable(false);
        setRestarting(false);
        setBack(true);
        return;
      }
    }
    if (!mounted.current) return;
    setRestarting(false);
    setFailed(t('settings.look.jdNotBack'));
  }, [jd, t]);

  let version: ReactNode = null;
  // While the desktop build sets up its own JDownloader, or after that failed,
  // the sub-card says so.
  const setupNote = jd && !jd.configured && jd.setup ? jdMissing(t, jd) : null;
  if (setupNote) version = setupNote[0];
  else if (jd && !jd.configured) version = t('settings.selftest.status.skipped');
  else if (jd && !jd.reachable) version = t('health.state.failed');
  else if (jd) version = jd.version;
  const busy = checking || restarting;

  return (
    <ToolCard
      name={t('health.part.jd')}
      sub={[t('settings.look.tool.jd'), t('settings.look.jdUpdatesItself')]}
      version={version}
    >
      {setupNote && (
        <p role="status" className={`text-sm ${jd?.setup === 'failed' ? 'text-statusFail' : 'text-carbon-textSub'}`}>
          {setupNote[1]}
        </p>
      )}
      {jd?.setup === 'failed' && (
        <div className="flex justify-end">
          <Button kind="secondary" icon={<IconRefresh />} onClick={() => void onRetry()}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      {/* Without a JDownloader there is nothing to ask. */}
      {jd?.configured && (
        <div data-new="jd-auto-update">
          <ToggleRow
            checked={cfg.jdAutoUpdate}
            onChange={(v) => patch({ jdAutoUpdate: v })}
            label={t('settings.look.updatesAutoUpdate')}
            hint={t('settings.look.jdAutoUpdateHint')}
          />
        </div>
      )}
      {jd?.configured && (
        <div className="flex flex-col gap-1.5">
          {failed && <p className="text-sm text-statusFail">{failed}</p>}
          {restarting && <p className="text-sm text-carbon-textSub">{t('settings.look.jdRestarting')}</p>}
          {back && <p className="text-sm text-statusOk">{t('settings.look.jdBack', { version: jd.version ?? '' })}</p>}
          <div data-new="jd-update" className="flex flex-wrap items-center justify-end gap-3">
            <CheckButton
              face={checkFace(t, checked, available)}
              checking={checking}
              disabled={busy}
              shake={shake}
              onCheck={() => void onCheck()}
            />
            {/* Only after a check found an update, as with yt-dlp. */}
            {(restarting || (checked && available)) && (
              <Button kind="secondary" icon={<IconUpdate />} disabled={busy} onClick={() => void onUpdate()}>
                {restarting ? t('settings.look.jdUpdating') : t('settings.look.jdUpdate')}
              </Button>
            )}
          </div>
        </div>
      )}
    </ToolCard>
  );
}

const ENGINES: Record<string, { name: string; sub: TranslationKey }> = {
  'github.com/GopeedLab/gopeed': { name: 'Gopeed', sub: 'settings.look.tool.gopeed' },
  'github.com/anacrolix/torrent': { name: 'anacrolix/torrent', sub: 'settings.look.tool.torrent' },
  'github.com/nwaples/rardecode/v2': { name: 'rardecode', sub: 'settings.look.tool.rar' },
  'github.com/bodgit/sevenzip': { name: 'sevenzip', sub: 'settings.look.tool.sevenzip' },
  'github.com/dop251/goja': { name: 'goja', sub: 'settings.look.tool.goja' },
  'modernc.org/sqlite': { name: 'SQLite', sub: 'settings.look.tool.sqlite' },
};

// A module taken at a commit rather than a release carries a pseudo-version
// that ends in the commit's hash.
const PSEUDO = /[-.]\d{14}-([0-9a-f]{12})$/;

/** moduleVersion is a module's version as the card shows it: the release, or the short hash of the commit it was taken at. */
export function moduleVersion(version: string): string {
  const commit = PSEUDO.exec(version)?.[1];
  return commit ? commit.slice(0, 7) : version.replace(/^v/, '');
}

/** EngineRow is a library built into KnightLoader, which moves with KnightLoader's own updates. */
function EngineRow({ module: m }: { module: BuiltModule }) {
  const { t } = useT();
  const known = ENGINES[m.path];
  return (
    <ToolRow
      name={known?.name ?? m.path}
      sub={known && t(known.sub)}
      version={moduleVersion(m.version)}
      source={t(m.fork ? 'settings.look.toolsSource.fork' : 'settings.look.toolsSource.built')}
    />
  );
}

/** checkLine is what a yt-dlp check found, '' when the button's own face says it all. */
function checkLine(t: Translate, latest: YtdlpLatest, current: string): string {
  if (!latest.checked) return t('settings.resolvers.toolsCheckFailed', { error: latest.detail ?? '' });
  const tag = latest.tag ?? '';
  switch (latest.compare) {
    case 'same':
      return '';
    case 'newer':
      return t('settings.resolvers.toolsNewer', { latest: tag, current });
    case 'older':
      return t('settings.resolvers.toolsOlder', { current, latest: tag });
    default:
      // Also when nothing is installed; no claim about which is newer.
      return t('settings.resolvers.toolsUnknown', { latest: tag, current });
  }
}
