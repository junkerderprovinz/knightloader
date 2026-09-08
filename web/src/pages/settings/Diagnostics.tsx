import { useCallback, useState } from 'react';
import { type Diagnostics as DiagnosticsBundle, fetchDiagnostics } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useResource } from '../../lib/useResource';
import { Button, Card, ErrorCard, LoadingCard, SectionTitle } from '../../components/ui';
import { IconDownloads } from '../../lib/icons';
import { LogFileCard } from './diagnostics/LogFileCard';
import { LogViewerCard } from './diagnostics/LogViewerCard';
import { MaintenanceCard } from './diagnostics/Maintenance';
import { OwnershipCard } from './diagnostics/Ownership';
import { ProxyCheckCard } from './diagnostics/ProxyCheck';
import { SelfTestCard } from './diagnostics/SelfTest';
import { StartupReportCard } from './diagnostics/StartupReport';

/**
 * The diagnostics page: what this build is, what it is running on, and its
 * own recent log output - one live preview and one button that saves the same
 * document to a file, for attaching to a bug report.
 *
 * The ONE card this component still draws is not part of the settings draft
 * (context.tsx's useDraft): there is nothing to save on it, only something to
 * read and, on demand, write to a file. The seven cards below it live in their
 * own files under diagnostics/ and split three ways - the start report, the
 * self-test, the reverse-proxy check and the ownership strip are readings and
 * write nothing at all; the log viewer holds its own filters and cursor; and
 * the log-file and maintenance cards are the exceptions that carry real
 * settings fields and patch the draft like any other page. Each says which it
 * is in its own file.
 * The two fetches - the preview on mount and the one right before a download -
 * are deliberately separate calls rather than one cached response, because the
 * whole point of the log lines and the goroutine count is that they keep
 * moving; a bundle built from whatever the page happened to load with would
 * be stale the moment something new gets logged.
 *
 * The strings this page needs are not in en.ts yet - locale files are one
 * writer's lane per wave (10F, phase 3 of this one, same arrangement
 * Captcha.tsx and Connections.tsx already use), and the lookup below asks the
 * real catalogue first, so the day these keys land it stops being consulted.
 */
const PENDING = {
  'settings.diagnostics.subtitle':
    'What this build is, what it is running on, and its own recent log output - for attaching to a bug report.',
  'settings.diagnostics.systemTitle': 'System information',
  'settings.diagnostics.version': 'Version',
  'settings.diagnostics.deployment': 'Build',
  'settings.diagnostics.deployment.container': 'Container',
  'settings.diagnostics.deployment.desktop': 'Desktop',
  'settings.diagnostics.goVersion': 'Go',
  'settings.diagnostics.platform': 'Platform',
  'settings.diagnostics.goroutines': 'Goroutines',
  'settings.diagnostics.download': 'Download diagnostics bundle',
  'settings.diagnostics.downloading': 'Preparing…',
  'settings.diagnostics.downloadHint':
    'A JSON file with the fields above, your settings with every password removed, and the log lines below.',
  'settings.diagnostics.downloadFailed': 'Could not build the bundle: {error}',
  'settings.diagnostics.loadFailed': 'Could not load diagnostics. Is the server reachable?',
  'settings.diagnostics.toolsMissing': 'not found',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

/**
 * The raw "container"/"desktop" the server sends (internal/buildinfo.Deployment)
 * translated for display, with the raw value itself as the fallback - a third
 * deployment kind a later wave adds must still show up as something rather
 * than as a blank cell.
 */
function deploymentLabel(cx: ReturnType<typeof useCx>, raw: string): string {
  if (raw === 'container') return cx('settings.diagnostics.deployment.container');
  if (raw === 'desktop') return cx('settings.diagnostics.deployment.desktop');
  return raw;
}

/** A JSON-safe, sortable filename stamp - not fmtDate, which is locale-formatted for reading, not for a filename. */
function fileStamp(): string {
  return new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z');
}

function saveJSON(doc: unknown, filename: string): void {
  const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export function Diagnostics() {
  const { t } = useT();
  const cx = useCx();
  const { data, failed, loading, reload } = useResource<DiagnosticsBundle>(fetchDiagnostics);
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState('');

  async function onDownload() {
    setError('');
    setDownloading(true);
    try {
      // Fetched fresh rather than reusing `data`: see the page's own doc
      // comment above for why a bundle built from a stale preview defeats
      // the point of shipping live log lines and a goroutine count at all.
      const fresh = await fetchDiagnostics();
      saveJSON(fresh, `knightloader-diagnostics-${fileStamp()}.json`);
    } catch (e) {
      setError(cx('settings.diagnostics.downloadFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setDownloading(false);
    }
  }

  if (loading) return <LoadingCard label={t('common.loading')} />;
  if (failed || !data) {
    return <ErrorCard message={cx('settings.diagnostics.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle hint={cx('settings.diagnostics.subtitle')}>{cx('settings.diagnostics.systemTitle')}</SectionTitle>

        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label={cx('settings.diagnostics.version')} value={data.version} />
          <Stat label={cx('settings.diagnostics.deployment')} value={deploymentLabel(cx, data.deployment)} />
          <Stat label={cx('settings.diagnostics.goVersion')} value={data.goVersion} />
          <Stat label={cx('settings.diagnostics.platform')} value={`${data.os}/${data.arch}`} />
          <Stat label={cx('settings.diagnostics.goroutines')} value={String(data.goroutines)} />
          {/* Plain string literals, not catalogue keys, and deliberately:
              "yt-dlp" and "ffmpeg" are program names, the same word in
              every locale (Resolvers.tsx makes the same argument for codec
              names). It also keeps them out of check-settings-search.mjs's
              forward pass, which scans every label= in a settings page. */}
          <Stat label="yt-dlp" value={data.mediaTools?.ytdlp?.version || cx('settings.diagnostics.toolsMissing')} />
          <Stat label="ffmpeg" value={data.mediaTools?.ffmpeg?.version || cx('settings.diagnostics.toolsMissing')} />
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <Button onClick={onDownload} disabled={downloading} icon={<IconDownloads width={16} height={16} />}>
            {downloading ? cx('settings.diagnostics.downloading') : cx('settings.diagnostics.download')}
          </Button>
          <span className="text-[11px] text-carbon-textMuted">{cx('settings.diagnostics.downloadHint')}</span>
        </div>
        {error && <span className="text-sm text-statusFail">{error}</span>}
      </Card>

      {/* THE HUES BELOW ARE A SEQUENCE, 0..7 IN DRAW ORDER, AND NOTHING ELSE.
          The palette position belongs to the page's card ORDER (ui.tsx's Card),
          so a badge sequence that jumps reads as a bug. Five separate waves
          added a card to this page at once, each numbering against the file as
          it stood when they started; the rule that survives every one of those
          merges is this one, so the whole page is renumbered here rather than
          any single card's suggestion being taken literally. Add a card, and
          renumber from the top again. */}

      {/* Directly under the system card, because it answers the next question
          that card raises: this is what the build it just named actually found
          when it started. It reads the bundle the page already loaded rather
          than fetching again - two ways of reading one document are two things
          that can disagree. */}
      <StartupReportCard hue={1} report={data.startup} />

      {/* The two self-test cards sit between the system readout and the log,
          because that is the order somebody debugging reads the page in: what
          this build is, then what it can find wrong with itself, then the raw
          lines. */}
      <SelfTestCard hue={2} />
      <ProxyCheckCard hue={3} />

      {/* The reading first, then the setting that keeps it: somebody opens this
          page to READ the log, and the file card is the "and you can keep these"
          that follows. logHint's own wording says "the log file below", so the
          order is part of the copy and not only of the layout.

          `capacity` is handed down rather than fetched again: this component
          already has it out of the diagnostics bundle, and a second copy of the
          ring's own number is a second thing to keep in step. */}
      <LogViewerCard hue={4} />
      <LogFileCard hue={5} capacity={data.logCapacity} />

      <MaintenanceCard hue={6} />

      {/* Who this instance writes files as. It fetches its own document rather
          than reading `data.ownership` from the bundle above, and the reason is
          the same one the two fetches on this page already have: the bundle is
          a snapshot taken on mount, and this card is the one somebody keeps
          open while they change a run command in another window. It draws
          nothing at all if its request fails, so a side reading can never
          replace the diagnostics somebody came here for. */}
      <OwnershipCard hue={7} />
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] text-carbon-textMuted">{label}</span>
      <span className="glim-num text-sm text-carbon-text" dir="ltr">
        {value}
      </span>
    </div>
  );
}
