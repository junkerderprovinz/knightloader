// The Pairing page: the name this instance goes by in its group, and the
// twelve words, the members and the relay (pairing/PairingSection.tsx).
import { Card, Field, SectionTitle, TextInput } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { ListArea } from './controls';
import { PairingSection } from './pairing/PairingSection';

export function Pairing() {
  return (
    <div className="flex flex-col gap-10">
      <PairingSection />
      {/* After the group, since most people never need to name a machine. */}
      <IdentityCard />
    </div>
  );
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
    <Card hue={2} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.access.identity.title')}</SectionTitle>
      <Field label={t('settings.access.identity.nameLabel')} hint={t('settings.access.identity.nameHint')}>
        <TextInput
          placeholder={t('settings.access.identity.namePlaceholder')}
          value={cfg.instanceName}
          onChange={(e) => patch({ instanceName: e.target.value })}
        />
      </Field>
      <Field label={t('settings.access.identity.domainsLabel')} hint={t('settings.access.identity.domainsHint')}>
        <ListArea rows={3} dir="ltr" lines={cfg.knownDomains} onLines={(knownDomains) => patch({ knownDomains })} />
      </Field>
    </Card>
  );
}
