import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { ConnectionsCard } from './Connections';
import { HeaderProfilesCard } from './network/HeaderProfiles';
import { HostRulesCard } from './network/HostRules';
import { ProxyCheckCard } from './network/ProxyCheck';
import { ReconnectCards } from './Reconnect';

/**
 * Network is how downloads leave this machine and how the browser reaches it:
 * the outbound connections, the reverse proxy in front, the router's new
 * address, the headers a site gets and what each host is allowed.
 */
export function Network() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.network')} />
      <ConnectionsCard hue={0} />
      <ProxyCheckCard hue={1} />
      <ReconnectCards hue={2} />
      {/* Its values live in the credential store, so the card saves itself. */}
      <HeaderProfilesCard hue={5} />
      <HostRulesCard hue={6} />
    </div>
  );
}
