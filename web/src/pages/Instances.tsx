// Instances shows the group as a net and every instance of it as a card, this
// one first and the phones of the group last, and below them the instances
// announcing themselves on this network, which can be added by address.
// Pairing itself happens on the Pairing page in Settings, which the Pairing
// button opens.
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  type ConnectInfo,
  type DiscoveredInstance,
  type Instance,
  type Settings,
  addInstance,
  connectWS,
  fetchConnect,
  fetchDeploymentInfo,
  fetchDiscovered,
  fetchInstances,
  fetchSettings,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { basePath } from '../lib/basePath';
import { openExternal } from '../lib/external';
import { IconLink } from '../lib/icons';
import { useToast } from '../lib/toast';
import { fetchFeatures, type Feature } from './settings/features';
import { ModulesPageBadge } from './settings/ModuleToggle';
import { Button, Card, PageHeader, SectionTitle } from '../components/ui';
import { GroupNet, SELF_KEY, appNodeKind, useNetHover, type NetNode } from '../components/GroupNet';
import { AppCard, InstanceCard } from '../components/InstanceCard';
import { RemovalWindow, type Removal } from '../components/InstanceRemoval';

/** How often the page asks who is there. The answer is this instance's own
 *  view of the group and of its stored peers, so asking costs nothing on the
 *  network; each card reads its own figures. */
export const INSTANCES_REFRESH_MS = 20_000;

/** The address as a card shows it, the way it would be typed. */
function withoutScheme(url: string): string {
  return url.replace(/^https?:\/\//, '');
}

export function Instances({
  firstHue = 0,
}: {
  /** The palette position of the grid's card, which follows the cards above it. */
  firstHue?: number;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const navigate = useNavigate();
  const [peers, setPeers] = useState<Instance[] | null>(null);
  const [group, setGroup] = useState<ConnectInfo | null>(null);
  // One failure counter per discovered row, so a refusal shakes that row's button.
  const [shakes, setShakes] = useState<Record<string, number>>({});
  // The name set in settings/Access.tsx, so this instance shows like a peer,
  // and which build it is, for the glyph before that name.
  const [ownName, setOwnName] = useState('');
  const [ownKind, setOwnKind] = useState('');
  // Instances announcing themselves on this network (internal/discovery),
  // polled so an open page follows them coming and going.
  const [found, setFound] = useState<DiscoveredInstance[]>([]);
  // The module's row while it is switched off. The server then lists no peers,
  // and the page says why instead of looking as if they were gone.
  const [off, setOff] = useState<Feature | null>(null);
  const net = useNetHover<HTMLDivElement>();
  // Whether each instance answered its card, by the card's key, so the net can
  // dim the node of one that does not.
  const [answers, setAnswers] = useState<Record<string, boolean>>({});
  const [removing, setRemoving] = useState<Removal | null>(null);

  const load = () => {
    fetchInstances()
      .then(setPeers)
      .catch(() => {});
    fetchConnect()
      .then(setGroup)
      .catch(() => {});
  };
  const loadFound = () => fetchDiscovered().then(setFound).catch(() => {});
  const loadOff = () =>
    fetchFeatures()
      .then((f) => setOff(f.modules.find((m) => m.id === 'federation' && m.verdict === 'shipped' && !m.enabled) ?? null))
      .catch(() => {});
  useEffect(() => {
    load();
    loadFound();
    loadOff();
    fetchSettings()
      .then((s: Settings) => setOwnName(s.instanceName))
      .catch(() => {});
    fetchDeploymentInfo()
      .then((d) => setOwnKind(d.deployment))
      .catch(() => {});
    const everyFew = window.setInterval(loadFound, 5000);
    const refresh = () => {
      if (document.visibilityState === 'visible') load();
    };
    const every20 = window.setInterval(refresh, INSTANCES_REFRESH_MS);
    document.addEventListener('visibilitychange', refresh);
    const close = connectWS(
      (type) => {
        if (type !== 'settings') return;
        load();
        loadOff();
      },
      ['settings'],
    );
    return () => {
      window.clearInterval(everyFew);
      window.clearInterval(every20);
      document.removeEventListener('visibilitychange', refresh);
      close();
    };
  }, []);

  function shake(id: string) {
    setShakes((s) => ({ ...s, [id]: (s[id] ?? 0) + 1 }));
  }

  // Discovery only supplies the address; the peer still decides whether to
  // trust us.
  async function onAddFound(f: DiscoveredInstance) {
    try {
      const r = await addInstance(f.name, f.url);
      // A refusal and an unreachable peer need different fixes, so they get
      // different toasts.
      if (r.refused) {
        toast(t('instances.refusedByPassword'), 'fail', 'action-failed');
        shake(f.id);
      } else if (!r.online) {
        toast(t('instances.offlineWarning'), 'fail', 'action-failed');
        shake(f.id);
      }
      load();
      await loadFound();
    } catch (e: unknown) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail', 'action-failed');
      shake(f.id);
    }
  }

  const openPairing = () => navigate('/settings/pairing');
  const apps = group?.apps ?? [];
  // Nothing to show but this instance: the page is the way into pairing.
  const empty = !off && group !== null && peers !== null && !group.active && peers.length === 0 && apps.length === 0;

  // A group member carries the key the Pairing page gives it, so the two
  // pages name one thing one way.
  const entryOf = (p: Instance) => (p.relayId ? `member:${p.name}` : `peer:${p.name}`);
  const routeOf = (p: Instance) => {
    if (!p.relayId) return t('pairing.byAddress');
    const member = group?.members.find((m) => m.id === p.relayId);
    return member ? (member.direct ? t('pairing.direct') : t('pairing.viaRelay')) : '';
  };
  const onReach = (entry: string, online: boolean) =>
    setAnswers((a) => (a[entry] === online ? a : { ...a, [entry]: online }));

  const nodes: NetNode[] = [
    ...(peers ?? []).map(
      (p): NetNode => ({
        key: entryOf(p),
        kind: 'instance',
        name: p.displayName ?? p.name,
        sub: routeOf(p),
        dim: answers[entryOf(p)] === false,
      }),
    ),
    ...apps.map(
      (a): NetNode => ({
        key: `app:${a.id}`,
        kind: appNodeKind(a.deployment),
        name: a.name || a.id,
        sub: a.deployment === 'extension' ? t('instances.kind.extension') : t('instances.kind.mobile'),
        dim: !a.connected,
      }),
    ),
  ];

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('instances.title')} />

      {off && (
        <p className="flex flex-wrap items-center gap-x-2 text-sm text-carbon-textSub">
          {t('instances.moduleOff')}
          <ModulesPageBadge m={off} title={t('settings.nav.modules')} />
        </p>
      )}

      <Card hue={firstHue}>
        {/* One card width wherever the grid stands, here and on the Instances
            settings tab, and one height per row. A narrow window shows fewer
            cards. */}
        <div {...net.scope} className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] items-stretch gap-4">
          {nodes.length > 0 && (
            <div className="col-span-full rounded-[var(--radius-card)] bg-carbon-surface2 px-5 py-4 max-sm:p-3">
              <GroupNet
                self={{ name: ownName || t('instances.thisInstance'), sub: ownName ? t('instances.thisInstance') : '' }}
                nodes={nodes}
                hot={net.hot}
                onPick={net.pick}
                alt={t('pairing.netAlt')}
              />
            </div>
          )}
          {/* This instance shows the address the group is told, and Open
              stays here on the local download list. */}
          <InstanceCard
            entry={SELF_KEY}
            hot={net.hot === SELF_KEY}
            name={ownName || t('instances.thisInstance')}
            deployment={ownKind}
            address={withoutScheme(group?.address || location.host + basePath())}
            base="/api"
            // Without a configured name the title already says "this instance".
            isSelf={ownName !== ''}
            onOpen={() => navigate('/downloads')}
          />
          {(peers ?? []).map((p) => {
            // A group member's own announced address, a stored peer's the
            // one it was added with. Without one, Open shows its downloads
            // here.
            const address = (p.relayId ? p.address : p.url) ?? '';
            const entry = entryOf(p);
            const name = p.displayName ?? p.name;
            return (
              <InstanceCard
                key={p.name}
                entry={entry}
                hot={net.hot === entry}
                name={name}
                deployment={p.deployment}
                address={withoutScheme(address)}
                route={routeOf(p)}
                base={`/api/instances/${encodeURIComponent(p.name)}`}
                onOpen={
                  address ? () => openExternal(address) : () => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)
                }
                onReach={(online) => onReach(entry, online)}
                onRemove={() => setRemoving({ kind: p.relayId ? 'member' : 'peer', id: p.name, name })}
              />
            );
          })}
          {empty && (
            <button
              type="button"
              onClick={openPairing}
              className="flex flex-col items-center justify-center gap-1.5 rounded-[var(--radius-card)] border-[1.5px] border-dashed
                border-carbon-border px-5 py-7 text-center transition-colors hover:bg-carbon-hover
                focus-visible:shadow-[0_0_0_2px_var(--focus-ring)] focus-visible:outline-none"
            >
              <span className="text-sm font-semibold text-carbon-text">{t('pairing.title')}</span>
              <span className="text-sm text-carbon-textMuted">{t('instances.pairLead')}</span>
            </button>
          )}
          {apps.map((a) => (
            <AppCard
              key={a.id}
              entry={`app:${a.id}`}
              hot={net.hot === `app:${a.id}`}
              name={a.name || a.id}
              deployment={a.deployment}
              connected={a.connected}
              lastSeen={a.lastSeen}
              onRemove={() => setRemoving({ kind: 'app', id: a.id, name: a.name || a.id })}
            />
          ))}
        </div>
      </Card>
      <div>
        <Button icon={<IconLink />} onClick={openPairing}>
          {t('pairing.title')}
        </Button>
      </div>
      {removing && <RemovalWindow removal={removing} onClose={() => setRemoving(null)} onRemoved={load} />}

      {found.length > 0 && (
        <Card hue={firstHue + 1} className="flex flex-col gap-3">
          <SectionTitle hint={t('instances.foundHint')}>{t('instances.foundTitle')}</SectionTitle>
          {found.map((f) => (
            <div key={f.id} className="flex flex-wrap items-center gap-3">
              <span className="min-w-0 flex-1">
                <span className="text-sm text-carbon-text">{f.name}</span>
                <span className="ms-2 text-xs text-carbon-textMuted" dir="ltr">
                  {f.url}
                </span>
              </span>
              {f.known ? (
                <span className="text-xs text-carbon-textMuted">{t('instances.foundKnown')}</span>
              ) : (
                <Button
                  kind="secondary"
                  className="px-2.5 text-xs"
                  shake={shakes[f.id] ?? 0}
                  onClick={() => void onAddFound(f)}
                >
                  {t('instances.foundAdd')}
                </Button>
              )}
            </div>
          ))}
        </Card>
      )}
    </div>
  );
}
