import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { DriveCard } from './integrations/DriveCard';
import { McpCard } from './integrations/McpCard';
import { VpnCard } from './integrations/VpnCard';

/**
 * Integrations holds the outside services this instance is set up with by
 * picking a brand, the VPN providers its downloads go out through and the AI
 * clients that reach it over MCP, and the debrid drive other programs mount.
 */
export function Integrations() {
  const { t } = useT();
  return (
    <div data-new="integrations-page" className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.integrations')} />
      <VpnCard hue={0} routesHue={1} />
      <McpCard />
      <DriveCard hue={3} />
    </div>
  );
}
