import type { ReactNode } from 'react';
import { IconBolt, IconLink, IconSendTest } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import type { ChannelKind } from '../../../lib/eventtargets';

/** The kinds offered when adding, in the order the picker shows them. A kind with a host gets its logo from there. */
export const KINDS: { kind: ChannelKind; name: string; host?: string; icon?: ReactNode }[] = [
  { kind: 'discord', name: 'Discord' },
  { kind: 'slack', name: 'Slack', host: 'slack.com' },
  { kind: 'gotify', name: 'Gotify', host: 'gotify.net' },
  { kind: 'ntfy', name: 'ntfy', host: 'ntfy.sh' },
  { kind: 'matrix', name: 'Matrix', host: 'matrix.org' },
  { kind: 'apprise', name: 'Apprise', icon: <IconBolt width={18} height={18} /> },
  { kind: 'smtp', name: '', icon: <IconSendTest width={18} height={18} /> },
  { kind: 'webhook', name: '', icon: <IconLink width={18} height={18} /> },
];

/** useKindName names a kind as the page shows it. */
export function useKindName(): (kind: ChannelKind) => string {
  const { t } = useT();
  return (kind) => {
    if (kind === 'webhook') return t('settings.eventTargets.anyAddress');
    if (kind === 'smtp') return t('settings.eventTargets.kind.smtp');
    return KINDS.find((k) => k.kind === kind)?.name ?? kind;
  };
}

