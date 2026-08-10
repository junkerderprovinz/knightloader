import { useCallback, useRef, useState } from 'react';
import {
  type DeploymentInfo,
  BACKUP_DOWNLOAD_URL,
  fetchDeploymentInfo,
  requestQuit,
  requestRestart,
  uploadRestore,
} from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useResource } from '../../lib/useResource';
import { Button, Card, ErrorCard, LoadingCard, Modal, SectionTitle } from '../../components/ui';
import { IconDownloads, IconRetry, IconSignOut } from '../../lib/icons';

/**
 * The system page: quit/restart, and backup/restore - the four routes
 * build-plan.md's Wave 10 (10D) shipped with no frontend at all, found by
 * that wave's own adversarial review ("a complete backend with zero
 * frontend callers... backup, restore, quit and restart are unreachable by
 * any user"). Reachable only from here on purpose: none of the four belong
 * on a page anyone lands on by accident, unlike a settings toggle.
 *
 * The strings this page needs are not in en.ts yet - locale files are one
 * writer's lane per wave, and the lookup below asks the real catalogue
 * first, so the day these keys land it stops being consulted. Same
 * arrangement as Diagnostics.tsx (10C) and Schedule.tsx (10A).
 */
const PENDING = {
  'settings.system.subtitle': 'Quit, restart, and back up or restore this instance’s data.',
  'settings.system.deployment.container': 'Container',
  'settings.system.deployment.desktop': 'Desktop',
  'settings.system.lifecycleTitle': 'Quit & restart',
  'settings.system.quit': 'Quit',
  'settings.system.restart': 'Restart',
  'settings.system.quitConfirmTitle': 'Quit KnightLoader?',
  'settings.system.restartConfirmTitle': 'Restart KnightLoader?',
  'settings.system.quitConfirmBody':
    'In-flight work is drained first, then the process exits. {note}',
  'settings.system.confirmCancel': 'Cancel',
  'settings.system.confirmProceed': 'Confirm',
  'settings.system.unavailable': 'This build has no way to do this from the browser.',
  'settings.system.acting': 'Working…',
  'settings.system.shuttingDown':
    'Shutting down. If this instance comes back on its own, the page will reconnect once it does; otherwise close this tab.',
  'settings.system.actionFailed': 'Could not do this: {error}',
  'settings.system.backupTitle': 'Backup',
  'settings.system.backupHint':
    'Downloads the database and settings as one archive, including passwords - keep it somewhere private.',
  'settings.system.backupButton': 'Download backup',
  'settings.system.restoreTitle': 'Restore',
  'settings.system.restoreHint': 'Replaces this instance’s data with a previously downloaded backup.',
  'settings.system.restoreButton': 'Upload backup…',
  'settings.system.restoreConfirmTitle': 'Restore from backup?',
  'settings.system.restoreConfirmBody':
    'This replaces the current database and settings with the contents of “{name}”. This cannot be undone.',
  'settings.system.restoring': 'Validating and staging…',
  'settings.system.restoreFailed': 'Could not restore: {error}',
  'settings.system.restoreStaged': '{status}',
  'settings.system.loadFailed': 'Could not load. Is the server reachable?',
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

function deploymentLabel(cx: ReturnType<typeof useCx>, raw: string): string {
  if (raw === 'container') return cx('settings.system.deployment.container');
  if (raw === 'desktop') return cx('settings.system.deployment.desktop');
  return raw;
}

export function System() {
  const cx = useCx();
  const { data, failed, loading, reload } = useResource<DeploymentInfo>(fetchDeploymentInfo);

  const [confirmAction, setConfirmAction] = useState<'quit' | 'restart' | null>(null);
  const [acting, setActing] = useState(false);
  const [actionError, setActionError] = useState('');
  const [shuttingDown, setShuttingDown] = useState(false);

  const fileInput = useRef<HTMLInputElement>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreError, setRestoreError] = useState('');
  const [restoreStatus, setRestoreStatus] = useState('');

  async function confirmLifecycle() {
    if (!confirmAction) return;
    setActing(true);
    setActionError('');
    try {
      const res = confirmAction === 'quit' ? await requestQuit() : await requestRestart();
      setShuttingDown(true);
      void res;
    } catch (e) {
      setActionError(cx('settings.system.actionFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setActing(false);
      setConfirmAction(null);
    }
  }

  async function confirmRestore() {
    if (!pendingFile) return;
    setRestoring(true);
    setRestoreError('');
    try {
      const res = await uploadRestore(pendingFile);
      setRestoreStatus(res.status);
      if (res.restarting) setShuttingDown(true);
    } catch (e) {
      setRestoreError(cx('settings.system.restoreFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setRestoring(false);
      setPendingFile(null);
    }
  }

  if (loading) return <LoadingCard label="…" />;
  if (failed || !data) {
    return <ErrorCard message={cx('settings.system.loadFailed')} retry={reload} retryLabel="↻" />;
  }

  if (shuttingDown) {
    return (
      <Card className="flex flex-col gap-3">
        <p className="text-sm text-carbon-text">{cx('settings.system.shuttingDown')}</p>
      </Card>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <Card className="flex flex-col gap-2">
        <p className="text-sm text-carbon-textSub">{cx('settings.system.subtitle')}</p>
        <span className="glim-eyebrow w-fit">{deploymentLabel(cx, data.deployment)}</span>
      </Card>

      <SectionTitle>{cx('settings.system.lifecycleTitle')}</SectionTitle>
      <Card className="flex flex-col gap-3">
        <p className="text-[11px] text-carbon-textMuted">{data.note}</p>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            kind="danger"
            icon={<IconSignOut width={16} height={16} />}
            disabled={!data.canQuit || acting}
            onClick={() => setConfirmAction('quit')}
          >
            {cx('settings.system.quit')}
          </Button>
          <Button
            kind="secondary"
            icon={<IconRetry width={16} height={16} />}
            disabled={!data.canRestart || acting}
            onClick={() => setConfirmAction('restart')}
          >
            {cx('settings.system.restart')}
          </Button>
          {(!data.canQuit || !data.canRestart) && (
            <span className="text-[11px] text-carbon-textMuted">{cx('settings.system.unavailable')}</span>
          )}
        </div>
        {actionError && <span className="text-sm text-statusFail">{actionError}</span>}
      </Card>

      <SectionTitle>{cx('settings.system.backupTitle')}</SectionTitle>
      <Card className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <Button
            kind="secondary"
            icon={<IconDownloads width={16} height={16} />}
            onClick={() => {
              window.location.href = BACKUP_DOWNLOAD_URL;
            }}
          >
            {cx('settings.system.backupButton')}
          </Button>
          <span className="text-[11px] text-carbon-textMuted">{cx('settings.system.backupHint')}</span>
        </div>
      </Card>

      <SectionTitle>{cx('settings.system.restoreTitle')}</SectionTitle>
      <Card className="flex flex-col gap-3">
        <input
          ref={fileInput}
          type="file"
          accept="application/zip,.zip"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            // Cleared straight away, or picking the same file twice in a row
            // raises no change event and a second restore attempt after a
            // failed one silently does nothing - the same reason Rules.tsx's
            // import input already does this.
            e.target.value = '';
            if (f) setPendingFile(f);
          }}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Button kind="secondary" onClick={() => fileInput.current?.click()} disabled={restoring}>
            {cx('settings.system.restoreButton')}
          </Button>
          <span className="text-[11px] text-carbon-textMuted">{cx('settings.system.restoreHint')}</span>
        </div>
        {restoreError && <span className="text-sm text-statusFail">{restoreError}</span>}
        {restoreStatus && !restoreError && (
          <span className="text-sm text-statusOk">{cx('settings.system.restoreStaged', { status: restoreStatus })}</span>
        )}
      </Card>

      {confirmAction && (
        <Modal
          title={cx(confirmAction === 'quit' ? 'settings.system.quitConfirmTitle' : 'settings.system.restartConfirmTitle')}
          onClose={() => (acting ? undefined : setConfirmAction(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setConfirmAction(null)} disabled={acting}>
                {cx('settings.system.confirmCancel')}
              </Button>
              <Button kind="danger" onClick={() => void confirmLifecycle()} disabled={acting}>
                {acting ? cx('settings.system.acting') : cx('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {cx('settings.system.quitConfirmBody', { note: data.note })}
          </p>
        </Modal>
      )}

      {pendingFile && (
        <Modal
          title={cx('settings.system.restoreConfirmTitle')}
          onClose={() => (restoring ? undefined : setPendingFile(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setPendingFile(null)} disabled={restoring}>
                {cx('settings.system.confirmCancel')}
              </Button>
              <Button kind="danger" onClick={() => void confirmRestore()} disabled={restoring}>
                {restoring ? cx('settings.system.restoring') : cx('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {cx('settings.system.restoreConfirmBody', { name: pendingFile.name })}
          </p>
        </Modal>
      )}
    </div>
  );
}
