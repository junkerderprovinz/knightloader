import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  FieldGroup,
  InfoBubble,
  LinkBadge,
  SectionTitle,
  ToggleRow,
  type ButtonVerdict,
} from '../../../components/ui';
import {
  fetchDeploymentInfo,
  fetchHealth,
  fetchJDStatus,
  fetchMediaTools,
  fetchUpdateCheck,
  fetchYtdlpLatest,
  revertYtdlp,
  updateYtdlp,
  type BuiltModule,
  type DeploymentInfo,
  type JDStatus,
  type MediaToolsStatus,
  type UpdateCheck,
  type YtdlpLatest,
} from '../../../lib/api';
import { IconCheck, IconClose, IconDownload, IconRefresh } from '../../../lib/icons';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';
import { SubCard } from '../controls';

// Two cards. Updates holds what can be updated on its own, KnightLoader and
// yt-dlp, each a sub-card with its version, where it comes from, the same two
// switches under the same names and a check button that shows its answer on
// itself. yt-dlp is the one program outside the build that moves faster than
// KnightLoader's releases, which is why it is updated here at all. The other
// card lists the rest, one row each with its version and where it comes from:
// they come with the image, the computer or the build, or update themselves,
// so there is nothing to press.

type Translate = ReturnType<typeof useT>['t'];

type Face = { label: string; icon: ReactNode; verdict?: ButtonVerdict };

/** The check button's face for an answer that has come back, or for none yet. */
function checkFace(t: Translate, checked: boolean | null, newer: boolean, known = true): Face {
  if (checked === null || !known) return { label: t('settings.look.updatesCheck'), icon: <IconRefresh /> };
  if (!checked) return { label: t('settings.info.checkFailed'), icon: <IconClose />, verdict: 'fail' };
  if (newer) return { label: t('settings.info.updatesFound', { n: 1 }), icon: <IconDownload />, verdict: 'warn' };
  return { label: t('settings.info.upToDate'), icon: <IconCheck />, verdict: 'ok' };
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
        icon={checking ? <IconRefresh /> : face.icon}
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
 * ToolRow is one built-in part: its name and what it does, its version and
 * where it comes from, in columns every row of the card shares.
 */
function ToolRow({ name, sub, version, source }: { name: string; sub?: string; version: ReactNode; source: string }) {
  return (
    <section className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-0.5 @xl:grid-cols-[minmax(0,1fr)_9rem_15rem]">
        <span className="flex min-w-0 flex-col">
          <h3 className="text-sm text-carbon-text">{name}</h3>
          {sub && <span className="text-subline text-carbon-textMuted">{sub}</span>}
        </span>
        <span dir="ltr" className="glim-num truncate text-end text-sm text-carbon-textSub @xl:text-start">
          {version}
        </span>
        <span className="col-span-2 text-subline text-carbon-textMuted @xl:col-span-1">{source}</span>
      </div>
    </section>
  );
}

/**
 * ToolCard is a part KnightLoader updates, as a sub-card: its name, its
 * version and where it comes from at the heading's end, and its update
 * controls under them.
 */
function ToolCard({
  name,
  sub,
  version,
  source,
  children,
}: {
  name: string;
  sub?: string;
  version: ReactNode;
  source: string;
  children: ReactNode;
}) {
  return (
    <SubCard
      label={name}
      hint={sub}
      className="gap-3"
      right={
        <span className="flex flex-col items-end">
          <span dir="ltr" className="glim-num text-sm text-carbon-textSub">
            {version}
          </span>
          <span className="text-subline text-carbon-textMuted">{source}</span>
        </span>
      }
    >
      {children}
    </SubCard>
  );
}

/** useDeployment is how this instance runs, null until the server has said. */
function useDeployment(): DeploymentInfo | null {
  const [deployment, setDeployment] = useState<DeploymentInfo | null>(null);
  useEffect(() => {
    void fetchDeploymentInfo()
      .then(setDeployment)
      .catch(() => {});
  }, []);
  return deployment;
}

/** Where a program on the server's path comes from: the image in a container, the computer otherwise. */
const hostSource = (t: Translate, deployment: string) =>
  t(deployment === 'desktop' ? 'settings.look.toolsSource.system' : 'settings.look.toolsSource.image');

/**
 * UpdatesCard holds the updates of KnightLoader and yt-dlp, each a sub-card.
 * `children` is a further group of the card, such as quit and restart.
 */
export function UpdatesCard({ hue, children }: { hue: number; children?: ReactNode }) {
  const { t } = useT();
  const deployment = useDeployment();

  return (
    <Card hue={hue} data-new="updates-card" className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.look.updatesHint')}>{t('settings.look.updatesTitle')}</SectionTitle>
      {/* Wait for the deployment rather than flash the wrong copy. */}
      {deployment && (
        <>
          <AppUpdates deployment={deployment.deployment} />
          <YtdlpUpdates deployment={deployment.deployment} />
        </>
      )}
      {children}
    </Card>
  );
}

/**
 * BuiltInCard lists every part KnightLoader brings or runs beside it and does
 * not update on its own, each with its version and where it comes from.
 */
export function BuiltInCard({ hue }: { hue: number }) {
  const { t } = useT();
  const deployment = useDeployment();
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);

  useEffect(() => {
    void fetchMediaTools()
      .then(setTools)
      .catch(() => {});
  }, []);

  const found = (tool?: { found: boolean; version?: string }) =>
    tools && (tool?.found ? tool.version : t('settings.resolvers.toolsNotFound'));

  return (
    <Card hue={hue} data-new="built-in" className="flex flex-col gap-6">
      <SectionTitle hint={t('settings.look.builtInHint')}>{t('settings.look.builtInTitle')}</SectionTitle>
      {deployment && (
        <div className="@container flex flex-col divide-y divide-carbon-border/40">
          <ToolRow
            name="ffmpeg"
            sub={t('settings.info.ffmpegSub')}
            version={found(tools?.ffmpeg)}
            source={hostSource(t, deployment.deployment)}
          />
          <ToolRow
            name="ffprobe"
            sub={t('settings.look.tool.ffprobe')}
            version={found(tools?.ffprobe)}
            source={hostSource(t, deployment.deployment)}
          />
          <JDRow />
          {(deployment.engines ?? []).map((m) => (
            <EngineRow key={m.path} module={m} />
          ))}
        </div>
      )}
    </Card>
  );
}

/**
 * AppUpdates checks GitHub for a newer release on request, and a container
 * also on opening the page while its switch is on (off by default). A
 * container is pointed at the release. The desktop build updates itself in the
 * background (desktop/updates.go), and a check there also tells whether a
 * downloaded version waits for the next start.
 */
function AppUpdates({ deployment }: { deployment: string }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const [version, setVersion] = useState('');
  const [check, setCheck] = useState<UpdateCheck | null>(null);
  const [checking, setChecking] = useState(false);
  // The failure counter of the check button, so a repeated failure shakes it again.
  const [shake, setShake] = useState(0);

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

  // Once, when the switch's value arrives, and on a container only: the
  // desktop build asks GitHub on its own. A later settings edit asks nothing.
  useEffect(() => {
    if (deployment === 'container' && cfg.autoUpdateCheck) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deployment, cfg.autoUpdateCheck]);

  const isDesktop = deployment === 'desktop';
  const ready = check?.ready;
  const available = !!check?.checked && check.available;
  const face = checkFace(t, check ? check.checked : null, available);

  return (
    <ToolCard
      name="KnightLoader"
      version={!version || version === 'dev' ? t('nav.workingTitle') : version.replace(/^v/, '')}
      source={t('settings.look.toolsSource.release')}
    >
      {!isDesktop && (
        <ToggleRow
          label={t('settings.look.updatesAuto')}
          hint={t('settings.look.updatesAutoHint')}
          checked={cfg.autoUpdateCheck}
          onChange={(v) => patch({ autoUpdateCheck: v })}
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
      <div className="flex justify-end">
        <CheckButton face={face} checking={checking} shake={shake} onCheck={() => void onCheck()} />
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
function YtdlpUpdates({ deployment }: { deployment: string }) {
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
  const [reverted, setReverted] = useState('');

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
    setReverted('');
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
    setReverted('');
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
    setReverted('');
    try {
      const after = await revertYtdlp();
      setTools(after);
      setReverted(after.ytdlp.path ?? '');
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

  const found = (tool?: { found: boolean; version?: string }) =>
    tools && (tool?.found ? tool.version : t('settings.resolvers.toolsNotFound'));

  return (
    <ToolCard
      name="yt-dlp"
      sub={t('settings.info.ytdlpSub')}
      version={found(ytdlp)}
      source={managedRunning ? t('settings.look.toolsSource.fetched') : hostSource(t, deployment)}
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

      {/* FieldGroup, because a label would pass clicks to the first button. */}
      <FieldGroup label={t('settings.resolvers.toolsActions')} hint={t('settings.resolvers.toolsActionsHint')}>
        {/* What the last action came to, above the buttons until the next one.
            The fetch came after the check, so its outcome wins. */}
        {failed && <p className="text-sm text-statusFail">{t('settings.resolvers.toolsFetchFailed', { error: failed })}</p>}
        {!failed && fetched && (
          <p className="text-sm text-statusOk">{t('settings.resolvers.toolsFetched', { version: fetched })}</p>
        )}
        {!failed && !fetched && reverted && (
          <p className="text-sm text-statusOk">{t('settings.resolvers.toolsReverted', { path: reverted })}</p>
        )}
        {!failed && !fetched && !reverted && latest && (
          <p className="text-sm text-carbon-textSub">{checkLine(t, latest, ytdlp?.version ?? '')}</p>
        )}

        {/* Revert, check, fetch: the forward action sits last. The JSX order
            sets it, so the row mirrors in right-to-left languages. */}
        <div className="flex flex-wrap items-center justify-end gap-3">
          {managed && (
            <Button kind="ghost" disabled={busy} onClick={() => void onRevert()}>
              {reverting ? t('settings.resolvers.toolsReverting') : t('settings.resolvers.toolsRevert')}
            </Button>
          )}
          <CheckButton face={face} checking={checking} disabled={busy} shake={shake} onCheck={() => void onCheck()} />
          {/* Only after a check found a tag, so nobody fetches a version they
              were not shown. */}
          {latest?.checked && latest.tag && (
            <Button kind="secondary" disabled={busy} onClick={() => void onFetch()}>
              {fetching ? t('settings.resolvers.toolsFetching') : t('settings.resolvers.toolsFetch')}
            </Button>
          )}
        </div>
      </FieldGroup>
    </ToolCard>
  );
}

/** JDRow is the headless JDownloader, which KnightLoader talks to and which updates itself. */
function JDRow() {
  const { t } = useT();
  const [jd, setJd] = useState<JDStatus | null>(null);

  useEffect(() => {
    void fetchJDStatus()
      .then(setJd)
      .catch(() => {});
  }, []);

  let version: ReactNode = null;
  if (jd && !jd.configured) version = t('settings.selftest.status.skipped');
  else if (jd && !jd.reachable) version = t('health.state.failed');
  else if (jd) version = jd.version;
  return (
    <ToolRow
      name={t('health.part.jd')}
      sub={t('settings.look.tool.jd')}
      version={version}
      source={t('settings.look.toolsSource.self')}
    />
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

function checkLine(t: Translate, latest: YtdlpLatest, current: string): string {
  if (!latest.checked) return t('settings.resolvers.toolsCheckFailed', { error: latest.detail ?? '' });
  const tag = latest.tag ?? '';
  switch (latest.compare) {
    case 'same':
      return t('settings.resolvers.toolsSame', { version: current || tag });
    case 'newer':
      return t('settings.resolvers.toolsNewer', { latest: tag, current });
    case 'older':
      return t('settings.resolvers.toolsOlder', { current, latest: tag });
    default:
      // Also when nothing is installed; no claim about which is newer.
      return t('settings.resolvers.toolsUnknown', { latest: tag, current });
  }
}
