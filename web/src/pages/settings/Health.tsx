import { useState } from 'react';
import { Button, Card, ErrorCard, SectionTitle } from '../../components/ui';
import { type Diagnostics, fetchDiagnostics } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { IconDownload } from '../../lib/icons';
import { useHealthReport } from '../../lib/useHealthReport';
import { useResource } from '../../lib/useResource';
import { saveDiagnostics } from './diagnostics/bundle';
import { LogCard } from './diagnostics/LogCard';
import { OwnerSection, Stat } from './diagnostics/Ownership';
import { StateCard } from './health/StateCard';

/**
 * Health is whether this instance works and what to hand over when it does
 * not: the state of its parts with the self-test and the start report, the
 * log, and what it runs on with the bundle for a bug report. Nothing here is
 * part of the settings draft except the log file's switches.
 */
export function Health() {
  const { report, failed } = useHealthReport();
  const bundle = useResource<Diagnostics>(fetchDiagnostics);

  return (
    <div data-new="health-diagnostics" className="flex flex-col gap-10">
      <StateCard hue={0} report={report} failed={failed} startup={bundle.data?.startup} />
      <LogCard hue={1} />
      <SystemCard hue={2} bundle={bundle} />
    </div>
  );
}

/** deploymentLabel translates buildinfo.Deployment, falling back to the raw value. */
function deploymentLabel(t: ReturnType<typeof useT>['t'], raw: string): string {
  if (raw === 'container') return t('settings.diagnostics.deployment.container');
  if (raw === 'desktop') return t('settings.diagnostics.deployment.desktop');
  return raw;
}

/**
 * SystemCard says what this build is and what it runs on, and saves the same
 * bundle to a file for a bug report. The versions of yt-dlp and ffmpeg are on
 * the Updates card, where they can be changed.
 */
function SystemCard({ hue, bundle }: { hue: number; bundle: ReturnType<typeof useResource<Diagnostics>> }) {
  const { t } = useT();
  const { data, failed, loading, reload } = bundle;
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState('');

  async function onDownload() {
    setError('');
    setDownloading(true);
    try {
      await saveDiagnostics();
    } catch (e) {
      setError(t('settings.diagnostics.downloadFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setDownloading(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.diagnostics.subtitle')}>
        {t('settings.diagnostics.systemReportTitle')}
      </SectionTitle>

      {loading ? (
        <span className="text-sm text-carbon-textMuted">{t('common.loading')}</span>
      ) : failed || !data ? (
        <ErrorCard
          nested
          message={t('settings.diagnostics.loadFailed')}
          retry={reload}
          retryLabel={t('common.retry')}
        />
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Stat label={t('settings.diagnostics.version')} value={data.version} />
          <Stat label={t('settings.diagnostics.deployment')} value={deploymentLabel(t, data.deployment)} />
          <Stat label={t('settings.diagnostics.platform')} value={`${data.os}/${data.arch}`} />
        </div>
      )}

      {/* Fetches its own reading, since people keep it open while they change
          the run command. */}
      <OwnerSection />

      <div className="flex flex-wrap items-center gap-3">
        <Button
          kind="secondary"
          data-new="bug-report-speed"
          onClick={onDownload}
          disabled={downloading}
          icon={<IconDownload width={16} height={16} />}
          hint={t('settings.diagnostics.downloadHint')}
        >
          {downloading ? t('settings.diagnostics.downloading') : t('settings.diagnostics.download')}
        </Button>
      </div>
      {error && <span className="text-sm text-statusFail">{error}</span>}
    </Card>
  );
}
