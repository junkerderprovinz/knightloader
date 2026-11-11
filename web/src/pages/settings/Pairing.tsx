// The Pairing page: the twelve words, the members, the name this instance
// goes by in its group and the relay (pairing/PairingSection.tsx).
import { Card, SectionTitle, TextInput } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { ListArea } from './controls';
import { PairingSection } from './pairing/PairingSection';
import { SetRow } from './pairing/parts';

export function Pairing() {
  return <PairingSection identity={<IdentityCard />} />;
}

// IdentityCard holds an optional name and the domains this instance is known
// by. They are only what the owner typed: the first one is the address the
// group is told, and a name taken from a request's Host header could be
// anybody's.
export function IdentityCard() {
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
        <div className="w-[18rem] max-w-full">
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
