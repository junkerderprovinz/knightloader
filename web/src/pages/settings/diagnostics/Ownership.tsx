import { useCallback } from 'react';
import { fetchFileOwner, type FileOwnerIdentity } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { Card, SectionTitle } from '../../../components/ui';

/**
 * The card shows the uid, gid and umask this instance writes files as. The
 * image runs as a fixed user and reads no PUID, PGID or UMASK, so any of them
 * that is set is echoed beside the identity in force, marked as ignored.
 * PENDING holds the English strings until the catalogue has them; the lookup
 * asks the catalogue first.
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

  // A failed side request draws nothing rather than covering the page.
  if (failed || !data) return null;

  const who = `${data.uid}:${data.gid}`;
  const named = (id: number, name: string) => (name ? `${id} (${name})` : String(id));

  // Each variable that is set and ignored, by name, so every value is echoed back.
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
        // Windows has no unix owners, and "0:0" would read as root.
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
