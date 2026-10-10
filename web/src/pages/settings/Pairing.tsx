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
// by, both ordinary settings fields. Domains are recorded automatically when
// somebody signs in through one (routes_remote.go's learnDomain); the box covers
// a domain nobody has signed in through yet, which an instance without a
// password needs before it answers on that name at all.
function IdentityCard() {
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
        label={t('settings.access.identity.domainsLabel')}
        hint={t('settings.access.identity.domainsHint')}
        htmlFor="known-domains"
      >
        <div className="w-[18rem] max-w-full">
          <ListArea
            id="known-domains"
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
