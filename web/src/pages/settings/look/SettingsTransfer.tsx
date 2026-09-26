import { useRef, useState } from 'react';
import { Button, Card, InfoBubble, Modal, SectionTitle, ToggleRow } from '../../../components/ui';
import { IconClose, IconDownloads, IconUpload } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import {
  ApiError,
  BACKUP_DOWNLOAD_URL,
  importSettings,
  settingsExportURL,
  uploadRestore,
  type SettingsExportDoc,
  type Settings,
} from '../../../lib/api';
import { useToast } from '../../../lib/toast';
import { withBase } from '../../../lib/basePath';
import { diffRows, parseExport, type TransferRow } from '../../../lib/settingsTransfer';
import { fetchSettingsSchema } from '../features';
import { useDraft } from '../context';
import { SettingsImportPreview } from './SettingsImportPreview';

/**
 * SettingsTransfer moves this box to another one in two ways. The archive
 * carries the whole install, restored wholesale at the next start. "Settings
 * only" carries settings.json, offered key by key and applied through
 * app.PatchSettings without a restart. Neither carries the logins, header
 * profiles, captcha keys, login password or user scripts, which live outside
 * settings.json; the title's (i) says so. A restore that restarts the server
 * reports through `onShutdown`.
 */
export function SettingsTransfer({ hue, onShutdown }: { hue: number; onShutdown: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const draft = useDraft();

  const archiveInput = useRef<HTMLInputElement>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreStatus, setRestoreStatus] = useState('');
  // The failure counter of the confirm button, so a repeated refusal shakes it again.
  const [restoreShake, setRestoreShake] = useState(0);
  // Restore pulses once a restore is staged; Import pulses once settings are
  // taken over and shakes on a file it cannot read.
  const [restored, setRestored] = useState(0);
  const [imported, setImported] = useState(0);
  const [importShake, setImportShake] = useState(0);

  const fileInput = useRef<HTMLInputElement>(null);

  // Off by default: a settings export is a file people send, and a missing
  // password after import is visible and easy to fix.
  const [withSecrets, setWithSecrets] = useState(false);

  const [pending, setPending] = useState<{ doc: SettingsExportDoc; rows: TransferRow[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [previewError, setPreviewError] = useState('');
  // What was taken over, which passwords are missing and which rules cannot fire.
  const [notice, setNotice] = useState<string[]>([]);
  const [failure, setFailure] = useState('');

  async function confirmRestore() {
    if (!pendingFile) return;
    setRestoring(true);
    try {
      const res = await uploadRestore(pendingFile);
      setRestoreStatus(res.status);
      setRestored((n) => n + 1);
      if (res.restarting) onShutdown();
      setPendingFile(null);
    } catch (e) {
      // The window stays open so the pressed button can shake.
      toast(t('settings.system.restoreFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setRestoreShake((n) => n + 1);
    } finally {
      setRestoring(false);
    }
  }

  async function choose(file: File) {
    setNotice([]);
    setFailure('');
    setPreviewError('');
    try {
      const doc = parseExport(await file.text());
      // The server's type table decides which keys are unknown, by the import
      // route's own rule.
      const schema = await fetchSettingsSchema();
      const stored = draft.cfg as unknown as Record<string, unknown>;
      setPending({ doc, rows: diffRows(doc, stored, schema) });
    } catch (e) {
      setFailure(t('settings.transfer.parseFailed', { reason: reasonOf(e) }));
      setImportShake((n) => n + 1);
    }
  }

  async function apply(keys: string[]) {
    if (!pending) return;
    setBusy(true);
    setPreviewError('');
    try {
      const res = await importSettings(pending.doc, keys);
      // Folded into both of the shell's copies, or the next autosave would send
      // the pre-import values back.
      draft.reseed(res.settings as Settings, res.applied);

      const lines = [t('settings.transfer.applied', { n: res.applied.length })];
      if (res.incomplete.length > 0) {
        lines.push(
          t('settings.transfer.incomplete', {
            fields: res.incomplete.map((code) => t(INCOMPLETE_LABEL[code] ?? 'settings.transfer.noPassword')).join(', '),
          }),
        );
      }
      if (res.ruleProblems > 0) lines.push(t('settings.transfer.rulesUncompiled', { n: res.ruleProblems }));
      setNotice(lines);
      setPending(null);
      setImported((n) => n + 1);
    } catch (e) {
      // The server's refusal names the failed check. Only the version guard is
      // translated, because it tells the reader what to do.
      if (e instanceof ApiError && e.code === 'transfer.tooNew') {
        setPreviewError(
          t('settings.transfer.tooNew', {
            version: String(e.params?.version ?? ''),
            running: String(e.params?.running ?? ''),
          }),
        );
      } else {
        setPreviewError(t('settings.transfer.applyFailed', { error: reasonOf(e) }));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.transfer.cardHint')}>{t('settings.transfer.cardTitle')}</SectionTitle>

      <div className="flex flex-col gap-2">
        <span className="flex items-center text-sm font-medium text-carbon-text">
          {t('settings.transfer.archiveLabel')}
          <InfoBubble tip={t('settings.transfer.archiveText')} />
        </span>
        <input
          ref={archiveInput}
          type="file"
          accept="application/zip,.zip"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            // Cleared so picking the same file again fires a change event.
            e.target.value = '';
            if (f) setPendingFile(f);
          }}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconDownloads width={16} height={16} />}
            onClick={() => {
              window.location.href = withBase(BACKUP_DOWNLOAD_URL);
            }}
          >
            {t('settings.system.backupButton')}
          </Button>
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconUpload width={16} height={16} />}
            onClick={() => archiveInput.current?.click()}
            disabled={restoring}
            confirm={restored}
          >
            {t('settings.system.restoreButton')}
          </Button>
        </div>
        {restoreStatus && (
          <span className="text-sm text-statusOk">{t('settings.system.restoreStaged', { status: restoreStatus })}</span>
        )}
      </div>

      <div className="flex flex-col gap-2 border-t border-carbon-border/60 pt-4">
        <span className="flex items-center text-sm font-medium text-carbon-text">
          {t('settings.transfer.settingsLabel')}
          <InfoBubble tip={t('settings.transfer.settingsText')} />
        </span>

        <ToggleRow
          label={t('settings.transfer.withSecrets')}
          hint={t('settings.transfer.withSecretsHint')}
          checked={withSecrets}
          onChange={setWithSecrets}
        />

        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = '';
            if (f) void choose(f);
          }}
        />

        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconDownloads width={16} height={16} />}
            onClick={() => {
              // Opened rather than fetched, so the browser owns the save dialog.
              window.location.href = settingsExportURL(withSecrets);
            }}
          >
            {t('settings.transfer.export')}
          </Button>
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconUpload width={16} height={16} />}
            onClick={() => fileInput.current?.click()}
            disabled={busy}
            shake={importShake}
            confirm={imported}
          >
            {t('settings.transfer.import')}
          </Button>
        </div>

        {failure && <span className="text-sm text-statusFail">{failure}</span>}
        {notice.map((line, i) => (
          <span key={i} className={i === 0 ? 'text-sm text-statusOk' : 'text-sm text-statusWarn'}>
            {line}
          </span>
        ))}
      </div>

      {pending && (
        <SettingsImportPreview
          doc={pending.doc}
          rows={pending.rows}
          busy={busy}
          error={previewError}
          onApply={(keys) => void apply(keys)}
          onClose={() => {
            if (busy) return;
            setPending(null);
            setPreviewError('');
          }}
        />
      )}

      {pendingFile && (
        <Modal
          title={t('settings.system.restoreConfirmTitle')}
          onClose={() => (restoring ? undefined : setPendingFile(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('settings.system.confirmCancel')}
                onClick={() => setPendingFile(null)}
                disabled={restoring}
              />
              <Button
                shake={restoreShake}
                kind="ghost"
                onClick={() => void confirmRestore()}
                disabled={restoring}
              >
                {restoring ? t('settings.system.restoring') : t('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('settings.system.restoreConfirmBody', { name: pendingFile.name })}</p>
        </Modal>
      )}
    </Card>
  );
}

/**
 * Labels for the incomplete-row codes the server sends. An unknown code falls
 * back to the generic "arrives without its password".
 */
const INCOMPLETE_LABEL: Record<
  string,
  | 'settings.transfer.incompleteReconnect'
  | 'settings.transfer.incompleteConnections'
  | 'settings.transfer.incompleteArchives'
  | 'settings.transfer.incompleteEventPrograms'
> = {
  'reconnect.password': 'settings.transfer.incompleteReconnect',
  'connections.password': 'settings.transfer.incompleteConnections',
  archivePasswords: 'settings.transfer.incompleteArchives',
  'eventPrograms.command': 'settings.transfer.incompleteEventPrograms',
};

/** reasonOf strips the class name a stringified Error puts in front. */
function reasonOf(e: unknown): string {
  return String(e).replace(/^(Error|ApiError|SyntaxError):\s*/, '');
}
