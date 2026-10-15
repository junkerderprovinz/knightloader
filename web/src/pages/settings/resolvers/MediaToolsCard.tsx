import { useEffect, useState } from 'react';
import { InfoBubble, useTooltip } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { fetchMediaTools, type MediaTool, type MediaToolsStatus } from '../../../lib/api';
import { ModuleCard, PageBadge } from '../ModuleToggle';

// The media tools card shows the yt-dlp and ffmpeg a media download runs, and
// whether each was found. Keeping yt-dlp current is an update like
// KnightLoader's own, so it lives with them on the General page, which the
// card's badge leads to.

const SOURCE_KEYS: Record<string, TranslationKey> = {
  managed: 'settings.resolvers.toolsFrom.managed',
  env: 'settings.resolvers.toolsFrom.env',
  path: 'settings.resolvers.toolsFrom.path',
  none: 'settings.resolvers.toolsFrom.none',
};

export function MediaToolsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);

  useEffect(() => {
    fetchMediaTools()
      .then(setTools)
      .catch(() => {
        // The server is unreachable, which the page already shows.
      });
  }, []);

  return (
    <ModuleCard
      id="ytdlp"
      hue={hue}
      title={t('settings.resolvers.toolsTitle')}
      titleHint={t('settings.resolvers.toolsHint')}
      titleRight={<PageBadge page="look" title={t('settings.nav.look')} />}
      hint={t('settings.resolvers.about')}
    >
      <div className="glim-well p-0">
        <ul className="flex flex-col">
          <ToolRow name="yt-dlp" tool={tools?.ytdlp} missingHint={t('settings.resolvers.toolsYtdlpMissingHint')} showSource />
          <ToolRow name="ffmpeg" tool={tools?.ffmpeg} missingHint={t('settings.resolvers.toolsFfmpegMissingHint')} />
          <ToolRow name="ffprobe" tool={tools?.ffprobe} missingHint={t('settings.resolvers.toolsFfmpegMissingHint')} last />
        </ul>
      </div>
    </ModuleCard>
  );
}

/**
 * ToolRow shows one program's version, source and path, with an (i) only when
 * it is missing.
 */
function ToolRow({
  name,
  tool,
  missingHint,
  showSource = false,
  last = false,
}: {
  name: string;
  tool?: MediaTool;
  missingHint: string;
  showSource?: boolean;
  last?: boolean;
}) {
  const { t } = useT();
  const found = Boolean(tool?.found);
  // The whole path, where the row truncates it.
  const pathTip = useTooltip<HTMLSpanElement>(tool?.path);
  return (
    <li
      className={`flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 ${
        last ? '' : 'border-b border-carbon-border/60'
      }`}
    >
      <span className="font-mono text-sm text-carbon-text" dir="ltr">
        {name}
      </span>
      <span className="glim-num text-sm text-carbon-text" dir="ltr">
        {found ? tool?.version || '' : t('settings.resolvers.toolsNotFound')}
      </span>
      {found && <span className="glim-eyebrow">{t('settings.resolvers.toolsInUse')}</span>}
      {showSource && tool?.source && SOURCE_KEYS[tool.source] && (
        <span className="text-xs text-carbon-textMuted">{t(SOURCE_KEYS[tool.source])}</span>
      )}
      {tool?.path && (
        <span className="ms-auto truncate font-mono text-xs text-carbon-textMuted" dir="ltr" {...pathTip.triggerProps}>
          {tool.path}
        </span>
      )}
      {pathTip.node}
      {!found && <InfoBubble className={tool?.path ? '' : 'ms-auto'} tip={missingHint} />}
    </li>
  );
}
