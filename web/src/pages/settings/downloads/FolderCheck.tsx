import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { checkFolderOwners, type FolderOwnerProbe, type FolderOwnerReport } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtDate } from '../../../lib/format';
import { Button, Card, SectionTitle, type ButtonVerdict } from '../../../components/ui';
import { IconCheck, IconClose } from '../../../lib/icons';
import { useDraft } from '../context';

// The folder check writes a probe file into each configured folder and
// compares its owner and mode with the folder's, which catches a share owned by
// another uid that settings.Validate lets through. It reports and prints the
// command to run, since the container cannot change its identity. It writes, so
// it runs on a button press or after a save that changed a folder, never on
// mount.

type Translate = ReturnType<typeof useT>['t'];

/**
 * owner formats "99:100 (nobody:users)", numbers first because they go into a
 * chown or --user, or just "1000:1000" when the ids have no names.
 */
function owner(uid: number, gid: number, user: string, group: string): string {
  if (!user && !group) return `${uid}:${gid}`;
  return `${uid}:${gid} (${user || uid}:${group || gid})`;
}

function roleLabel(t: Translate, role: string): string {
  switch (role) {
    case 'downloads':
      return t('settings.owner.role.downloads');
    case 'work':
      return t('settings.owner.role.work');
    case 'category':
      return t('settings.owner.role.category');
    case 'watch':
      return t('settings.owner.role.watch');
    default:
      return role;
  }
}

export function FolderCheckCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, dirty } = useDraft();
  const [report, setReport] = useState<FolderOwnerReport | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  // The failure counter of the check button, so a repeated failure shakes it again.
  const [shake, setShake] = useState(0);

  const run = useCallback(async () => {
    setError('');
    setRunning(true);
    try {
      const found = await checkFolderOwners();
      setReport(found);
      if (found.folders.some(unusable)) setShake((n) => n + 1);
    } catch (e) {
      setError(t('settings.owner.checkFailed', { error: String(e).replace(/^Error:\s*/, '') }));
      setShake((n) => n + 1);
    } finally {
      setRunning(false);
    }
  }, [t]);

  // The check after a save. A ref, seeded with the folders the page mounted
  // with, so opening the page writes nothing.
  const checked = useRef<string | null>(null);
  useEffect(() => {
    // Only once the save has landed, not on a half-typed path.
    if (dirty) return;
    const pair = `${cfg.downloadDir}\0${cfg.workDir}`;
    if (checked.current === null) {
      checked.current = pair;
      return;
    }
    if (checked.current === pair) return;
    checked.current = pair;
    void run();
  }, [dirty, cfg.downloadDir, cfg.workDir, run]);

  // The button shows the answer on itself: every folder usable, one that is
  // not, or a check that could not run. The rows below say which and why.
  const face: { label: string; icon?: ReactNode; verdict?: ButtonVerdict } = running
    ? { label: t('settings.owner.running') }
    : error
      ? { label: t('settings.info.checkFailed'), icon: <IconClose />, verdict: 'fail' }
      : !report
        ? { label: t('settings.owner.run') }
        : report.folders.some(unusable)
          ? { label: t('settings.diagnostics.verdict.fail'), icon: <IconClose />, verdict: 'fail' }
          : { label: t('settings.diagnostics.verdict.ok'), icon: <IconCheck />, verdict: 'ok' };

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.owner.checkHint')}>{t('settings.owner.checkTitle')}</SectionTitle>

      {/* Why the check could not run, above the button until the next one. */}
      {error && <span className="text-sm text-carbon-textSub">{error}</span>}

      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" icon={face.icon} verdict={face.verdict} shake={shake} onClick={() => void run()} disabled={running}>
          {face.label}
        </Button>
        <span role="status" className="sr-only">
          {face.verdict ? face.label : ''}
        </span>
        <span className="text-meta text-carbon-textMuted">
          {report ? t('settings.owner.checkedAt', { time: fmtDate(report.checkedAt) }) : t('settings.owner.never')}
        </span>
      </div>

      {/* There is always a download folder, so the list is never empty. */}
      {report && report.folders.length > 0 && (
        <div className="flex flex-col gap-3">
          {report.folders.map((f) => (
            <FolderRow key={`${f.role}:${f.dir}`} f={f} />
          ))}
        </div>
      )}
    </Card>
  );
}

/** unusable tells whether the check found something wrong with a folder. */
const unusable = (f: FolderOwnerProbe): boolean => f.verdict !== 'ok' && f.verdict !== 'unknown';

function FolderRow({ f }: { f: FolderOwnerProbe }) {
  const { t } = useT();
  const vars = {
    fileOwner: owner(f.fileUid, f.fileGid, f.fileUser, f.fileGroup),
    dirOwner: owner(f.dirUid, f.dirGid, f.dirUser, f.dirGroup),
    fileMode: f.fileMode,
    subdirMode: f.subdirMode,
    dirMode: f.dirMode,
    dirUid: f.dirUid,
    dirGid: f.dirGid,
    uid: f.fileUid,
    gid: f.fileGid,
    dir: f.dir,
    // omitempty, and absent unless a syscall failed.
    detail: f.detail ?? '',
  };
  const sentence = verdictKey(f.verdict);
  const bad = unusable(f);

  return (
    <div className="glim-well flex flex-col gap-1 p-4">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="text-meta uppercase tracking-wide text-carbon-textMuted">{roleLabel(t, f.role)}</span>
        <span className="glim-num break-all font-mono text-meta text-carbon-textSub" dir="ltr">
          {f.dir}
        </span>
      </div>
      <span className={`text-sm ${bad ? 'text-statusFail' : 'text-carbon-textSub'}`}>{t(sentence, vars)}</span>
      {fixes(f.verdict).map((key) => (
        <span key={key} className="text-meta text-carbon-textMuted" dir={key === 'settings.owner.fix.chown' ? 'ltr' : undefined}>
          {/* The chown line gets pasted into a shell, where the isolate marks
              t() sets around a path would become part of the path. */}
          {key === 'settings.owner.fix.chown'
            ? t(key, { uid: vars.uid, gid: vars.gid }).replace('{dir}', f.dir)
            : t(key, vars)}
        </span>
      ))}
    </div>
  );
}

function verdictKey(verdict: string): TranslationKey {
  switch (verdict) {
    case 'ok':
      return 'settings.owner.v.ok';
    case 'ownerMismatch':
      return 'settings.owner.v.ownerMismatch';
    case 'groupUnreadable':
      return 'settings.owner.v.groupUnreadable';
    case 'dirUnreadable':
      return 'settings.owner.v.dirUnreadable';
    case 'notWritable':
      return 'settings.owner.v.notWritable';
    case 'missing':
      return 'settings.owner.v.missing';
    default:
      return 'settings.owner.v.unknown';
  }
}

/**
 * fixes lists what helps for a verdict: naming the account in the run command
 * or handing the folder over on the host. PUID and PGID are never offered
 * because the image reads neither, and the mask verdicts get no chown, which
 * would not change the mode of new files.
 */
function fixes(verdict: string): TranslationKey[] {
  switch (verdict) {
    case 'ownerMismatch':
      return ['settings.owner.fix.user', 'settings.owner.fix.chown', 'settings.owner.fix.recreate', 'settings.owner.fix.past'];
    case 'groupUnreadable':
    case 'dirUnreadable':
      return ['settings.owner.fix.mask', 'settings.owner.fix.past'];
    case 'notWritable':
      return ['settings.owner.fix.user', 'settings.owner.fix.chown', 'settings.owner.fix.recreate'];
    default:
      return [];
  }
}
