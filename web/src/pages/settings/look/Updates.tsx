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
  fetchMediaTools,
  fetchUpdateCheck,
  fetchYtdlpLatest,
  revertYtdlp,
  updateYtdlp,
  type MediaToolsStatus,
  type UpdateCheck,
  type YtdlpLatest,
} from '../../../lib/api';
import { IconCheck, IconClose, IconDownload, IconRefresh } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { useDraft } from '../context';
import { SubHead } from '../controls';

// Every update in one card: KnightLoader's own and yt-dlp's, each with its
// version, the same two switches under the same names and a check button that
// shows its answer on itself. yt-dlp is the one program outside the build that
// moves faster than KnightLoader's releases, which is why it is here at all.

type Face = { label: string; icon: ReactNode; verdict?: ButtonVerdict };

/** The check button's face for an answer that has come back, or for none yet. */
function checkFace(t: ReturnType<typeof useT>['t'], checked: boolean | null, newer: boolean, known = true): Face {
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

/** Version is a component's number at the end of its heading. */
function Version({ children }: { children: ReactNode }) {
  return (
    <span dir="ltr" className="glim-num text-sm text-carbon-textSub">
      {children}
    </span>
  );
}

/**
 * UpdatesCard holds every update: KnightLoader's, then yt-dlp's. `children`
 * is a further group of the card, such as quit and restart.
 */
export function UpdatesCard({ hue, children }: { hue: number; children?: ReactNode }) {
  const { t } = useT();
  return (
    <Card hue={hue} data-new="updates-card" className="flex flex-col gap-6">
      <SectionTitle hint={t('settings.look.updatesHint')}>{t('settings.look.updatesTitle')}</SectionTitle>
      <AppUpdates />
      <YtdlpUpdates />
      {children}
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
function AppUpdates() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const [deployment, setDeployment] = useState<string | null>(null);
  const [version, setVersion] = useState('');
  const [check, setCheck] = useState<UpdateCheck | null>(null);
  const [checking, setChecking] = useState(false);
  // The failure counter of the check button, so a repeated failure shakes it again.
  const [shake, setShake] = useState(0);

  useEffect(() => {
    void fetchDeploymentInfo()
      .then((d) => setDeployment(d.deployment))
      .catch(() => {});
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

  // Wait for the deployment rather than flash the wrong copy.
  if (deployment === null) return null;
  const isDesktop = deployment === 'desktop';
  const ready = check?.ready;
  const available = !!check?.checked && check.available;
  const face = checkFace(t, check ? check.checked : null, available);

  return (
    <section className="flex flex-col gap-3">
      <SubHead
        label="KnightLoader"
        right={<Version>{!version || version === 'dev' ? t('nav.workingTitle') : version.replace(/^v/, '')}</Version>}
      />
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
    </section>
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
 * again, so the group warns when it has fallen behind the image's own
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

  return (
    <section className="flex flex-col gap-3">
      <SubHead
        label="yt-dlp"
        right={tools && <Version>{ytdlp?.found ? ytdlp.version : t('settings.resolvers.toolsNotFound')}</Version>}
      />

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
    </section>
  );
}

function checkLine(t: ReturnType<typeof useT>['t'], latest: YtdlpLatest, current: string): string {
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
