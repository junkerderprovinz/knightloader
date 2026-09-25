import { useState } from 'react';
import { type Diagnostics as DiagnosticsBundle, fetchDiagnostics } from '../../lib/api';
import { useT } from '../../lib/i18n';
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

/** deploymentLabel translates buildinfo.Deployment, falling back to the raw value. */
function deploymentLabel(t: ReturnType<typeof useT>['t'], raw: string): string {
  if (raw === 'container') return t('settings.diagnostics.deployment.container');
  if (raw === 'desktop') return t('settings.diagnostics.deployment.desktop');
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
      setError(t('settings.diagnostics.downloadFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setDownloading(false);
    }
  }

  if (loading) return <LoadingCard label={t('common.loading')} />;
  if (failed || !data) {
    return <ErrorCard message={t('settings.diagnostics.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.diagnostics.subtitle')}>{t('settings.diagnostics.systemTitle')}</SectionTitle>

        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label={t('settings.diagnostics.version')} value={data.version} />
          <Stat label={t('settings.diagnostics.deployment')} value={deploymentLabel(t, data.deployment)} />
          <Stat label={t('settings.diagnostics.goVersion')} value={data.goVersion} />
          <Stat label={t('settings.diagnostics.platform')} value={`${data.os}/${data.arch}`} />
          <Stat label={t('settings.diagnostics.goroutines')} value={String(data.goroutines)} />
          {/* Program names need no translation, and as literals they stay out
              of check-settings-search.mjs's scan of label= props. */}
          <Stat label="yt-dlp" value={data.mediaTools?.ytdlp?.version || t('settings.diagnostics.toolsMissing')} />
          <Stat label="ffmpeg" value={data.mediaTools?.ffmpeg?.version || t('settings.diagnostics.toolsMissing')} />
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <Button
            onClick={onDownload}
            disabled={downloading}
            icon={<IconDownloads width={16} height={16} />}
            hint={t('settings.diagnostics.downloadHint')}
          >
            {downloading ? t('settings.diagnostics.downloading') : t('settings.diagnostics.download')}
          </Button>
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
