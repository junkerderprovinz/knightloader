import { fetchFileOwner, type FileOwnerIdentity } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { InfoBubble } from '../../../components/ui';
import { SubHead } from '../controls';

/**
 * OwnerSection shows the uid, gid and umask this instance writes files as.
 * PUID and PGID are read only when the container starts as root, so each that
 * is set is echoed beside the identity in force, marked as applied or as not
 * read. UMASK is applied at start, and the section says whether the value took.
 */
export function OwnerSection() {
  const { t } = useT();
  const { data, failed } = useResource<FileOwnerIdentity>(fetchFileOwner);

  // A failed side request draws nothing rather than covering the page.
  if (failed || !data) return null;

  const who = `${data.uid}:${data.gid}`;
  const named = (id: number, name: string) => (name ? `${id} (${name})` : String(id));

  // Every variable that is set, with what became of it, so each value is echoed back.
  const asked: Array<{ name: string; line: string; took: boolean }> = [];
  const id = (name: string, value: string) =>
    asked.push(
      data.idsRead
        ? { name, took: true, line: t('settings.owner.envApplied', { name, value }) }
        : { name, took: false, line: t('settings.owner.envIgnored', { name, value, owner: who }) },
    );
  if (data.env.puid) id('PUID', data.env.puid);
  if (data.env.pgid) id('PGID', data.env.pgid);
  if (data.env.umask) {
    const value = data.env.umask;
    asked.push(
      data.umaskApplied
        ? { name: 'UMASK', took: true, line: t('settings.owner.umaskApplied', { value }) }
        : { name: 'UMASK', took: false, line: t('settings.owner.umaskRefused', { value }) },
    );
  }

  return (
    <section className="flex flex-col gap-4">
      <SubHead label={t('settings.owner.identityTitle')} hint={t('settings.owner.identityHint')} />

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
        <span className="text-meta text-carbon-textMuted">{t('settings.owner.desktopNote')}</span>
      )}

      {data.deployment !== 'desktop' && asked.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="flex items-center text-meta uppercase tracking-wide text-carbon-textMuted">
            {t('settings.owner.asked')}
            <InfoBubble tip={t('settings.owner.envHow')} />
          </span>
          {asked.map(({ name, line, took }) => (
            <span key={name} className={took ? 'text-sm text-carbon-textSub' : 'text-sm text-statusWarn'}>
              {line}
            </span>
          ))}
        </div>
      )}
    </section>
  );
}

/** Stat is one reading of the System card: a caption over a value. */
export function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-meta text-carbon-textMuted">{label}</span>
      <span className="glim-num text-sm text-carbon-text" dir="ltr">
        {value}
      </span>
    </div>
  );
}
