import { useEffect, useState } from 'react';
import { Card, InfoBubble, SectionTitle, useTooltip } from '../../../components/ui';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  fetchJDStatus,
  fetchMediaTools,
  type JDStatus,
  type MediaTool,
  type MediaToolsStatus,
} from '../../../lib/api';
import { requestJump } from '../jump';
import { PageBadge } from '../ModuleToggle';

// The media tools card shows the programs a link is handed to: the yt-dlp and
// ffmpeg a media download runs, whether each was found, and whether the
// JDownloader backend answers. Keeping yt-dlp current is an update like
// KnightLoader's own, so it lives with them on the General page, which the
// card's badge leads to.

const SOURCE_KEYS: Record<string, TranslationKey> = {
  managed: 'settings.resolvers.toolsFrom.managed',
  env: 'settings.resolvers.toolsFrom.env',
  path: 'settings.resolvers.toolsFrom.path',
  none: 'settings.resolvers.toolsFrom.none',
};

/** Where the badge lands: the yt-dlp sub-card of the updates, named after the part. */
const YTDLP_UPDATES = { page: 'look', title: 'settings.look.updatesTitle', label: 'health.part.ytdlp' } as const;

export function MediaToolsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [jd, setJD] = useState<JDStatus | null>(null);

  useEffect(() => {
    // The server is unreachable when either fails, which the page already shows.
    fetchMediaTools()
      .then(setTools)
      .catch(() => {});
    fetchJDStatus()
      .then(setJD)
      .catch(() => {});
  }, []);

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle
        hint={t('settings.resolvers.toolsHint')}
        right={
          <PageBadge
            page={YTDLP_UPDATES.page}
            title={t('settings.look.updatesTitle')}
            onFollow={() => requestJump(YTDLP_UPDATES)}
            data-new="ytdlp-updates"
          />
        }
      >
        {t('settings.resolvers.toolsTitle')}
      </SectionTitle>
      <div className="glim-well overflow-hidden p-0">
        <ul className="flex flex-col">
          <ToolRow name="yt-dlp" tool={tools?.ytdlp} missingHint={t('settings.resolvers.toolsYtdlpMissingHint')} showSource />
          <ToolRow name="ffmpeg" tool={tools?.ffmpeg} missingHint={t('settings.resolvers.toolsFfmpegMissingHint')} />
          <ToolRow name="ffprobe" tool={tools?.ffprobe} missingHint={t('settings.resolvers.toolsFfmpegMissingHint')} />
          {jd && <JDRow jd={jd} />}
        </ul>
      </div>
    </Card>
  );
}

/**
 * JDRow shows the JDownloader backend as a tool: in use while it answers,
 * with its revision as the version, and otherwise whether it is set up at all.
 */
function JDRow({ jd }: { jd: JDStatus }) {
  const { t } = useT();
  const tool: MediaTool = { found: jd.reachable, version: jd.version ? String(jd.version) : '' };
  const [missing, hint] = jd.configured
    ? [t('settings.resolvers.toolsUnreachable'), t('settings.resolvers.toolsJdUnreachableHint')]
    : [t('settings.resolvers.toolsNotConfigured'), t('settings.resolvers.toolsJdMissingHint')];
  return <ToolRow name="JDownloader" tool={tool} missing={missing} missingHint={hint} last data-new="jd-tool" />;
}

/**
 * ToolRow shows one program's version, source and path, with an (i) only when
 * it is missing. `missing` says what stands in for the version then, Not
 * found where it is left out.
 */
function ToolRow({
  name,
  tool,
  missing,
  missingHint,
  showSource = false,
  last = false,
  'data-new': mark,
}: {
  name: string;
  tool?: MediaTool;
  missing?: string;
  missingHint: string;
  showSource?: boolean;
  last?: boolean;
  'data-new'?: string;
}) {
  const { t } = useT();
  const found = Boolean(tool?.found);
  // The whole path, where the row truncates it.
  const pathTip = useTooltip<HTMLSpanElement>(tool?.path);
  return (
    <li
      data-new={mark}
      className={`flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5 transition-colors hover:bg-carbon-surface3 ${
        last ? '' : 'border-b border-carbon-border/60'
      }`}
    >
      <span className="font-mono text-sm text-carbon-text" dir="ltr">
        {name}
      </span>
      <span className="glim-num text-sm text-carbon-text" dir="ltr">
        {found ? tool?.version || '' : (missing ?? t('settings.resolvers.toolsNotFound'))}
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
