// Instances shows every instance of the group as a card, this one first, the
// phones of the group after them, and below them the instances announcing
// themselves on this network, which can be added by address. Pairing itself
// happens on the Pairing page in Settings, which the Pairing button opens.
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
  forgetApp,
  removeInstance,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { basePath } from '../lib/basePath';
import { openExternal } from '../lib/external';
import { IconLink } from '../lib/icons';
import { useToast } from '../lib/toast';
import { fetchFeatures, type Feature } from './settings/features';
import { ModulesPageBadge } from './settings/ModuleToggle';
import { Button, Card, PageHeader, SectionTitle } from '../components/ui';
import { AppCard, InstanceCard } from '../components/InstanceCard';

/** How often the page asks who is there. The answer is this instance's own
 *  view of the group and of its stored peers, so asking costs nothing on the
 *  network; each card reads its own figures. */
export const INSTANCES_REFRESH_MS = 20_000;

/** The address as a card shows it, the way it would be typed. */
function withoutScheme(url: string): string {
  return url.replace(/^https?:\/\//, '');
}

export function Instances() {
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

  async function onRemove(n: string) {
    await removeInstance(n);
    load();
  }

  async function onForgetApp(id: string) {
    try {
      await forgetApp(id);
    } catch (e: unknown) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail', 'action-failed');
    }
    load();
  }

  const openPairing = () => navigate('/settings/pairing');
  const apps = group?.apps ?? [];
  // Nothing to show but this instance: the page is the way into pairing.
  const empty = !off && group !== null && peers !== null && !group.active && peers.length === 0 && apps.length === 0;

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('instances.title')} />

      {off && (
        <p className="flex flex-wrap items-center gap-x-2 text-sm text-carbon-textSub">
          {t('instances.moduleOff')}
          <ModulesPageBadge m={off} title={t('settings.nav.modules')} />
        </p>
      )}

      {empty ? (
        <div className="flex min-h-[50vh] flex-col items-center justify-center">
          <button
            type="button"
            onClick={openPairing}
            className="grid w-full max-w-md grid-cols-[48px_minmax(0,1fr)] items-center gap-4 rounded-[var(--radius-card)] bg-carbon-surface
              p-5 text-start transition-colors hover:bg-carbon-surface2 focus-visible:shadow-[0_0_0_2px_var(--focus-ring)]
              focus-visible:outline-none"
          >
            <span className="grid h-12 w-12 place-items-center rounded-[var(--radius-control)] bg-accent text-accentContrast">
              <IconLink />
            </span>
            <span>
              <span className="block text-lg font-semibold text-carbon-text">{t('pairing.title')}</span>
              <span className="mt-0.5 block text-sm text-carbon-textSub">{t('instances.pairLead')}</span>
            </span>
          </button>
        </div>
      ) : (
        <>
          {/* One card width wherever the list stands, here and on the Instances
              settings tab, sized so the name with its status and the three
              figures each keep one line. A narrow window shows fewer cards. */}
          <div className="grid grid-cols-[repeat(auto-fill,min(100%,28rem))] gap-4">
            {/* This instance shows the address the group is told, and Open
                stays here on the local download list. */}
            <InstanceCard
              name={ownName || t('instances.thisInstance')}
              deployment={ownKind}
              address={withoutScheme(group?.address || location.host + basePath())}
              base="/api"
              // Without a configured name the title already says "this instance".
              isSelf={ownName !== ''}
              onOpen={() => navigate('/downloads')}
              hue={0}
            />
            {(peers ?? []).map((p, i) => {
              // A group member's own announced address, a stored peer's the
              // one it was added with. Without one, Open shows its downloads
              // here.
              const address = (p.relayId ? p.address : p.url) ?? '';
              return (
                <InstanceCard
                  key={p.name}
                  name={p.displayName ?? p.name}
                  deployment={p.deployment}
                  address={withoutScheme(address)}
                  base={`/api/instances/${encodeURIComponent(p.name)}`}
                  onOpen={
                    address
                      ? () => openExternal(address)
                      : () => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)
                  }
                  // A group member is built per request from the group's
                  // connections and is not stored, so there is nothing to remove.
                  onRemove={p.relayId ? undefined : () => onRemove(p.name)}
                  // The own card above is position 0.
                  hue={i + 1}
                />
              );
            })}
            {apps.map((a, i) => (
              <AppCard
                key={a.id}
                name={a.name || a.id}
                connected={a.connected}
                lastSeen={a.lastSeen}
                hue={(peers?.length ?? 0) + i + 1}
                onRemove={() => void onForgetApp(a.id)}
              />
            ))}
          </div>
          <div>
            <Button icon={<IconLink />} onClick={openPairing}>
              {t('pairing.title')}
            </Button>
          </div>
        </>
      )}

      {found.length > 0 && (
        <Card className="flex flex-col gap-3">
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
