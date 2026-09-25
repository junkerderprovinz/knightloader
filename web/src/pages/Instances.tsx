import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ApiError,
  type DiscoveredInstance,
  type Instance,
  type Settings,
  addInstance,
  connectWS,
  fetchDiscovered,
  fetchInstances,
  fetchSettings,
  removeInstance,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { basePath } from '../lib/basePath';
import { fetchFeatures, type Feature } from './settings/features';
import { ModulesPageBadge } from './settings/ModuleToggle';
import { useToast } from '../lib/toast';
import { PageHeader, Card, Button, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';

export function Instances() {
  const { t } = useT();
  const { toast } = useToast();
  const [peers, setPeers] = useState<Instance[]>([]);
  // One failure counter per discovered row, so a refusal shakes that row's button.
  const [shakes, setShakes] = useState<Record<string, number>>({});
  // The name set in settings/Access.tsx, so this instance shows like a peer.
  const [ownName, setOwnName] = useState('');
  // Instances announcing themselves on this network (internal/discovery),
  // polled so an open page follows them coming and going.
  const [found, setFound] = useState<DiscoveredInstance[]>([]);
  const navigate = useNavigate();

  // The module's row while it is switched off. The server then lists no peers,
  // and the page says why instead of looking as if they were gone.
  const [off, setOff] = useState<Feature | null>(null);

  const load = () => fetchInstances().then(setPeers);
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
    const iv = setInterval(loadFound, 5000);
    const close = connectWS(
      (type) => {
        if (type !== 'settings') return;
        load();
        loadOff();
      },
      ['settings'],
    );
    return () => {
      clearInterval(iv);
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
      await load();
      await loadFound();
    } catch (e: unknown) {
      // Discovery keeps listing while peer instances are switched off, but
      // adding one is refused.
      if (e instanceof ApiError && e.code === 'federationOff') toast(t('instances.moduleOff'), 'fail', 'action-failed');
      else toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail', 'action-failed');
      shake(f.id);
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('instances.title')} />

      {off && (
        <p className="flex flex-wrap items-center gap-x-2 text-sm text-carbon-textSub">
          {t('instances.moduleOff')}
          <ModulesPageBadge m={off} title={t('settings.nav.modules')} />
        </p>
      )}

      {/* One card width wherever the list stands, here and on the Instances
          settings tab, sized so the name with its status and the three figures
          each keep one line. A narrow window shows fewer cards. */}
      <div className="grid grid-cols-[repeat(auto-fill,min(100%,28rem))] gap-4">
        {/* Open goes to the local download list, with no ?instance=. */}
        <InstanceCard
          name={ownName || t('instances.thisInstance')}
          url={location.host + basePath()}
          base="/api"
          // Without a configured name the title already says "this instance".
          isSelf={ownName !== ''}
          onOpen={() => navigate('/downloads')}
          hue={0}
        />
        {peers.map((p, i) => (
          <InstanceCard
            key={p.name}
            name={p.displayName ?? p.name}
            url={p.url}
            relayId={p.relayId}
            base={`/api/instances/${encodeURIComponent(p.name)}`}
            onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)}
            // A relay peer is built per request from the relay's connections
            // and is not stored, so there is nothing to remove.
            onRemove={p.relayId ? undefined : () => onRemove(p.name)}
            // The own card above is position 0.
            hue={i + 1}
          />
        ))}
      </div>

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

      {/* Adding instances lives in settings/Access.tsx's RemoteAccessCard. */}
      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" hint={t('instances.connectWhere')} onClick={() => navigate('/settings/access')}>
          {t('instances.connectButton')}
        </Button>
      </div>
    </div>
  );
}
