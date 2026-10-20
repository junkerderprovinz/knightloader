import { useRef, useState } from 'react';
import { Button, Card, Modal, SectionTitle, TextInput } from '../../../components/ui';
import { IconClose, IconDownload, IconUpload } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { ApiError, BACKUP_DOWNLOAD_URL, uploadRestore } from '../../../lib/api';
import { useToast } from '../../../lib/toast';
import { withBase } from '../../../lib/basePath';
import { passwordPath, usePasswordFirst } from '../../../lib/passwordFirst';
import { SettingRow } from '../controls';
import { AutoBackupSection } from './AutoBackups';

/**
 * SettingsTransfer saves this box and puts a saved one back: one archive of
 * the whole install, restored wholesale at the next start. It does not carry
 * the logins, header profiles, captcha keys, login password or user scripts,
 * which live outside settings.json; the title's (i) says so. A restore that
 * restarts the server reports through `onShutdown`.
 */
export function SettingsTransfer({ hue, onShutdown }: { hue: number; onShutdown: () => void }) {
  const { t } = useT();
  const { toast } = useToast();

  const fileInput = useRef<HTMLInputElement>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  // Whether the chosen file is sealed, which is when another instance's app
  // key may be needed to open it.
  const [pendingSealed, setPendingSealed] = useState(false);
  const [sourceKey, setSourceKey] = useState('');
  const [restoring, setRestoring] = useState(false);
  const [restoreStatus, setRestoreStatus] = useState('');
  // The failure counter of the confirm button, so a repeated refusal shakes it again.
  const [restoreShake, setRestoreShake] = useState(0);
  // Upload pulses once a restore is staged.
  const [restored, setRestored] = useState(0);
  // A backup carries every stored password, which the server keeps back until
  // a login password is set.
  const locked = usePasswordFirst();

  async function choose(file: File) {
    setPendingSealed(new TextDecoder().decode(await file.slice(0, SEALED_MAGIC.length).arrayBuffer()) === SEALED_MAGIC);
    setSourceKey('');
    setPendingFile(file);
  }

  async function confirmRestore() {
    if (!pendingFile) return;
    setRestoring(true);
    try {
      const res = await uploadRestore(pendingFile, sourceKey);
      setRestoreStatus(res.status);
      setRestored((n) => n + 1);
      if (res.restarting) onShutdown();
      setPendingFile(null);
    } catch (e) {
      // The window stays open so the pressed button can shake.
      const refusal = e instanceof ApiError ? RESTORE_REFUSAL[e.code ?? ''] : undefined;
      const message = refusal
        ? t(refusal)
        : t('settings.system.restoreFailed', { error: String(e).replace(/^Error:\s*/, '') });
      toast(message, 'fail');
      setRestoreShake((n) => n + 1);
    } finally {
      setRestoring(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.transfer.cardHint')}>{t('settings.transfer.cardTitle')}</SectionTitle>

      <input
        ref={fileInput}
        type="file"
        accept="application/zip,.zip,.sealed"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          // Cleared so picking the same file again fires a change event.
          e.target.value = '';
          if (f) void choose(f);
        }}
      />

      <div className="flex flex-wrap items-center gap-3">
        <Button
          kind="secondary"
          icon={<IconDownload width={16} height={16} />}
          disabled={locked}
          onClick={() => {
            // Opened rather than fetched, so the browser owns the save dialog.
            window.location.href = withBase(BACKUP_DOWNLOAD_URL);
          }}
        >
          {t('settings.system.backupButton')}
        </Button>
        <Button
          kind="secondary"
          icon={<IconUpload width={16} height={16} />}
          onClick={() => fileInput.current?.click()}
          disabled={restoring}
          confirm={restored}
        >
          {t('settings.system.restoreButton')}
        </Button>
      </div>

      {restoreStatus && (
        <span className="text-sm text-statusOk">{t('settings.system.restoreStaged', { status: restoreStatus })}</span>
      )}
      {locked && (
        <span className="text-sm text-carbon-textSub">
          {t('settings.transfer.needsPassword', { path: passwordPath(t) })}
        </span>
      )}

      <AutoBackupSection hue={hue} locked={locked} />

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
          <p className="mt-2 text-sm text-carbon-text">{t('settings.system.restoreKeepsLogin')}</p>
          {pendingSealed && (
            <div className="mt-4">
              <SettingRow control="long" label={t('settings.system.restoreKeyLabel')} hint={t('settings.system.restoreKeyHint')}>
                <TextInput
                  type="password"
                  dir="ltr"
                  autoComplete="off"
                  value={sourceKey}
                  onChange={(e) => setSourceKey(e.target.value)}
                />
              </SettingRow>
            </div>
          )}
        </Modal>
      )}
    </Card>
  );
}

/** The first line of a sealed backup (internal/backup's sealedMagic). */
const SEALED_MAGIC = 'KnightLoader sealed backup 1\n';

/** The restore refusals that say what to do rather than what failed. */
const RESTORE_REFUSAL: Record<
  string,
  | 'settings.system.restoreKeyMissing'
  | 'settings.system.restoreKeyWrong'
  | 'settings.system.restoreKeyInvalid'
  | 'settings.system.restoreNewerSchema'
> = {
  'restore.appKeyMissing': 'settings.system.restoreKeyMissing',
  'restore.appKeyWrong': 'settings.system.restoreKeyWrong',
  'restore.appKeyInvalid': 'settings.system.restoreKeyInvalid',
  'restore.newerSchema': 'settings.system.restoreNewerSchema',
};
