import { useEffect, useState } from 'react';
import { Button, Card, ErrorCard, IconTile, InfoBubble, LoadingCard, PageHeader, SectionTitle } from '../../components/ui';
import { disconnectVpn, fetchVpn, type VpnProvider, type VpnState } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { IconGlobe } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { refusalText } from './tx';
import { ConnectDialog } from './vpn/ConnectDialog';
import { ProviderTiles } from './vpn/ProviderTiles';
import { VpnBadges, VpnMark, failureOf, nameOf } from './vpn/VpnMark';

// A connect settles within seconds, and a tunnel that drops shows within the
// slower beat.
const POLL_CONNECTING_MS = 1000;
const POLL_MS = 5000;

/**
 * Vpn is where torrents get their VPN: which provider they go through now, and
 * every provider KnightLoader can connect to.
 */
export function Vpn() {
  const { t } = useT();
  const { toast } = useToast();
  const [vpn, setVpn] = useState<VpnState | null>(null);
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  const [open, setOpen] = useState('');
  const [busy, setBusy] = useState(false);

  const connecting = vpn?.providers.some((p) => p.state === 'connecting') ?? false;
  useEffect(() => {
    let live = true;
    const load = async () => {
      try {
        const next = await fetchVpn();
        if (!live) return;
        setVpn(next);
        setFailed(false);
      } catch {
        if (live) setFailed(true);
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), connecting ? POLL_CONNECTING_MS : POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [connecting, retry]);

  if (!vpn) {
    return failed ? (
      <ErrorCard message={t('common.loadFailed')} retry={() => setRetry((n) => n + 1)} retryLabel={t('common.retry')} />
    ) : (
      <LoadingCard label={t('common.loading')} />
    );
  }

  const active = vpn.providers.find((p) => p.id === vpn.active);
  const picked = vpn.providers.find((p) => p.id === open);

  async function disconnect() {
    setBusy(true);
    try {
      setVpn(await disconnectVpn());
    } catch (e) {
      toast(refusalText(t, e), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.vpn')} />
      <TrafficCard hue={0} active={active} busy={busy} onEdit={setOpen} onDisconnect={() => void disconnect()} />
      <ProviderTiles hue={1} vpn={vpn} onPick={setOpen} />
      {picked && (
        <ConnectDialog provider={picked} current={active} hue={1} onClose={() => setOpen('')} onState={setVpn} />
      )}
    </div>
  );
}

/**
 * TrafficCard says in one line whether torrents go through a VPN, and through
 * which. While that provider is connecting or has failed, torrents wait.
 */
function TrafficCard({
  hue,
  active,
  busy,
  onEdit,
  onDisconnect,
}: {
  hue: number;
  active?: VpnProvider;
  busy: boolean;
  onEdit: (id: string) => void;
  onDisconnect: () => void;
}) {
  const { t } = useT();
  const name = active ? nameOf(t, active) : '';
  const waiting = active !== undefined && active.state !== 'connected';
  let sub = t('settings.vpn.notThroughSub');
  if (active?.state === 'connecting') sub = t('settings.vpn.connectingSub');
  else if (active?.state === 'failed') sub = t('settings.vpn.failedSub', { name });
  else if (active) sub = '';

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle>{t('settings.vpn.trafficTitle')}</SectionTitle>
      <div className="flex items-center gap-3">
        {active ? <VpnMark id={active.id} name={name} size={36} /> : <IconTile icon={<IconGlobe />} />}
        <div className="flex min-w-0 flex-col gap-0.5">
          <p className="flex items-center text-sm font-semibold text-carbon-text">
            {active ? t('settings.vpn.through', { name }) : t('settings.vpn.notThrough')}
            <InfoBubble tip={t('settings.vpn.killSwitchHint')} />
          </p>
          {sub && <p className={`text-meta ${waiting ? 'text-statusWarn' : 'text-carbon-textSub'}`}>{sub}</p>}
        </div>
      </div>
      {active && <VpnBadges provider={active} t={t} />}
      {active?.state === 'failed' && (
        <p role="status" className="text-sm text-statusFail">
          {failureOf(t, active)}
        </p>
      )}
      {active && (
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Button kind="secondary" onClick={() => onEdit(active.id)}>
            {t('settings.vpn.edit')}
          </Button>
          <Button kind="secondary" hint={t('settings.vpn.disconnectHint')} disabled={busy} onClick={onDisconnect}>
            {t('settings.vpn.disconnect')}
          </Button>
        </div>
      )}
    </Card>
  );
}
