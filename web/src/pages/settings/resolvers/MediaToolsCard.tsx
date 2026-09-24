import { useCallback, useEffect, useState } from 'react';
import { Button, Card, FieldGroup, InfoBubble, SectionTitle, ToggleRow, useTooltip } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  fetchMediaTools,
  fetchYtdlpLatest,
  revertYtdlp,
  updateYtdlp,
  type MediaTool,
  type MediaToolsStatus,
  type YtdlpLatest,
} from '../../../lib/api';
import { useDraft } from '../context';
import { ModuleToggle } from '../ModuleToggle';

// The media tools card shows the yt-dlp and ffmpeg a media download runs, and
// fetches a newer yt-dlp than the image carries when a site breaks between
// image builds. The fetch is always a button press, since yt-dlp ships
// regressions. A fetched copy never moves again, so the card warns when it has
// fallen behind the image's own (Status.shadowed).

const SOURCE_KEYS: Record<string, TranslationKey> = {
  managed: 'settings.resolvers.toolsFrom.managed',
  env: 'settings.resolvers.toolsFrom.env',
  path: 'settings.resolvers.toolsFrom.path',
  none: 'settings.resolvers.toolsFrom.none',
};

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

export function MediaToolsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [latest, setLatest] = useState<YtdlpLatest | null>(null);
  const [checking, setChecking] = useState(false);
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
      /* A failure here means the server is unreachable, which the page shows. */
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
    try {
      setLatest(await fetchYtdlpLatest());
    } catch (e) {
      // A GitHub refusal still answers 200, so this is the request failing.
      setLatest({ checked: false, compare: 'unknown', detail: String(e).replace(/^(Error|ApiError):\s*/, '') });
    } finally {
      setChecking(false);
    }
  }, []);

  // Checks once the stored switch has resolved, never before.
  useEffect(() => {
    if (cfg.ytdlpVersionCheck) void onCheck();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fires when the
    // stored switch resolves, not on every settings edit.
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
      setFailed(String(e).replace(/^(Error|ApiError):\s*/, ''));
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
      setFailed(String(e).replace(/^(Error|ApiError):\s*/, ''));
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

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.resolvers.toolsHint')}>{t('settings.resolvers.toolsTitle')}</SectionTitle>
      <ModuleToggle id="ytdlp" />

      <div className="glim-well p-0">
        <ul className="flex flex-col">
          <ToolRow name="yt-dlp" tool={ytdlp} missingHint={t('settings.resolvers.toolsYtdlpMissingHint')} showSource />
          <ToolRow name="ffmpeg" tool={tools?.ffmpeg} missingHint={t('settings.resolvers.toolsFfmpegMissingHint')} />
          <ToolRow name="ffprobe" tool={tools?.ffprobe} missingHint={t('settings.resolvers.toolsFfmpegMissingHint')} last />
        </ul>
      </div>

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
        label={t('settings.resolvers.toolsAutoCheck')}
        hint={t('settings.resolvers.toolsAutoCheckHint')}
      />

      {/* FieldGroup, because a label would pass clicks to the first button. */}
      <FieldGroup label={t('settings.resolvers.toolsActions')} hint={t('settings.resolvers.toolsActionsHint')}>
        {/* Revert, check, fetch: the forward action sits last. The JSX order
            sets it, so the row mirrors in right-to-left languages. */}
        <div className="flex flex-wrap items-center gap-3">
          {managed && (
            <Button kind="ghost" disabled={busy} onClick={() => void onRevert()}>
              {reverting ? t('settings.resolvers.toolsReverting') : t('settings.resolvers.toolsRevert')}
            </Button>
          )}

          <Button kind="secondary" disabled={busy} onClick={() => void onCheck()}>
            {checking ? t('settings.resolvers.toolsChecking') : t('settings.resolvers.toolsCheck')}
          </Button>

          {/* Only after a check found a tag, so nobody fetches a version they
              were not shown. */}
          {latest?.checked && latest.tag && (
            <Button kind="primary" disabled={busy} onClick={() => void onFetch()}>
              {fetching ? t('settings.resolvers.toolsFetching') : t('settings.resolvers.toolsFetch')}
            </Button>
          )}
        </div>

        {/* The fetch came after the check, so its outcome wins. */}
        {failed && <p className="mt-2 text-sm text-statusFail">{t('settings.resolvers.toolsFetchFailed', { error: failed })}</p>}
        {!failed && fetched && (
          <p className="mt-2 text-sm text-statusOk">{t('settings.resolvers.toolsFetched', { version: fetched })}</p>
        )}
        {!failed && !fetched && reverted && (
          <p className="mt-2 text-sm text-statusOk">{t('settings.resolvers.toolsReverted', { path: reverted })}</p>
        )}
        {!failed && !fetched && !reverted && latest && (
          <p className={`mt-2 text-sm ${latest.checked ? 'text-carbon-textSub' : 'text-statusFail'}`}>
            {checkLine(t, latest, ytdlp?.version ?? '')}
          </p>
        )}
      </FieldGroup>
    </Card>
  );
}

/**
 * ToolRow shows one program's version, source and path, with an (i) only when
 * it is missing.
 */
function ToolRow({
  name,
  tool,
  missingHint,
  showSource = false,
  last = false,
}: {
  name: string;
  tool?: MediaTool;
  missingHint: string;
  showSource?: boolean;
  last?: boolean;
}) {
  const { t } = useT();
  const found = Boolean(tool?.found);
  // The whole path, where the row truncates it.
  const pathTip = useTooltip<HTMLSpanElement>(tool?.path);
  return (
    <li
      className={`flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 ${
        last ? '' : 'border-b border-carbon-border/60'
      }`}
    >
      <span className="font-mono text-sm text-carbon-text" dir="ltr">
        {name}
      </span>
      <span className="glim-num text-sm text-carbon-text" dir="ltr">
        {found ? tool?.version || '' : t('settings.resolvers.toolsNotFound')}
      </span>
      {found && <span className="glim-eyebrow">{t('settings.resolvers.toolsInUse')}</span>}
      {showSource && tool?.source && SOURCE_KEYS[tool.source] && (
        <span className="text-xs text-carbon-textMuted">{t(SOURCE_KEYS[tool.source])}</span>
      )}
      {tool?.path && (
        <span className="ms-auto truncate font-mono text-xs text-carbon-textMuted" dir="ltr" {...pathTip.triggerProps}>
          {tool.path}
        </span>
      )}
      {pathTip.node}
      {!found && <InfoBubble className={tool?.path ? '' : 'ms-auto'} tip={missingHint} />}
    </li>
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
