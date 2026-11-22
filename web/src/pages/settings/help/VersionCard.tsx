import { useCallback, useEffect, useRef, useState } from 'react';
import {
  fetchHealth,
  fetchUpdateCheck,
  fetchUpdateProgress,
  installUpdate,
  restartIntoUpdate,
  type DeploymentInfo,
  type UpdateCheck,
  type UpdateProgress,
} from '../../../lib/api';
import { onUpdateReady } from '../../../lib/desktop';
import { openExternal } from '../../../lib/external';
import { useT } from '../../../lib/i18n';
import { openWhatsNew, useWhatsNew } from '../../../lib/useWhatsNew';
import { shownVersion, versionCardText, type UpdateRoute, type UpdateState } from '../../../lib/versionCards';
import { useDraft } from '../context';
import { InfoVersionCard } from './cards';

export const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
export const MANUAL_URL = 'https://junkerderprovinz.github.io/knightloader/';

/**
 * routeOf is how this copy gets a new version. The desktop build fetches and
 * installs its own (desktop/updates.go). A container cannot replace itself,
 * and neither can a Flatpak, which Flatpak installs, so both are shown how.
 */
export function routeOf(d: Pick<DeploymentInfo, 'deployment' | 'updatesFromStore'>): UpdateRoute {
  return d.deployment === 'desktop' && !d.updatesFromStore ? 'install' : 'guide';
}

const installing = (p: UpdateProgress | null) => p?.state === 'downloading' || p?.state === 'starting';

/** How often the card asks how far an update it started has come. */
const INSTALL_POLL = 500;

const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

const errorText = (e: unknown) => String(e).replace(/^(Error|ApiError):\s*/, '');

/** stateOf puts what the check and a running update came to into the card's terms. */
export function stateOf(s: {
  checking: boolean;
  check: UpdateCheck | null;
  lastCheck?: Date;
  progress: UpdateProgress | null;
  ready?: string;
}): UpdateState {
  const p = s.progress;
  if (s.checking) return { kind: 'checking' };
  if (p?.state === 'failed') return { kind: 'failed', reason: p.error };
  if (p?.state === 'downloading') {
    const percent = p.total && p.total > 0 ? Math.floor(((p.done ?? 0) * 100) / p.total) : 0;
    return { kind: 'downloading', percent };
  }
  // The installed copy's installer has the file and closes KnightLoader.
  if (p?.state === 'starting' || p?.state === 'started') return { kind: 'downloading', percent: 100 };
  // A downloaded version outranks the offer of it: it only waits for the next start.
  if (s.ready) return { kind: 'ready', version: shownVersion(s.ready) };
  if (!s.check) return { kind: 'unchecked' };
  if (!s.check.checked) return { kind: 'failed' };
  if (s.check.available) return { kind: 'available', version: shownVersion(s.check.latest ?? '') };
  return { kind: 'current', lastCheck: s.lastCheck };
}

/**
 * VersionCard is KnightLoader with its version and where its updates stand.
 * While the switch is on, opening the page asks GitHub once; the desktop
 * build also says there whether a downloaded version waits for its next
 * start. Update now on the desktop build fetches the new version at once: the
 * copy installed for all users starts the new installer, any other copy puts
 * it in place, and Restart for it then starts it.
 */
export function VersionCard({ deployment, hue }: { deployment: DeploymentInfo; hue: number }) {
  const { t, lang } = useT();
  const { cfg, patch } = useDraft();
  const news = useWhatsNew();
  const [version, setVersion] = useState('');
  const [check, setCheck] = useState<UpdateCheck | null>(null);
  const [lastCheck, setLastCheck] = useState<Date | undefined>();
  const [checking, setChecking] = useState(false);
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [announced, setAnnounced] = useState<string | undefined>();
  const mounted = useRef(true);
  const route = routeOf(deployment);
  const updatesItself = route === 'install';

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
    if (!mounted.current) return;
    setCheck(next);
    setLastCheck(new Date());
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
  }, []);

  useEffect(() => {
    mounted.current = true;
    // An update started before the page opened is still shown.
    if (updatesItself) {
      void fetchUpdateProgress()
        .then((p) => (p.state ? follow(p) : undefined))
        .catch(() => {});
    }
    const off = onUpdateReady(setAnnounced);
    return () => {
      mounted.current = false;
      off();
    };
  }, [updatesItself, follow]);

  // Once on opening while the switch is on, never before the stored switch
  // has resolved and not on every settings edit.
  useEffect(() => {
    if (cfg.autoUpdate) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.autoUpdate]);

  const onRestart = useCallback(async () => {
    try {
      await restartIntoUpdate();
    } catch (e) {
      setProgress({ state: 'failed', error: errorText(e) });
    }
  }, []);

  const onUpdate = useCallback(async () => {
    try {
      await follow(await installUpdate());
    } catch (e) {
      setProgress({ state: 'failed', error: errorText(e) });
    }
  }, [follow]);

  const ready = updatesItself
    ? (announced ?? check?.ready ?? (progress?.state === 'ready' ? progress.version : undefined))
    : undefined;
  const state = stateOf({ checking, check, lastCheck, progress, ready });
  // While a newer release is out, What's new is its notes, which only its
  // release page has yet.
  const onWhatsNew = () => {
    if (state.kind === 'available') openExternal(check?.url || `${REPO_URL}/releases/latest`);
    else openWhatsNew();
  };
  const flatpak = !!deployment.updatesFromStore;

  return (
    <InfoVersionCard
      hue={hue}
      data-new="updates-card"
      text={versionCardText(
        t,
        lang,
        flatpak ? t('settings.look.updatesAutoUpdateStoreHint') : t('settings.look.updatesAutoUpdateContainerHint'),
      )}
      name="KnightLoader"
      version={version}
      repoUrl={REPO_URL}
      route={route}
      state={state}
      onCheck={() => void onCheck()}
      onUpdate={() => void onUpdate()}
      onRestart={() => void onRestart()}
      guideUrl={`${MANUAL_URL}installing/${flatpak ? '#the-flatpak' : '#updating-the-container'}`}
      onWhatsNew={onWhatsNew}
      unseen={news?.unseen.length ?? 0}
      autoUpdate={cfg.autoUpdate}
      onAutoUpdateChange={(on) => patch({ autoUpdate: on })}
    />
  );
}
