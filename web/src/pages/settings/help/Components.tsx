import { useCallback, useEffect, useRef, useState, type ComponentProps, type ReactNode } from 'react';
import { AnchoredPanel } from '../../../components/AnchoredPanel';
import { Button, Card, ErrorCard, HintBubble, InfoBubble, SectionTitle, ToggleRow, type ButtonVerdict } from '../../../components/ui';
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
  type JDStatus,
  type MediaTool,
  type MediaToolsStatus,
  type UpdateCheck,
  type UpdateProgress,
  type YtdlpLatest,
} from '../../../lib/api';
import { followExternal } from '../../../lib/external';
import { GLIMSTONE_VERSION } from '../../../lib/glimstoneVersion';
import {
  IconClose,
  IconGear,
  IconInstall,
  IconRefresh,
  IconRetry,
  IconUndo,
  IconUpdate,
  IconUpdateAvailable,
  IconUpToDate,
} from '../../../lib/icons';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { openWhatsNew, useWhatsNew } from '../../../lib/useWhatsNew';
import { NewsGlyph } from '../../../components/whatsnew/WhatsNew';
import { useDraft } from '../context';

// The Components card holds everything KnightLoader is made of or starts, one
// line each: the name, the version linked to its release or project page, a
// short status where there is one, and the buttons as glyphs at the end.
// KnightLoader, yt-dlp and the JDownloader backend update on their own, so
// their lines carry a check and the update, and a gear for their switches.
// yt-dlp moves faster than KnightLoader's releases, which is why it is updated
// here at all, and JDownloader is a program of its own beside KnightLoader.

type Translate = ReturnType<typeof useT>['t'];

type Hint = ComponentProps<typeof HintBubble>['hint'];

/** A short status on a row, with the whole sentence behind its (i). */
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

type Face = { label: string; icon: ReactNode; verdict?: ButtonVerdict };

/** The check button's face for an answer that has come back, or for none yet. */
function checkFace(t: Translate, checked: boolean | null, newer: boolean, known = true): Face {
  if (checked === null || !known) return { label: t('settings.look.updatesCheck'), icon: <IconUpdate /> };
  if (!checked) return { label: t('settings.info.checkFailed'), icon: <IconClose />, verdict: 'fail' };
  if (newer) return { label: t('settings.info.updatesFound', { n: 1 }), icon: <IconUpdateAvailable />, verdict: 'warn' };
  return { label: t('settings.info.upToDate'), icon: <IconUpToDate />, verdict: 'ok' };
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
      <GlyphButton
        icon={checking ? <IconUpdate /> : face.icon}
        title={checking ? t('settings.look.updatesChecking') : face.label}
        verdict={checking ? undefined : face.verdict}
        shake={shake}
        disabled={checking || disabled}
        onClick={onCheck}
      />
      <span role="status" className="sr-only">
        {face.verdict && !checking ? face.label : ''}
      </span>
    </>
  );
}

/**
 * RowSettings is the gear at the end of a row and the window it opens under
 * itself, for the switches of one component.
 */
function RowSettings({ name, children, 'data-new': mark }: { name: string; children: ReactNode; 'data-new'?: string }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const anchor = useRef<HTMLSpanElement>(null);
  const title = t('settings.info.updateSettings');
  return (
    <>
      <span ref={anchor} data-new={mark} className="inline-flex">
        <Button
          kind="secondary"
          icon={<IconGear />}
          title={title}
          aria-label={title}
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        />
      </span>
      {open && (
        <AnchoredPanel anchor={anchor} title={name} onClose={close}>
          {children}
        </AnchoredPanel>
      )}
    </>
  );
}

/**
 * ComponentRow is one component on one line: its name with what it does
 * behind an (i), a short status, its version linked to its release or project
 * page, and its buttons. The version and the buttons stand in columns every
 * row shares, so the numbers line up.
 */
function ComponentRow({
  name,
  sub,
  version,
  href,
  status,
  actions,
  'data-new': mark,
}: {
  name: string;
  sub?: Hint;
  version?: string;
  href?: string;
  status?: Status | null;
  actions?: ReactNode;
  'data-new'?: string;
}) {
  const shown = version?.replace(/^v/, '');
  return (
    <section
      data-new={mark}
      className="grid min-h-11 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 py-1.5 @xl:grid-cols-[minmax(0,1fr)_auto_8rem_7.5rem]"
    >
      <h3 className="flex min-w-0 items-center gap-1.5 text-sm text-carbon-text">
        {name}
        <HintBubble hint={sub} />
      </h3>
      <span role="status" className={`flex items-center justify-end text-sm ${status?.tone ? TONE[status.tone] : 'text-carbon-textSub'}`}>
        {status && (
          <>
            {status.text}
            {status.detail && <InfoBubble tip={status.detail} />}
          </>
        )}
      </span>
      <span dir="ltr" className="glim-num truncate text-sm text-carbon-textSub @xl:text-end">
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
      <span className="flex items-center justify-end gap-2">{actions}</span>
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

/**
 * ComponentsCard lists every component on `hue`, one line each, then the
 * button for what the updates brought.
 */
export function ComponentsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { data: deployment, failed, reload } = useResource(fetchDeploymentInfo);
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [jd, setJd] = useState<JDStatus | null>(null);

  useEffect(() => {
    // The server is unreachable when either fails, which the page already shows.
    void fetchMediaTools()
      .then(setTools)
      .catch(() => {});
    void fetchJDStatus()
      .then(setJd)
      .catch(() => {});
  }, []);

  const missing = (tool?: MediaTool, detail?: string): Status | null =>
    tools && !tool?.found ? { text: t('settings.resolvers.toolsNotFound'), detail, tone: 'fail' } : null;

  return (
    <Card hue={hue} data-new="updates-card" className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.info.componentsHint')}>{t('settings.info.componentsTitle')}</SectionTitle>
      {/* Wait for the deployment rather than flash the wrong copy. */}
      {failed && <DeploymentError retry={reload} />}
      {deployment && (
        <div data-new="built-in" className="@container flex flex-col divide-y divide-carbon-border/40">
          <AppUpdates deployment={deployment.deployment} />
          <YtdlpUpdates />
          <JDUpdates />
          <ComponentRow
            name="GlimStone"
            sub={t('settings.info.glimstoneSub')}
            version={GLIMSTONE_VERSION}
            href={releasePage(GLIMSTONE_URL, GLIMSTONE_VERSION) || GLIMSTONE_URL}
          />
          <ComponentRow
            name="ffmpeg"
            sub={t('settings.info.ffmpegSub')}
            version={tools?.ffmpeg.version}
            href="https://ffmpeg.org/"
            status={missing(tools?.ffmpeg)}
          />
          <ComponentRow
            name="ffprobe"
            sub={t('settings.look.tool.ffprobe')}
            version={tools?.ffprobe.version}
            href="https://ffmpeg.org/"
            status={missing(tools?.ffprobe)}
          />
          {/* Without UnRAR the built-in reader, rardecode, unpacks RAR sets. */}
          <ComponentRow
            name="UnRAR"
            sub={t('settings.look.tool.unrar')}
            version={tools?.unrar?.found ? tools.unrar.version : undefined}
            href="https://www.rarlab.com/"
            status={missing(tools?.unrar, t('settings.resolvers.toolsUnrarMissingHint'))}
          />
          {/* The desktop build fetches it for JDownloader and renews it with
              its own updates. */}
          {jd?.runtime && (
            <ComponentRow
              name={`Java (${jd.runtime.vendor})`}
              sub={t('settings.look.tool.jre')}
              version={jd.runtime.version}
              href="https://adoptium.net/temurin/"
            />
          )}
          {(deployment.engines ?? []).map((m) => (
            <EngineRow key={m.path} module={m} />
          ))}
        </div>
      )}
      <div className="flex flex-wrap justify-end gap-3">
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
 * build sets up its own, and what to do about it, with how far the download of
 * the Java runtime that runs it has come.
 */
function jdMissing(t: Translate, jd: JDStatus): [string, string] {
  switch (jd.setup) {
    case 'starting': {
      const total = jd.setupTotal ?? 0;
      const hint =
        total > 0 && (jd.setupDone ?? 0) < total
          ? t('settings.resolvers.toolsJdDownloading', { percent: String(Math.floor(((jd.setupDone ?? 0) * 100) / total)) })
          : t('settings.resolvers.toolsJdStartingHint');
      return [t('settings.resolvers.toolsJdStarting'), hint];
    }
    case 'failed':
      return [t('settings.resolvers.toolsJdFailed'), t('settings.resolvers.toolsJdFailedHint', { error: jd.detail ?? '' })];
    default:
      return [t('settings.resolvers.toolsNotConfigured'), t('settings.resolvers.toolsJdMissingHint')];
  }
}

/** How often the row asks how far an update it started has come. */
const INSTALL_POLL = 500;

const installing = (p: UpdateProgress | null) => p?.state === 'downloading' || p?.state === 'starting';

/** installLine is what an update the row started has come to, '' before one. */
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

/** appStatus is what KnightLoader's row says about a check or an update, null while nothing is to say. */
function appStatus(
  t: Translate,
  s: { checking: boolean; progress: UpdateProgress | null; ready?: string; available: boolean; check: UpdateCheck | null; isDesktop: boolean },
): Status | null {
  if (s.checking) return { text: t('settings.look.updatesChecking') };
  const p = s.progress;
  if (p?.state === 'failed') return { text: t('settings.info.statusFailed'), detail: installLine(t, p), tone: 'fail' };
  if (p?.state === 'downloading') return { text: installLine(t, p) };
  if (p?.state === 'starting' || p?.state === 'started') return { text: t('settings.look.updatesInstalling'), detail: installLine(t, p) };
  // A downloaded version outranks the offer of it: it only waits for a restart.
  if (s.ready) return { text: t('settings.info.statusReady'), detail: t('settings.look.updatesReady', { version: s.ready }), tone: 'ok' };
  if (s.available)
    return {
      text: t('settings.look.updatesAvailable', { version: s.check?.latest ?? '' }),
      detail: s.isDesktop ? undefined : t('settings.look.updatesContainerHint'),
      tone: 'warn',
    };
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
  // No stamp yet, or 'dev' for an untagged build: the working title stands in.
  const stamped = version !== '' && version !== 'dev';

  return (
    <ComponentRow
      name="KnightLoader"
      sub={t('settings.look.updatesHint')}
      version={stamped ? version : t('nav.workingTitle')}
      href={stamped ? releasePage(REPO_URL, version) || REPO_URL : undefined}
      status={appStatus(t, { checking, progress, ready, available, check, isDesktop })}
      actions={
        <>
          <CheckButton face={face} checking={checking} disabled={installing(progress)} shake={shake} onCheck={() => void onCheck()} />
          {isDesktop && available && !ready && progress?.state !== 'started' && (
            <GlyphButton
              icon={<IconInstall />}
              title={installing(progress) ? t('settings.look.updatesInstalling') : t('settings.look.updatesInstall')}
              shake={installShake}
              disabled={checking || installing(progress)}
              onClick={() => void onInstall()}
            />
          )}
          <RowSettings name="KnightLoader">
            {/* The desktop build checks on its own and has no use for this
                switch. A container keeps the stored value as it is, so a key
                press on the disabled switch changes nothing either. */}
            {!isDesktop && (
              <ToggleRow
                label={t('settings.look.updatesAuto')}
                hint={t('settings.look.updatesAutoContainerHint')}
                checked={false}
                disabled
                onChange={() => {}}
              />
            )}
            {/* A container cannot replace itself, so there the row is disabled
                and says why. */}
            <ToggleRow
              label={t('settings.look.updatesAutoUpdate')}
              hint={isDesktop ? t('settings.look.updatesAutoUpdateHint') : t('settings.look.updatesAutoUpdateContainerHint')}
              checked={isDesktop && cfg.autoUpdate}
              disabled={!isDesktop}
              onChange={(v) => patch({ autoUpdate: v })}
            />
          </RowSettings>
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

const errorText = (e: unknown) => String(e).replace(/^(Error|ApiError):\s*/, '');

/** checkLine is what a yt-dlp check found, '' when the check button's own face says it all. */
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

/**
 * YtdlpUpdates fetches a newer yt-dlp than the image carries when a site
 * breaks between image builds, by a button press or once a day while
 * ytdlpAutoUpdate is on. With the daily update off a fetched copy never moves
 * again, so the row warns when it has fallen behind the image's own
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

  // What the last action came to wins over a standing warning, and a warning
  // over what the last check found.
  let status: Status | null = null;
  if (fetching) status = { text: t('settings.resolvers.toolsFetching') };
  else if (reverting) status = { text: t('settings.resolvers.toolsReverting') };
  else if (checking) status = { text: t('settings.look.updatesChecking') };
  else if (failed) status = { text: t('settings.info.statusFailed'), detail: t('settings.resolvers.toolsFetchFailed', { error: failed }), tone: 'fail' };
  else if (fetched) status = { text: t('settings.info.statusDone'), detail: t('settings.resolvers.toolsFetched', { version: fetched }), tone: 'ok' };
  else if (reverted !== null)
    status = {
      text: t('settings.info.statusDone'),
      detail: reverted ? t('settings.resolvers.toolsReverted', { path: reverted }) : t('settings.resolvers.toolsRevertedNone'),
      tone: reverted ? 'ok' : 'warn',
    };
  else if (managedIsOlder)
    status = {
      text: t('settings.info.statusNote'),
      detail: t('settings.resolvers.toolsShadowedOlder', {
        current: ytdlp?.version ?? '',
        system: shadowed?.version ?? '',
        path: shadowed?.path ?? '',
      }),
      tone: 'warn',
    };
  else if (managedBroken)
    status = { text: t('settings.info.statusNote'), detail: t('settings.resolvers.toolsManagedBroken', { path: tools?.managedPath ?? '' }), tone: 'warn' };
  else if (latest && !latest.checked) status = { text: t('settings.info.checkFailed'), detail: line, tone: 'fail' };
  else if (latest?.compare === 'newer')
    status = { text: t('settings.look.updatesAvailable', { version: latest.tag ?? '' }), detail: line, tone: 'warn' };
  else if (line) status = { text: t('settings.info.statusNote'), detail: line };
  else if (tools && !ytdlp?.found) status = { text: t('settings.resolvers.toolsNotFound'), tone: 'fail' };

  // The copy the image or the computer has, while KnightLoader's own runs.
  const shadowedNote =
    managedRunning && shadowed && !managedIsOlder
      ? t('settings.resolvers.toolsShadowed', {
          path: shadowed.path ?? '',
          version: shadowed.version || t('settings.resolvers.toolsNotFound'),
        })
      : null;

  return (
    <ComponentRow
      name="yt-dlp"
      sub={[t('settings.info.ytdlpSub'), t('settings.resolvers.toolsActionsHint'), ...(shadowedNote ? [shadowedNote] : [])]}
      version={ytdlp?.found ? ytdlp.version : undefined}
      href={ytdlp?.version ? `https://github.com/yt-dlp/yt-dlp/releases/tag/${encodeURIComponent(ytdlp.version)}` : undefined}
      status={status}
      actions={
        <>
          {/* Revert, check, fetch: the forward action sits last. */}
          {managed && <GlyphButton icon={<IconUndo />} title={t('settings.resolvers.toolsRevert')} disabled={busy} onClick={() => void onRevert()} />}
          <CheckButton face={face} checking={checking} disabled={busy} shake={shake} onCheck={() => void onCheck()} />
          {/* Only after a check found a tag, so nobody fetches a version they
              were not shown. */}
          {latest?.checked && latest.tag && (
            <GlyphButton icon={<IconInstall />} title={t('settings.resolvers.toolsFetch')} disabled={busy} onClick={() => void onFetch()} />
          )}
          <RowSettings name="yt-dlp">
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
          </RowSettings>
        </>
      }
    />
  );
}

/** How often, and how long, the row asks for JDownloader while it restarts for an update. */
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
 * has it install that and restart, after which the row waits until it answers
 * again and shows its new revision. While jdAutoUpdate is on, the server does
 * the same once a day when none of JD's downloads runs.
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

  // While the desktop build sets up its own JDownloader, or after that failed,
  // the row says so.
  const setupNote = jd && !jd.configured ? jdMissing(t, jd) : null;
  let status: Status | null = null;
  if (setupNote) status = { text: setupNote[0], detail: setupNote[1], tone: jd?.setup === 'failed' ? 'fail' : undefined };
  else if (restarting) status = { text: t('settings.look.jdUpdating'), detail: t('settings.look.jdRestarting') };
  else if (checking) status = { text: t('settings.look.updatesChecking') };
  else if (failed) status = { text: t('settings.info.statusFailed'), detail: failed, tone: 'fail' };
  else if (back) status = { text: t('settings.info.statusDone'), detail: t('settings.look.jdBack', { version: jd?.version ?? '' }), tone: 'ok' };
  else if (jd && !jd.reachable) status = { text: t('health.state.failed'), tone: 'fail' };
  else if (checked && available) status = { text: t('settings.info.updatesFound', { n: 1 }), tone: 'warn' };
  const busy = checking || restarting;

  return (
    <ComponentRow
      name={t('health.part.jd')}
      sub={[t('settings.look.tool.jd'), t('settings.look.jdUpdatesItself')]}
      version={jd?.configured && jd.reachable && jd.version ? String(jd.version) : undefined}
      href="https://jdownloader.org/"
      status={status}
      data-new="jd-tool"
      actions={
        <>
          {jd?.setup === 'failed' && <GlyphButton icon={<IconRetry />} title={t('common.retry')} onClick={() => void onRetry()} />}
          {/* Without a JDownloader there is nothing to ask. */}
          {jd?.configured && (
            <span data-new="jd-update" className="flex items-center gap-2">
              <CheckButton
                face={checkFace(t, checked, available)}
                checking={checking}
                disabled={busy}
                shake={shake}
                onCheck={() => void onCheck()}
              />
              {/* Only after a check found an update, as with yt-dlp. */}
              {(restarting || (checked && available)) && (
                <GlyphButton
                  icon={<IconRefresh />}
                  title={restarting ? t('settings.look.jdUpdating') : t('settings.look.jdUpdate')}
                  disabled={busy}
                  onClick={() => void onUpdate()}
                />
              )}
            </span>
          )}
          {jd?.configured && (
            <RowSettings name={t('health.part.jd')} data-new="jd-auto-update">
              <ToggleRow
                checked={cfg.jdAutoUpdate}
                onChange={(v) => patch({ jdAutoUpdate: v })}
                label={t('settings.look.updatesAutoUpdate')}
                hint={t('settings.look.jdAutoUpdateHint')}
              />
            </RowSettings>
          )}
        </>
      }
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
    <ComponentRow
      name={known?.name ?? m.path}
      sub={known && t(known.sub)}
      version={moduleVersion(m.version)}
      href={`https://pkg.go.dev/${m.path}@${m.version}`}
    />
  );
}
