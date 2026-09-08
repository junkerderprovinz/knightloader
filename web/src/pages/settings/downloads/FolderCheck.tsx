import { useCallback, useEffect, useRef, useState } from 'react';
import { checkFolderOwners, type FolderOwnerProbe, type FolderOwnerReport } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fmtDate } from '../../../lib/format';
import { Button, Card, SectionTitle } from '../../../components/ui';
import { useDraft } from '../context';

/**
 * What actually happens to a file this instance writes into each configured
 * folder: who it ends up belonging to, and who else can then open it.
 *
 * WHY THIS IS NOT THE CHECK THE FOLDER FIELDS ALREADY DO. Saving a folder runs
 * settings.Validate, which creates the directory and writes a probe file into
 * it. On a NAS that write SUCCEEDS while the process is uid 1000 and the share
 * belongs to 99:100 - the file simply lands owned by 1000. So the folder field
 * goes green, the download works, and the media server next door reports an
 * empty library, with nothing anywhere connecting the two. This card is the
 * measurement that can see it: it compares what the probe file CAME OUT AS
 * against what the folder already was.
 *
 * IT REPORTS AND IT NEVER REPAIRS. Nothing here can change an owner or a mask:
 * the container's identity is fixed the moment it starts, and this process
 * cannot become another user. What the card produces is the exact command to
 * run, which is the part somebody staring at "the library is empty" has no way
 * to work out.
 *
 * IT WRITES, so it is behind a POST and behind either a button press or a save.
 * Deliberately NOT on mount: opening a settings page must not drop a file into
 * every configured folder, several of which are network shares that may be
 * asleep. The automatic run is the one the item asks for - after a save that
 * changed the download or the working folder - and it is the moment the answer
 * is worth having, because that is when the ownership can have changed.
 *
 * The strings are not in en.ts yet: locale files are one writer's lane per wave
 * (the arrangement Diagnostics.tsx, Captcha.tsx and Connections.tsx already
 * use), and the lookup below asks the real catalogue first, so the day these
 * keys land it stops being consulted.
 */
const PENDING = {
  'settings.owner.checkTitle': 'Folder check',
  'settings.owner.checkHint':
    'This really writes one file and one sub-folder into each folder, reads back who owns them and who else may read them, and removes both again. It has to be measured rather than worked out: a share can carry its own permission rules, and a folder marked set-group-id hands out a different group than the one this process has. It runs on its own after you save a changed folder, and whenever you press the button.',
  'settings.owner.run': 'Check the folders now',
  'settings.owner.running': 'Checking…',
  'settings.owner.never': 'Not checked yet.',
  'settings.owner.checkedAt': 'Last checked {time}.',
  'settings.owner.checkFailed': 'The check could not run: {error}',
  'settings.owner.role.downloads': 'Download folder',
  'settings.owner.role.work': 'Working folder',
  'settings.owner.role.category': 'Category folder',
  'settings.owner.role.watch': 'Watched folder',
  'settings.owner.v.ok':
    'Files land here as {fileOwner} with {fileMode}, new sub-folders with {subdirMode}, and the folder itself belongs to {dirOwner}. Nothing about that stands out.',
  'settings.owner.v.ownerMismatch':
    'Files land here as {fileOwner}, but the folder belongs to {dirOwner}. Reading them usually still works; renaming, moving and deleting them from another account does not, and anything that expects the folder to own what is in it treats those files as foreign.',
  'settings.owner.v.groupUnreadable':
    'Files land here with {fileMode}, so only {fileOwner} can open them. Anything running under another account sees the names and gets nothing else.',
  'settings.owner.v.dirUnreadable':
    'New sub-folders land here with {subdirMode}. Nobody but {fileOwner} gets inside them, whatever the files in them allow, and every download lands in one.',
  'settings.owner.v.notWritable': 'This instance cannot write here at all: {detail}',
  'settings.owner.v.missing': 'The folder is not there. It will be created the first time something is written into it.',
  'settings.owner.v.unknown': 'This system has no file owners, so there is nothing to compare here. That is not a fault.',
  'settings.owner.fix.user':
    'Start the container under the account the folder belongs to: --user {dirUid}:{dirGid} in the run command. On Unraid that goes in Extra Parameters.',
  'settings.owner.fix.chown': 'Or hand the folder over instead, on the host: chown -R {uid}:{gid} {dir}',
  'settings.owner.fix.mask':
    'This instance cannot change that. The mask comes from whatever started the container, and a mounted share can force its own with a umask, file_mode or dir_mode option. Fix it where the folder is mounted.',
  'settings.owner.fix.recreate':
    'A changed run command needs the container recreated. Restarting is not enough: the identity is fixed the moment it starts.',
  'settings.owner.fix.past':
    'Files already downloaded keep the owner they were written with. Anything you change applies to what comes next; the rest needs the command above.',
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
 * "99:100 (nobody:users)", or "1000:1000" when the numbers have no names.
 *
 * The NUMBERS lead and the names follow, deliberately. The number is what goes
 * into a chown and into --user; the name is the part that makes the line
 * readable and the part that is missing exactly when it matters most, because a
 * container started with --user 99:100 runs as a uid nothing in the image's
 * /etc/passwd mentions.
 */
function owner(uid: number, gid: number, user: string, group: string): string {
  if (!user && !group) return `${uid}:${gid}`;
  return `${uid}:${gid} (${user || uid}:${group || gid})`;
}

/** The role id the server sends, looked up locally - it is a stable id and never prose. */
function roleLabel(cx: ReturnType<typeof useCx>, role: string): string {
  switch (role) {
    case 'downloads':
      return cx('settings.owner.role.downloads');
    case 'work':
      return cx('settings.owner.role.work');
    case 'category':
      return cx('settings.owner.role.category');
    case 'watch':
      return cx('settings.owner.role.watch');
    default:
      // A role a later wave adds must still draw as something rather than as a
      // blank cell, the same fallback deploymentLabel makes on the Diagnostics
      // page.
      return role;
  }
}

export function FolderCheckCard({ hue }: { hue: number }) {
  const cx = useCx();
  const { cfg, dirty } = useDraft();
  const [report, setReport] = useState<FolderOwnerReport | null>(null);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');

  const run = useCallback(async () => {
    setError('');
    setRunning(true);
    try {
      setReport(await checkFolderOwners());
    } catch (e) {
      setError(cx('settings.owner.checkFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setRunning(false);
    }
  }, [cx]);

  // The automatic run, and the whole of the "check on save" the item asks for.
  //
  // A ref rather than state, because updating state here would re-run the effect
  // that set it. It starts as the pair the page mounted with, which is what
  // keeps the first render from firing a write: an unchanged folder has nothing
  // new to say, and a probe file in every configured folder is not something
  // opening a page should do.
  const checked = useRef<string | null>(null);
  useEffect(() => {
    // Only once the save has landed. While the draft is dirty the value in cfg
    // is what somebody is still typing, and checking a half-typed path would
    // report a missing folder at every keystroke.
    if (dirty) return;
    const pair = `${cfg.downloadDir} ${cfg.workDir}`;
    if (checked.current === null) {
      checked.current = pair;
      return;
    }
    if (checked.current === pair) return;
    checked.current = pair;
    void run();
  }, [dirty, cfg.downloadDir, cfg.workDir, run]);

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={cx('settings.owner.checkHint')}>{cx('settings.owner.checkTitle')}</SectionTitle>

      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={() => void run()} disabled={running}>
          {running ? cx('settings.owner.running') : cx('settings.owner.run')}
        </Button>
        <span className="text-[11px] text-carbon-textMuted">
          {report ? cx('settings.owner.checkedAt', { time: fmtDate(report.checkedAt) }) : cx('settings.owner.never')}
        </span>
      </div>

      {error && <span className="text-sm text-statusFail">{error}</span>}

      {/* No empty state. The list is never empty on an instance that answered
          at all - there is always a download folder, configured or the built-in
          one - so a sentence for that case would be a sentence nobody can reach
          and nobody can check. A failed request has its own line above. */}
      {report && report.folders.length > 0 && (
        <div className="flex flex-col gap-3">
          {report.folders.map((f) => (
            <FolderRow key={`${f.role}:${f.dir}`} f={f} cx={cx} />
          ))}
        </div>
      )}
    </Card>
  );
}

function FolderRow({ f, cx }: { f: FolderOwnerProbe; cx: ReturnType<typeof useCx> }) {
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
    // Absent for every verdict that is not about a failed syscall - the field
    // is omitempty on the wire, and a sentence reading "…at all: undefined"
    // is worse than one that simply stops.
    detail: f.detail ?? '',
  };
  const sentence = verdictKey(f.verdict);
  const bad = f.verdict !== 'ok' && f.verdict !== 'unknown';

  return (
    <div className="glim-well flex flex-col gap-1 p-4">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="text-[11px] uppercase tracking-wide text-carbon-textMuted">{roleLabel(cx, f.role)}</span>
        {/* ltr regardless of interface direction, the same convention every
            other path cell in settings/ uses: a path mixes separators and Latin
            names and does not read correctly mirrored. */}
        <span className="glim-num break-all font-mono text-[11px] text-carbon-textSub" dir="ltr">
          {f.dir}
        </span>
      </div>
      <span className={`text-sm ${bad ? 'text-statusFail' : 'text-carbon-textSub'}`}>{cx(sentence, vars)}</span>
      {fixes(f.verdict).map((key) => (
        <span key={key} className="text-[11px] text-carbon-textMuted" dir={key === 'settings.owner.fix.chown' ? 'ltr' : undefined}>
          {cx(key, vars)}
        </span>
      ))}
    </div>
  );
}

/** The verdict id the server sends, mapped to the sentence that explains it. */
function verdictKey(verdict: string): PendingKey {
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
 * What to do about each verdict, and nothing beyond what is actually true of
 * this image.
 *
 * PUID AND PGID ARE NOT OFFERED ANYWHERE HERE. The image declares USER knight,
 * so it starts as uid 1000 and cannot change uid; nothing in it reads those two
 * variables, and advice to set them would send somebody to recreate a container
 * for no effect at all and then disbelieve the rest of the page. What does work
 * is naming the account in the run command, or handing the folder over on the
 * host, and both are offered.
 *
 * The mask verdicts get no chown line: chowning a folder does not change the
 * mode new files are created with, so it would be a command that changes
 * something real and fixes nothing.
 */
function fixes(verdict: string): PendingKey[] {
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
