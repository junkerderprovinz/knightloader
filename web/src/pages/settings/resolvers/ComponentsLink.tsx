import { Card } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { requestJump } from '../jump';
import { PageBadge } from '../ModuleToggle';

/** Where the badge lands: the yt-dlp sub-card of the Components card on Info. */
const YTDLP = { page: 'help', title: 'settings.info.componentsTitle', label: 'health.part.ytdlp' } as const;

/**
 * ComponentsLink is one line that says where yt-dlp, ffmpeg, UnRAR and
 * JDownloader are kept current, with the way there, since a media link that
 * stopped working brings people to this page first.
 */
export function ComponentsLink({ hue }: { hue: number }) {
  const { t } = useT();
  return (
    <Card hue={hue}>
      <p className="flex flex-wrap items-center gap-x-3 gap-y-2 text-sm text-carbon-textSub">
        {t('settings.resolvers.toolsMoved', { page: t('settings.nav.help'), name: t('settings.info.componentsTitle') })}
        <PageBadge
          page={YTDLP.page}
          title={t('settings.info.componentsTitle')}
          onFollow={() => requestJump(YTDLP)}
          data-new="ytdlp-updates"
        />
      </p>
    </Card>
  );
}
