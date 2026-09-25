import { fetchFileOwner, type FileOwnerIdentity } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { Card, InfoBubble, SectionTitle } from '../../../components/ui';

/**
 * The card shows the uid, gid and umask this instance writes files as. The
 * image runs as a fixed user and reads no PUID, PGID or UMASK, so any of them
 * that is set is echoed beside the identity in force, marked as ignored.
 */
export function OwnershipCard({ hue }: { hue: number }) {
  const { t } = useT();
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
      <SectionTitle hint={t('settings.owner.identityHint')}>{t('settings.owner.identityTitle')}</SectionTitle>

      {!data.known ? (
        // Windows has no unix owners, and "0:0" would read as root.
        <span className="text-sm text-carbon-textSub">{t('settings.owner.noOwners')}</span>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Stat label={t('settings.owner.uid')} value={named(data.uid, data.user)} />
          <Stat label={t('settings.owner.gid')} value={named(data.gid, data.group)} />
          <Stat
            label={t('settings.owner.umask')}
            value={data.umaskKnown ? data.umask : t('settings.owner.umaskUnknown')}
          />
        </div>
      )}

      {data.deployment === 'desktop' && (
        <span className="text-[11px] text-carbon-textMuted">{t('settings.owner.desktopNote')}</span>
      )}

      {data.deployment !== 'desktop' && ignored.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="flex items-center text-[11px] uppercase tracking-wide text-carbon-textMuted">
            {t('settings.owner.asked')}
            <InfoBubble tip={t('settings.owner.envHow')} />
          </span>
          {ignored.map(([name, value]) => (
            <span key={name} className="text-sm text-statusWarn">
              {t('settings.owner.envIgnored', { name, value, owner: who })}
            </span>
          ))}
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
