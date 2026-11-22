import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, InfoBubble } from '../../../components/ui';
import {
  fetchComponentUpdate,
  fetchJDStatus,
  fetchMediaTools,
  fetchQBittorrentStatus,
  restartQBittorrent,
  retryJDSetup,
  startComponentUpdate,
  type ComponentResult,
  type ComponentUpdate,
  type DeploymentInfo,
  type JDStatus,
  type MediaTool,
  type MediaToolsStatus,
  type QBittorrentStatus,
} from '../../../lib/api';
import { IconRetry } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { componentsCardText, type UpdateTone } from '../../../lib/versionCards';
import { Dropdown } from '../../../components/Dropdown';
import { useDraft } from '../context';
import { InfoComponentsCard, type ComponentsCardRow } from './cards';

// The Components card holds every program KnightLoader starts, one line each:
// the name with what it is in its (i), a short status where there is one, a
// button where there is something to do, and its version. yt-dlp, ffmpeg,
// Deno, Java and the JDownloader backend share the one switch below them, and
// Update components at the foot of the card updates them all at once.
// qBittorrent stays at the version picked on its row.

type Translate = ReturnType<typeof useT>['t'];

/** A short status on a row, and behind its (i) what went wrong where something needs doing. */
type Status = { text: string; detail?: string; tone?: UpdateTone };

const status = (s: Status | null): Pick<ComponentsCardRow, 'status' | 'detail' | 'tone'> =>
  s ? { status: s.text, detail: s.detail, tone: s.tone } : {};

/** What a row says about its part of the last update, null where there is nothing to say. */
function runStatus(t: Translate, r: ComponentResult | undefined): Status | null {
  switch (r?.state) {
    case 'checking':
      return { text: t('versionCard.checking') };
    case 'updated':
      return { text: t('settings.info.statusDone'), tone: 'ok' };
    case 'waits':
      return { text: t('settings.info.statusWaits'), detail: t('settings.info.statusWaitsHint'), tone: 'warn' };
    case 'failed':
      return { text: t('settings.info.statusFailed'), detail: t('settings.resolvers.toolsFetchFailed', { error: r.detail ?? '' }), tone: 'fail' };
  }
  return null;
}

/** How often the card asks how far the update has come, and for JDownloader while it restarts. */
const RUN_POLL = 1_000;
const JD_POLL = 2_000;
const JD_BACK_WITHIN = 5 * 60_000;
const JD_SETUP_POLL = 1_000;

const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

const errorText = (e: unknown) => String(e).replace(/^(Error|ApiError):\s*/, '');

/**
 * ComponentsCard lists every component on `hue`, then the one switch for
 * them and the button that updates them now.
 */
export function ComponentsCard({ deployment, hue }: { deployment: DeploymentInfo; hue: number }) {
  const { t, lang } = useT();
  const { cfg, patch } = useDraft();
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [jd, setJd] = useState<JDStatus | null>(null);
  const [run, setRun] = useState<ComponentUpdate | null>(null);
  const [running, setRunning] = useState(false);
  const [shake, setShake] = useState(0);
  const qbit = useQBittorrentRow();
  // Ends every poll once the page is left.
  const mounted = useRef(true);

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

  // Follows the update until every component has come to something, then
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
    // An update started before the page opened is still shown.
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
    setRun(null);
    let st: ComponentUpdate;
    try {
      st = await followRun(await startComponentUpdate());
    } catch (e) {
      st = { running: false, results: { all: { state: 'failed', detail: errorText(e) } } };
      setRun(st);
    }
    if (!mounted.current) return;
    if (Object.values(st.results ?? {}).some((r) => r.state === 'failed')) setShake((n) => n + 1);
    setRunning(false);
  };

  const results = run?.results ?? {};
  const whole = results.all ? runStatus(t, results.all) : null;
  const installer = deployment.deployment === 'desktop' && !deployment.updatesFromStore && deployment.installer;
  const rows: ComponentsCardRow[] = [
    ytdlpRow(t, tools, results['yt-dlp']),
    toolRow(t, 'FFmpeg', tools?.ffmpeg, results.ffmpeg, {
      hint: t('componentsCard.about.ffmpeg'),
      releasePage: 'https://ffmpeg.org/download.html',
    }),
    toolRow(t, 'Deno', tools?.deno, results.deno, {
      hint: t('componentsCard.about.deno'),
      repoUrl: 'https://github.com/denoland/deno',
    }),
    ...(jd?.runtime ? [javaRow(t, jd, results.java)] : []),
    jdRow(t, lang, jd, results.jdownloader, () => void onRetrySetup()),
    qbit,
    ...(installer
      ? [
          {
            name: 'PommelPorter',
            kind: 'installer' as const,
            hint: t('componentsCard.installer'),
            version: installer,
            repoUrl: 'https://github.com/junkerderprovinz/pommelporter',
          },
        ]
      : []),
  ];

  return (
    <InfoComponentsCard
      hue={hue}
      text={componentsCardText(t)}
      name="KnightLoader"
      rows={rows}
      autoUpdate={cfg.componentsAutoUpdate}
      onAutoUpdateChange={(on) => patch({ componentsAutoUpdate: on })}
      onUpdate={() => void onUpdate()}
      updating={running}
      shake={shake}
    >
      {whole && (
        <p role="status" className="glim-version-state" data-tone={whole.tone}>
          <span className="glim-version-line">{whole.text}</span>
          {whole.detail && <InfoBubble tip={whole.detail} />}
        </p>
      )}
    </InfoComponentsCard>
  );
}

/**
 * compareVersions orders two yt-dlp date versions numerically, like
 * mediatools.CompareVersions. It returns null when either is not a run of
 * numbers, so an unordered pair is never shown as equal.
 */
function compareVersions(a: string, b: string): number | null {
  const parse = (v: string): number[] | null => {
    const s = v.trim().replace(/^v/, '');
    if (!s) return null;
    const parts = s.split('.').map((p) => (/^\d+$/.test(p) ? Number(p) : NaN));
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
 * ytdlpRow is yt-dlp, which moves faster than KnightLoader's releases. Beside
 * what the update came to it warns when the fetched copy has fallen behind
 * the image's own (Status.shadowed), which happens while the switch is off,
 * and when the fetched copy does not start and the other yt-dlp took over.
 */
function ytdlpRow(t: Translate, tools: MediaToolsStatus | null, result?: ComponentResult): ComponentsCardRow {
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

  // What the update came to wins over a standing warning.
  let s = runStatus(t, result);
  if (!s && managedIsOlder)
    s = {
      text: t('settings.info.statusNote'),
      detail: t('settings.resolvers.toolsShadowedOlder', { current: ytdlp?.version ?? '', system: shadowed?.version ?? '' }),
      tone: 'warn',
    };
  else if (!s && managedBroken)
    s = {
      text: t('settings.resolvers.toolsFellBack'),
      detail: t('settings.resolvers.toolsManagedBroken', { path: tools?.managedPath ?? '' }),
      tone: 'warn',
    };
  else if (!s && tools && !ytdlp?.found) s = { text: t('settings.resolvers.toolsNotFound'), tone: 'fail' };

  return {
    name: 'yt-dlp',
    hint: t('componentsCard.about.ytdlp'),
    version: ytdlp?.found ? (ytdlp.version ?? '') : '',
    repoUrl: 'https://github.com/yt-dlp/yt-dlp',
    ...status(s),
  };
}

/** toolRow is ffmpeg or Deno: its version, what the update came to, and whether there is one at all. */
function toolRow(
  t: Translate,
  name: string,
  tool: MediaTool | undefined,
  result: ComponentResult | undefined,
  where: Pick<ComponentsCardRow, 'hint' | 'repoUrl' | 'releasePage'>,
): ComponentsCardRow {
  let s = runStatus(t, result);
  if (!s && tool && !tool.found) s = { text: t('settings.resolvers.toolsNotFound'), tone: 'fail' };
  return { name, version: tool?.found ? (tool.version ?? '') : '', ...where, ...status(s) };
}

/**
 * javaRow is the Java runtime the desktop build fetched for its own
 * JDownloader, with a newer one the server found and JDownloader does not run
 * on yet.
 */
function javaRow(t: Translate, jd: JDStatus, result?: ComponentResult): ComponentsCardRow {
  let s = runStatus(t, result);
  if (!s && jd.runtimeUpdate) s = { text: t('settings.look.updatesAvailable', { version: jd.runtimeUpdate }), tone: 'warn' };
  return {
    name: 'Java',
    hint: t('componentsCard.about.java'),
    version: jd.runtime?.version ?? '',
    releasePage: 'https://adoptium.net/temurin/releases/',
    ...status(s),
  };
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

/**
 * jdRow is the headless JDownloader, which KnightLoader talks to. The update
 * has it install a waiting update, after which the card waits until it
 * answers again and shows its new revision.
 */
function jdRow(t: Translate, lang: string, jd: JDStatus | null, result: ComponentResult | undefined, onRetry: () => void): ComponentsCardRow {
  // While the desktop build sets up its own JDownloader, or after that failed,
  // the row says so.
  let s = jd && !jd.configured ? jdMissing(t, lang, jd) : null;
  if (!s) s = runStatus(t, result);
  if (!s && jd && !jd.reachable) s = { text: t('health.state.failed'), tone: 'fail' };
  else if (!s && jd?.updateWaits) s = { text: t('settings.info.updatesFound', { n: 1 }), tone: 'warn' };
  return {
    name: t('health.part.jd'),
    'data-new': 'jd-tool',
    hint: t('componentsCard.about.jd'),
    version: jd?.configured && jd.reachable && jd.version ? String(jd.version) : '',
    releasePage: 'https://jdownloader.org/',
    ...status(s),
    actions: jd?.setup === 'failed' && (
      <Button kind="secondary" icon={<IconRetry />} title={t('common.retry')} aria-label={t('common.retry')} onClick={onRetry} />
    ),
  };
}

/** How often the qBittorrent row asks after it while it is on its way up. */
const QBIT_POLL = 1_000;

const qbitMoving = (st: QBittorrentStatus | null) => st?.state === 'fetching' || st?.state === 'starting';

/** qbitStatus is what the qBittorrent row says, null once it simply runs. */
function qbitStatus(t: Translate, st: QBittorrentStatus | null): Status | null {
  switch (st?.state) {
    case 'fetching': {
      const percent = st.total && st.total > 0 ? Math.floor(((st.done ?? 0) * 100) / st.total) : 0;
      return { text: t('settings.info.qbitDownloading', { percent: String(percent) }) };
    }
    case 'starting':
      return { text: t('settings.info.qbitStarting'), detail: st.detail };
    case 'failed':
    case 'stopped':
    case 'off':
      return { text: t('health.state.failed'), detail: st.detail, tone: 'fail' };
  }
  if (st?.detail) return { text: t('settings.info.statusNote'), detail: st.detail, tone: 'warn' };
  return null;
}

/**
 * useQBittorrentRow is the qBittorrent KnightLoader runs for its torrents. It
 * stays at the version picked on its row, which the automatic update and
 * Update components leave alone, since private trackers admit only the
 * versions on their list. Picking another one fetches it and restarts
 * qBittorrent.
 */
function useQBittorrentRow(): ComponentsCardRow {
  const { t } = useT();
  const { cfg, patchNow } = useDraft();
  const [st, setSt] = useState<QBittorrentStatus | null>(null);
  const mounted = useRef(true);

  const follow = useCallback(async (first: QBittorrentStatus) => {
    let next = first;
    setSt(next);
    while (mounted.current && qbitMoving(next)) {
      await pause(QBIT_POLL);
      next = await fetchQBittorrentStatus().catch(() => next);
      if (mounted.current) setSt(next);
    }
  }, []);

  useEffect(() => {
    mounted.current = true;
    void fetchQBittorrentStatus().then(follow, () => {});
    return () => {
      mounted.current = false;
    };
  }, [follow]);

  const torrent = (cfg as { torrent?: Record<string, unknown> }).torrent ?? {};
  const held = typeof torrent.engineVersion === 'string' && torrent.engineVersion ? torrent.engineVersion : (st?.default ?? '');
  const chosen = st?.versions.find((v) => v.version === held) ?? st?.versions.find((v) => v.version === st.default);

  // Saved at once rather than with the page's other edits: the server swaps
  // qBittorrent on the save, and the row follows the swap.
  const onPick = async (version: string) => {
    // Partial<Settings> does not name `torrent`, as on the Torrents page.
    await patchNow({ torrent: { ...torrent, engineVersion: version } } as unknown as Parameters<typeof patchNow>[0]);
    await follow(await fetchQBittorrentStatus());
  };

  const onRetry = async () => {
    try {
      await follow(await restartQBittorrent());
    } catch (e) {
      setSt((prev) => prev && { ...prev, state: 'failed', detail: errorText(e) });
    }
  };

  const stopped = st?.state === 'failed' || st?.state === 'off';
  const versions = st?.versions ?? [];
  const picks = versions.length > 0;
  return {
    name: 'qBittorrent',
    'data-new': 'qbittorrent',
    hint: t('componentsCard.about.qbit'),
    version: st?.state === 'running' ? (st.version ?? '') : '',
    releasePage: 'https://www.qbittorrent.org/news',
    ...status(qbitStatus(t, st)),
    actions: (stopped || picks) && (
      <>
        {stopped && (
          <Button
            kind="secondary"
            icon={<IconRetry />}
            title={t('common.retry')}
            aria-label={t('common.retry')}
            onClick={() => void onRetry()}
          />
        )}
        {picks && (
          <>
            <Dropdown
              look="dense"
              width="value"
              label={t('settings.info.qbitVersion')}
              tip={t('settings.info.qbitVersion')}
              value={chosen?.version ?? null}
              options={versions.map((v) => ({
                value: v.version,
                label: v.version === st?.default ? t('settings.info.qbitRecommended', { version: v.version }) : v.version,
              }))}
              onChange={(v) => void onPick(v).catch(() => {})}
            />
            <InfoBubble
              tip={`${t('settings.info.qbitVersionHint')} ${
                chosen && chosen.whitelists.length > 0
                  ? t('settings.info.qbitOnList', { trackers: chosen.whitelists.join(', ') })
                  : `${t('settings.info.qbitOffList')} ${t('settings.info.qbitOlderClients')}`
              }`}
            />
          </>
        )}
      </>
    ),
  };
}
