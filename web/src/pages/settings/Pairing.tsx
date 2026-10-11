// The Pairing page: the twelve words, the members, the name this instance
// goes by in its group and the relay (pairing/PairingSection.tsx).
import { Card, SectionTitle, TextInput } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { ListArea } from './controls';
import { PairingSection } from './pairing/PairingSection';
import { SetRow } from './pairing/parts';

export function Pairing() {
  return <PairingSection identity={(group) => <IdentityCard found={group?.networkAddress ?? ''} />} />;
}

// IdentityCard holds an optional name and the addresses this instance is known
// by. Its address on the network it finds itself, and that one is only shown.
// A domain is recorded when somebody signs in through it (routes_remote.go's
// learnDomain); the box covers a domain nobody has signed in through yet, which
// an instance without a password needs before it answers on that name at all.
export function IdentityCard({ found }: { found: string }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <Card hue={5} className="flex flex-col">
      <SectionTitle>{t('settings.access.identity.title')}</SectionTitle>
      <SetRow
        label={t('settings.access.identity.nameLabel')}
        hint={t('settings.access.identity.nameHint')}
        htmlFor="instance-name"
      >
        <div className="w-[18rem] max-w-full">
          <TextInput
            id="instance-name"
            placeholder={t('settings.access.identity.namePlaceholder')}
            value={cfg.instanceName}
            onChange={(e) => patch({ instanceName: e.target.value })}
          />
        </div>
      </SetRow>
      <SetRow
        label={t('settings.access.identity.addressesLabel')}
        hint={t('settings.access.identity.addressesHint')}
        htmlFor="known-addresses"
      >
        <div className="flex w-[18rem] max-w-full flex-col gap-2">
          {found && (
            <p data-testid="found-address" className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 px-3">
              <span dir="ltr" className="min-w-0 break-all font-mono text-sm text-carbon-text">
                {found}
              </span>
              <span className="glim-eyebrow">{t('settings.access.identity.addressFound')}</span>
            </p>
          )}
          <ListArea
            id="known-addresses"
            rows={3}
            dir="ltr"
            lines={cfg.knownDomains}
            onLines={(knownDomains) => patch({ knownDomains })}
          />
        </div>
      </SetRow>
    </Card>
  );
}
