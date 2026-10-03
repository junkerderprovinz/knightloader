import { useRef, useState } from 'react';
import { Button, Card, Field, InfoBubble, SectionTitle } from '../../../components/ui';
import { PathInput } from '../../../components/FolderPicker';
import { IconUpload } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import {
  ApiError,
  applyJDImport,
  readJDImportFile,
  readJDImportPath,
  type JDImportPreview,
  type JDImportReport,
} from '../../../lib/api';
import { JDImportPreviewDialog, itemName, reasonText } from './JDImportPreview';

/**
 * JDImportCard moves an install over from JDownloader 2. Its cfg folder comes
 * in as an uploaded zip or as a path on the server, the preview asks what to
 * take over, and the report under the buttons says what came and what stayed
 * behind. The server holds the preview's passwords; the browser never sees one.
 */
export function JDImportCard({ hue }: { hue: number }) {
  const { t } = useT();
  const fileInput = useRef<HTMLInputElement>(null);
  const [path, setPath] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');
  const [shake, setShake] = useState(0);
  const [preview, setPreview] = useState<JDImportPreview | null>(null);
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState('');
  const [report, setReport] = useState<{ preview: JDImportPreview; report: JDImportReport; ticked: number } | null>(null);
  const [done, setDone] = useState(0);

  async function read(load: () => Promise<JDImportPreview>) {
    setBusy(true);
    setFailure('');
    setReport(null);
    try {
      setPreview(await load());
    } catch (e) {
      setFailure(refusalText(t, e));
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  async function apply(ids: string[]) {
    if (!preview) return;
    setApplying(true);
    setApplyError('');
    try {
      const res = await applyJDImport(preview.token, ids);
      setReport({ preview, report: res, ticked: ids.length });
      setPreview(null);
      setDone((n) => n + 1);
    } catch (e) {
      setApplyError(refusalText(t, e));
    } finally {
      setApplying(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.jdimport.cardHint')}>{t('settings.jdimport.cardTitle')}</SectionTitle>

      <div className="flex flex-col gap-2">
        <span className="flex items-center text-sm font-medium text-carbon-text">
          {t('settings.jdimport.uploadLabel')}
          <InfoBubble tip={t('settings.jdimport.uploadText')} />
        </span>
        <input
          ref={fileInput}
          type="file"
          accept="application/zip,.zip"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            // Cleared so picking the same file again fires a change event.
            e.target.value = '';
            if (f) void read(() => readJDImportFile(f));
          }}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconUpload width={16} height={16} />}
            onClick={() => fileInput.current?.click()}
            disabled={busy}
            confirm={done}
          >
            {t('settings.jdimport.uploadButton')}
          </Button>
        </div>
      </div>

      <div className="flex flex-col gap-2 border-t border-carbon-border/60 pt-4">
        <Field label={t('settings.jdimport.pathLabel')} hint={t('settings.jdimport.pathHint')}>
          <PathInput value={path} onValue={setPath} title={t('settings.jdimport.pathLabel')} />
        </Field>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={hue}
            kind="secondary"
            onClick={() => void read(() => readJDImportPath(path.trim()))}
            disabled={busy || path.trim() === ''}
            shake={shake}
          >
            {busy ? t('settings.jdimport.reading') : t('settings.jdimport.readButton')}
          </Button>
        </div>
      </div>

      {failure && <span className="text-sm text-statusFail">{failure}</span>}
      {report && <Report {...report} />}

      {preview && (
        <JDImportPreviewDialog
          preview={preview}
          busy={applying}
          error={applyError}
          onApply={(ids) => void apply(ids)}
          onClose={() => {
            if (applying) return;
            setPreview(null);
            setApplyError('');
          }}
        />
      )}
    </Card>
  );
}

/** Report says what came over, and names everything that did not, with why. */
function Report({ preview, report, ticked }: { preview: JDImportPreview; report: JDImportReport; ticked: number }) {
  const { t } = useT();
  const byId = new Map(preview.items.map((it) => [it.id, it]));
  const failed = new Set(report.failed.map((f) => f.id));
  const lines: string[] = [];
  for (const f of report.failed) {
    const it = byId.get(f.id);
    lines.push(t('settings.jdimport.line', { name: it ? itemName(t, it) : f.id, reason: reasonText(t, f.reason) }));
  }
  for (const it of preview.items) {
    if (it.blocked && !failed.has(it.id)) {
      lines.push(t('settings.jdimport.line', { name: itemName(t, it), reason: reasonText(t, it.blocked) }));
    }
  }
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-sm text-statusOk">
        {t('settings.jdimport.done', { n: report.imported.length, total: ticked })}
      </span>
      {report.links > 0 && (
        <span className="text-sm text-carbon-text">
          {report.links === 1 ? t('settings.jdimport.doneLinksOne') : t('settings.jdimport.doneLinks', { n: report.links })}
        </span>
      )}
      {report.filterStops && <span className="text-sm text-statusWarn">{t('settings.jdimport.filterStops')}</span>}
      {lines.length > 0 && (
        <>
          <span className="mt-1 text-sm text-carbon-text">{t('settings.jdimport.leftBehind')}</span>
          <ul className="flex max-h-64 min-w-0 flex-col gap-0.5 overflow-y-auto ps-4 text-[11px] text-carbon-textMuted">
            {lines.map((line, i) => (
              <li key={i} className="list-disc break-words">
                {line}
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

/** refusalText words a refused read or apply, translating the codes this card knows. */
function refusalText(t: ReturnType<typeof useT>['t'], e: unknown): string {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 'jdimport.noConfig':
        return t('settings.jdimport.noConfig');
      case 'jdimport.notZip':
        return t('settings.jdimport.notZip');
      case 'jdimport.expired':
        return t('settings.jdimport.expired');
      case 'jdimport.unknown':
        return t('settings.jdimport.unknown');
      case 'jdimport.replaced':
        return t('settings.jdimport.replaced');
      case 'jdimport.pathMissing':
        return t('settings.jdimport.pathMissing', { path: String(e.params?.path ?? '') });
    }
  }
  return t('settings.jdimport.failed', { error: String(e).replace(/^(Error|ApiError):\s*/, '') });
}
