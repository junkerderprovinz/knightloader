import { useCallback, useState } from 'react';
import { type Diagnostics as DiagnosticsBundle, fetchDiagnostics } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useResource } from '../../lib/useResource';
import { Button, Card, ErrorCard, LoadingCard, SectionTitle } from '../../components/ui';
import { IconDownloads } from '../../lib/icons';

/**
 * The diagnostics page: what this build is, what it is running on, and its
 * own recent log output - one live preview and one button that saves the same
 * document to a file, for attaching to a bug report.
 *
 * Nothing here is part of the settings draft (context.tsx's useDraft): there
 * is nothing to save, only something to read and, on demand, write to a file.
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
  'settings.diagnostics.logTitle': 'Recent log lines',
  'settings.diagnostics.logHint': 'The last {n} lines this process has logged, oldest first. Nothing here is written to disk.',
  'settings.diagnostics.logEmpty': 'Nothing logged yet.',
  'settings.diagnostics.refresh': 'Refresh',
  'settings.diagnostics.loadFailed': 'Could not load diagnostics. Is the server reachable?',
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
      <Card className="flex flex-col gap-5">
        <SectionTitle hue={0}>{cx('settings.diagnostics.systemTitle')}</SectionTitle>
        <p className="text-sm text-carbon-textSub">{cx('settings.diagnostics.subtitle')}</p>

        <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
          <Stat label={cx('settings.diagnostics.version')} value={data.version} />
          <Stat label={cx('settings.diagnostics.deployment')} value={deploymentLabel(cx, data.deployment)} />
          <Stat label={cx('settings.diagnostics.goVersion')} value={data.goVersion} />
          <Stat label={cx('settings.diagnostics.platform')} value={`${data.os}/${data.arch}`} />
          <Stat label={cx('settings.diagnostics.goroutines')} value={String(data.goroutines)} />
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <Button onClick={onDownload} disabled={downloading} icon={<IconDownloads width={16} height={16} />}>
            {downloading ? cx('settings.diagnostics.downloading') : cx('settings.diagnostics.download')}
          </Button>
          <span className="text-[11px] text-carbon-textMuted">{cx('settings.diagnostics.downloadHint')}</span>
        </div>
        {error && <span className="text-sm text-statusFail">{error}</span>}
      </Card>

      <Card className="p-0">
        <div className="p-5 pb-0">
          <SectionTitle
            hue={1}
            hint={cx('settings.diagnostics.logHint', { n: data.logCapacity })}
            right={<Button kind="ghost" onClick={reload}>{cx('settings.diagnostics.refresh')}</Button>}
          >
            {cx('settings.diagnostics.logTitle')}
          </SectionTitle>
        </div>
        {data.logLines.length === 0 ? (
          <div className="p-5 text-sm text-carbon-textMuted">{cx('settings.diagnostics.logEmpty')}</div>
        ) : (
          // ltr regardless of interface direction, the same convention every
          // other path/URL/code cell in settings/ already uses (Access.tsx's
          // port list, Advanced.tsx's key table): log lines mix paths, hosts
          // and stack traces, none of which read correctly mirrored.
          <pre
            dir="ltr"
            className="max-h-96 overflow-auto whitespace-pre-wrap break-all p-4 font-mono text-[11px] leading-relaxed text-carbon-textSub"
          >
            {data.logLines.join('\n')}
          </pre>
        )}
      </Card>
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
