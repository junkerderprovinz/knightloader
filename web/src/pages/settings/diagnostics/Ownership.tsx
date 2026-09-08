import { useCallback } from 'react';
import { fetchFileOwner, type FileOwnerIdentity } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { Card, SectionTitle } from '../../../components/ui';

/**
 * Who this instance writes files as, on the page somebody is already on when
 * they are trying to work out why something is wrong.
 *
 * IT IS A READOUT AND IT PROMISES NOTHING. This image declares USER knight, so
 * the process starts as uid 1000 and cannot become another uid; PUID, PGID and
 * UMASK are read by nothing at any layer. So the card says what the identity IS
 * and, where one of those variables is set, says plainly that nothing read it.
 * The one thing it must never do is imply that setting PUID would change the
 * number above it, which would send somebody to recreate a container for no
 * effect and then disbelieve the rest of the page.
 *
 * WHY THE VARIABLES APPEAR AT ALL. Because the alternative is worse: an
 * operator who typed PUID=99 into the template and is looking at files owned by
 * 1000 has no way, from inside the app, to tell "the image ignores it" from "I
 * typed it wrong" from "something else overrode it". Showing what was asked for
 * beside what is in force answers all three at once, and it is the single most
 * useful line this feature produces.
 *
 * The GET behind it writes nothing at all, so this card may hold it on mount -
 * unlike the folder check on the Downloads page, which measures by writing and
 * is therefore behind a button and a save.
 *
 * The strings are not in en.ts yet, for the reason this page's own PENDING map
 * already gives: locale files are one writer's lane per wave, and the lookup
 * below asks the real catalogue first.
 */
const PENDING = {
  'settings.owner.identityTitle': 'Who this instance writes as',
  'settings.owner.identityHint':
    'Every file this instance creates gets this owner and these permissions, and so does everything it starts: yt-dlp, ffmpeg and the bundled JDownloader. In a container the identity is fixed the moment the container starts, by the account it was started under, and nothing on this page can change it.',
  'settings.owner.uid': 'User',
  'settings.owner.gid': 'Group',
  'settings.owner.umask': 'Umask',
  'settings.owner.umaskUnknown': 'This kernel does not report it',
  'settings.owner.asked': 'Asked for',
  'settings.owner.envIgnored':
    '{name}={value} is set, but nothing in this image reads it. The identity is fixed by the account the container was started under, which is {owner}.',
  'settings.owner.envHow':
    'To run under another account, name it in the run command: --user <uid>:<gid>. On Unraid that goes in Extra Parameters, and the container has to be recreated for it to take.',
  'settings.owner.desktopNote':
    'The desktop app runs under your own account. PUID, PGID and UMASK are container settings and do nothing here.',
  'settings.owner.noOwners': 'This system has no file owners, so there is nothing to compare. That is not a fault.',
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

export function OwnershipCard({ hue }: { hue: number }) {
  const cx = useCx();
  const { data, failed } = useResource<FileOwnerIdentity>(fetchFileOwner);

  // No LoadingCard and no ErrorCard: this is one card among several on a page
  // that has already loaded, and a failed side request must not replace the
  // diagnostics somebody came here to read. It simply draws nothing.
  if (failed || !data) return null;

  const who = `${data.uid}:${data.gid}`;
  const named = (id: number, name: string) => (name ? `${id} (${name})` : String(id));

  // A variable that is set and read by nothing. Listed by NAME rather than as
  // one lumped sentence, because an operator who set two of them needs to see
  // both of their own values back.
  const ignored: Array<[string, string]> = [];
  if (!data.envRead) {
    if (data.env.puid) ignored.push(['PUID', data.env.puid]);
    if (data.env.pgid) ignored.push(['PGID', data.env.pgid]);
    if (data.env.umask) ignored.push(['UMASK', data.env.umask]);
  }

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={cx('settings.owner.identityHint')}>{cx('settings.owner.identityTitle')}</SectionTitle>

      {!data.known ? (
        // The third answer, and it is not a zero: a Windows desktop build has no
        // unix owners at all, and printing "0:0" there would say root owns the
        // downloads - a confident answer to a question that cannot be asked.
        <span className="text-sm text-carbon-textSub">{cx('settings.owner.noOwners')}</span>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Stat label={cx('settings.owner.uid')} value={named(data.uid, data.user)} />
          <Stat label={cx('settings.owner.gid')} value={named(data.gid, data.group)} />
          <Stat
            label={cx('settings.owner.umask')}
            value={data.umaskKnown ? data.umask : cx('settings.owner.umaskUnknown')}
          />
        </div>
      )}

      {data.deployment === 'desktop' && (
        <span className="text-[11px] text-carbon-textMuted">{cx('settings.owner.desktopNote')}</span>
      )}

      {data.deployment !== 'desktop' && ignored.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="text-[11px] uppercase tracking-wide text-carbon-textMuted">{cx('settings.owner.asked')}</span>
          {ignored.map(([name, value]) => (
            <span key={name} className="text-sm text-statusWarn">
              {cx('settings.owner.envIgnored', { name, value, owner: who })}
            </span>
          ))}
          <span className="text-[11px] text-carbon-textMuted">{cx('settings.owner.envHow')}</span>
        </div>
      )}
    </Card>
  );
}

/** The same two-line reading the system card above it draws, kept local so this
 *  card can be dropped in or moved without dragging a helper across files. */
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
