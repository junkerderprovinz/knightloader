import { useCallback, useEffect, useState } from 'react';
import { Button, Card, FieldGroup, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
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

/**
 * The two programs a media download actually runs, and the one operation that
 * changes which yt-dlp that is.
 *
 * WHY THIS CARD IS FIRST ON THE PAGE. Somebody who lands on the Resolvers page
 * is usually here because a link that worked last month stopped working, and an
 * out of date yt-dlp is by a wide margin the most common cause of that. The
 * quality strip below is a preference; this is the fact. Reading the version
 * before scrolling past nine cards of options is the whole point of the
 * position.
 *
 * WHY A FETCH BUTTON EXISTS AT ALL, given that both programs come from the
 * container image. Because the image is rebuilt on KnightLoader's schedule and
 * yt-dlp ships on its own, which is measured in days when a big site changes
 * something. The gap between "yt-dlp fixed it" and "the image that carries it
 * was rebuilt" is exactly the window this closes.
 *
 * WHY THERE IS NO "INSTALL IT AUTOMATICALLY" SWITCH, and there deliberately
 * never will be one here (jdp, 2026-09-08). Replacing the extractor unattended
 * silently changes what every download produces, and yt-dlp does ship
 * regressions. The version CHECK has a switch, off by default; the fetch is a
 * button somebody presses.
 *
 * THE ONE SENTENCE THIS CARD EXISTS TO BE ABLE TO SAY. A copy fetched into the
 * data directory never moves again, while the image's own yt-dlp moves forward
 * with every rebuild. So the operator who fetched once, eighteen months ago, to
 * fix a broken extractor is now running something OLDER than the container
 * ships, having "fixed" it. Status.shadowed is what makes that visible, and the
 * warning built from it is the difference between this feature helping in the
 * long run and quietly making things worse.
 */

/** The source word for a yt-dlp row, in the catalogue rather than inline. */
const SOURCE_KEYS: Record<string, TranslationKey> = {
  managed: 'settings.resolvers.toolsFrom.managed',
  env: 'settings.resolvers.toolsFrom.env',
  path: 'settings.resolvers.toolsFrom.path',
  none: 'settings.resolvers.toolsFrom.none',
};

/**
 * Orders two yt-dlp versions the way the Go side does (internal/mediatools's
 * CompareVersions), and for the same reason it is not a string comparison:
 * yt-dlp versions are dates, "2026.08.11.1" is a same-day rerelease of
 * "2026.08.11", and a lexical compare puts "2026.9.1" before "2026.08.11".
 *
 * Null when the two cannot be ordered at all - a source build's "2026.08.11.dev0",
 * an empty string, anything that is not a run of numbers. A comparison that
 * could not be made must never be drawn as an answer, which is why this returns
 * null rather than 0.
 *
 * Only used for the "your fetched copy is older than the system's" warning. The
 * check against GitHub is ordered by the server, which is the side that knows
 * what it asked.
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
  // Three separate outcome lines rather than one, because they answer three
  // different questions and the last one asked is the one worth showing: a
  // fetch that failed must not be wiped by a later successful check, and a
  // successful fetch must not sit above a stale "a newer one is published".
  const [fetched, setFetched] = useState('');
  const [failed, setFailed] = useState('');
  const [reverted, setReverted] = useState('');

  const load = useCallback(async () => {
    try {
      setTools(await fetchMediaTools());
    } catch {
      /* The card draws nothing rather than claiming anything: this route never
         calls out, so a failure here means the server is unreachable, and the
         page around it will already be saying so. */
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
      // The route answers 200 even when GitHub refused, carrying GitHub's own
      // sentence - so reaching here means the request itself failed, and there
      // is no better wording available than the one the browser gave.
      setLatest({ checked: false, compare: 'unknown', detail: String(e).replace(/^(Error|ApiError):\s*/, '') });
    } finally {
      setChecking(false);
    }
  }, []);

  // The opt-in half, fired once when the stored switch first resolves - never
  // on every render, and never before it has resolved, which would make an
  // outbound call on a page whose owner had switched it off. Same shape as the
  // update card's own auto-check on the Look page.
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
      // The version on the card has to be the one now running, and only the
      // status route knows that - the install rewired the resolver behind it.
      await load();
      // The check result is now about a release that IS installed, so the line
      // built from it would keep saying "a newer one is published".
      setLatest(null);
    } catch (e) {
      // The server's own sentence, which names the asset it tried, the
      // program's own failure and what to do about it. A key of ours here
      // would be a vaguer copy of a message that was written to be specific.
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
  // A record with no working copy behind it: quarantined by a virus scanner,
  // deleted by hand, left over from a data directory copied between machines.
  // Something else is running instead and the card has to say which.
  const managedBroken = Boolean(managed) && !managedRunning;
  // The slow failure: the fetched copy has fallen behind the one the machine
  // already had. Only claimed when the two versions can actually be ordered.
  const managedIsOlder =
    managedRunning &&
    Boolean(shadowed?.version) &&
    Boolean(ytdlp?.version) &&
    compareVersions(ytdlp?.version ?? '', shadowed?.version ?? '') === -1;

  const busy = checking || fetching || reverting;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.resolvers.toolsHint')}>{t('settings.resolvers.toolsTitle')}</SectionTitle>

      {/* The glim-well wrapper with a plain list inside, as the accounts table
          and the cookie jars list do it - never a nested Card, and never a Card
          per row. */}
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

      {/* FieldGroup, not Field: this caption sits over three buttons, and a
          <label> wrapping a button forwards its own clicks to the first
          control inside it - the same reason the preset host row below uses
          one. */}
      <FieldGroup label={t('settings.resolvers.toolsActions')} hint={t('settings.resolvers.toolsActionsHint')}>
        <div className="flex flex-wrap items-center gap-3">
          <Button kind="secondary" disabled={busy} onClick={() => void onCheck()}>
            {checking ? t('settings.resolvers.toolsChecking') : t('settings.resolvers.toolsCheck')}
          </Button>

          {/* Only after a check has come back with a tag. Nobody gets to fetch
              a version they were never shown, and "fetch" with no idea what is
              about to arrive is how somebody replaces a working extractor by
              accident. */}
          {latest?.checked && latest.tag && (
            <Button kind="primary" disabled={busy} onClick={() => void onFetch()}>
              {fetching ? t('settings.resolvers.toolsFetching') : t('settings.resolvers.toolsFetch')}
            </Button>
          )}

          {/* Only while there is something to go back from. */}
          {managed && (
            <Button kind="ghost" disabled={busy} onClick={() => void onRevert()}>
              {reverting ? t('settings.resolvers.toolsReverting') : t('settings.resolvers.toolsRevert')}
            </Button>
          )}
        </div>

        {/* One outcome line, and the order is the order in which the answers
            were given: what the fetch did beats what the check found, because
            the fetch happened afterwards. */}
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
 * One program, one line: what it is, what version it reports, where it came
 * from, and where it is. The info bubble appears only when the program is
 * missing, because that is the only state where an explanation changes what
 * somebody does next.
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
  return (
    <li
      className={`flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 ${
        last ? '' : 'border-b border-carbon-border/60'
      }`}
    >
      {/* ltr regardless of interface direction, the same convention every
          other program name, path and code cell in settings/ already uses. */}
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
        <span className="ms-auto truncate font-mono text-xs text-carbon-textMuted" dir="ltr" title={tool.path}>
          {tool.path}
        </span>
      )}
      {!found && <InfoBubble className={tool?.path ? '' : 'ms-auto'} tip={missingHint} />}
    </li>
  );
}

/** The sentence a finished check produces, one per answer the server can give. */
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
      // "unknown", and also the case where nothing is installed to compare
      // against. Saying nothing about which is newer is the whole reason this
      // state exists rather than being folded into "you are current".
      return t('settings.resolvers.toolsUnknown', { latest: tag, current });
  }
}
