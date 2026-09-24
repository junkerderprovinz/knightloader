import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { ConnectionsCard } from './Connections';
import { HeaderProfilesCard } from './network/HeaderProfiles';
import { HostRulesCard } from './network/HostRules';
import { ReconnectCards } from './Reconnect';

/**
 * Network is how downloads leave this machine: which outbound connections they
 * spread across, how the router is asked for a new address, which headers a
 * site gets, and what each host is allowed.
 */
export function Network() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.network')} />
      <ConnectionsCard hue={0} />
      <ReconnectCards hue={1} />
      {/* Its values live in the credential store, so the card saves itself. */}
      <HeaderProfilesCard hue={4} />
      <HostRulesCard hue={5} />
    </div>
  );
}
