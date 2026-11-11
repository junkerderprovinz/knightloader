// BehindList names the devices that did not follow the pairing into the
// fortress before the group on the old phrase closed, each with what it is and
// the button that takes it out. The owner knows them by name, so no address
// shows.
import type { BehindDevice } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconBrowser, IconFleet, IconPhone, IconTrash } from '../lib/icons';
import type { Removal } from './InstanceRemoval';
import { Button, InfoBubble } from './ui';

const KIND: Record<string, TranslationKey> = {
  mobile: 'instances.kind.mobile',
  extension: 'instances.kind.extension',
  container: 'instances.kind.container',
  server: 'instances.kind.server',
  desktop: 'instances.kind.desktop',
};

/** kindOf is what the device is: the app for a phone or browser, the build for an instance. */
export function kindOf(d: BehindDevice): TranslationKey {
  return KIND[d.kind === 'instance' ? d.deployment : d.kind] ?? 'pairing.pendingInstance';
}

export function BehindList({ devices, onRemove }: { devices: BehindDevice[]; onRemove: (r: Removal) => void }) {
  const { t } = useT();
  if (devices.length === 0) return null;
  return (
    <section className="flex flex-col gap-2" data-testid="behind" aria-label={t('pairing.behindTitle')}>
      <span className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
        {t('pairing.behindTitle')}
        <InfoBubble tip={t('pairing.behindTip')} />
      </span>
      <ul className="flex flex-col gap-2">
        {devices.map((d) => {
          const name = d.name || d.id;
          return (
            <li
              key={d.id}
              className="flex min-h-11 flex-wrap items-center gap-x-2.5 gap-y-0.5 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5"
            >
              <span className="shrink-0 text-carbon-textMuted [&>svg]:h-4.5 [&>svg]:w-4.5">
                {d.kind === 'mobile' ? <IconPhone /> : d.kind === 'extension' ? <IconBrowser /> : <IconFleet />}
              </span>
              <span className="min-w-24 flex-1 truncate text-sm font-medium text-carbon-text">{name}</span>
              <span className="glim-eyebrow shrink-0 tracking-[.12em]">{t(kindOf(d))}</span>
              <Button
                kind="secondary"
                icon={<IconTrash width={14} height={14} />}
                className="kl-raised-btn px-2.5 text-xs"
                aria-label={t('instances.removeTitle', { name })}
                onClick={() => onRemove({ kind: d.kind === 'instance' ? 'member' : 'app', id: d.id, name })}
              >
                {t('instances.remove')}
              </Button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
