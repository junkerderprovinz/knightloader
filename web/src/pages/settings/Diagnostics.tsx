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

// The diagnostics page shows what this build is and what it runs on, with a
// button that saves the same bundle to a file for a bug report. The download
// fetches a fresh bundle, since the log lines and goroutine count keep moving.
//
// PENDING holds the English strings until the catalogue has them; the lookup
// asks the catalogue first.
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

/** deploymentLabel translates buildinfo.Deployment, falling back to the raw value. */
function deploymentLabel(cx: ReturnType<typeof useCx>, raw: string): string {
  if (raw === 'container') return cx('settings.diagnostics.deployment.container');
  if (raw === 'desktop') return cx('settings.diagnostics.deployment.desktop');
  return raw;
}

/** fileStamp is a sortable UTC stamp for the file name. */
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
          {/* Program names need no translation, and as literals they stay out
              of check-settings-search.mjs's scan of label= props. */}
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

      {/* The hues number the cards 0..7 in draw order; renumber when adding one. */}
      <StartupReportCard hue={1} report={data.startup} />

      <SelfTestCard hue={2} />
      <ProxyCheckCard hue={3} />

      {/* logHint speaks of "the log file below", so the order is part of the copy. */}
      <LogViewerCard hue={4} />
      <LogFileCard hue={5} capacity={data.logCapacity} />

      <MaintenanceCard hue={6} />

      {/* Fetches its own reading, since people keep it open while they change
          the run command. */}
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
