import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
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
import { IconChevronEnd } from '../lib/icons';
import { fetchFeatures } from './settings/features';
import { useToast } from '../lib/toast';
import { PageHeader, Card, Button, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';

export function Instances() {
  const { t } = useT();
  const { toast } = useToast();
  const [peers, setPeers] = useState<Instance[]>([]);
  // One counter per discovered row. Bumping it remounts that row's button so
  // .glim-shake replays on a repeated failure.
  const [shakes, setShakes] = useState<Record<string, number>>({});
  // The name set in settings/Access.tsx, so this instance shows like a peer.
  const [ownName, setOwnName] = useState('');
  // Instances announcing themselves on this network (internal/discovery),
  // polled so an open page follows them coming and going.
  const [found, setFound] = useState<DiscoveredInstance[]>([]);
  const navigate = useNavigate();

  // Switched off on the Modules page, the server lists no peers; the page says
  // why instead of looking as if they were gone.
  const [off, setOff] = useState(false);

  const load = () => fetchInstances().then(setPeers);
  const loadFound = () => fetchDiscovered().then(setFound).catch(() => {});
  const loadOff = () =>
    fetchFeatures()
      .then((f) => setOff(f.modules.some((m) => m.id === 'federation' && m.verdict === 'shipped' && !m.enabled)))
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
        toast(t('instances.refused'), 'fail', 'action-failed');
        shake(f.id);
      } else if (!r.online) {
        toast(t('instances.offlineWarning'), 'fail', 'action-failed');
        shake(f.id);
      }
      await load();
      await loadFound();
    } catch (e: any) {
      toast(t('list.failed', { error: String(e?.message ?? e) }), 'fail', 'action-failed');
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
          <Link
            to="/settings/modules"
            className="flex items-center gap-1 underline-offset-2 hover:text-carbon-text hover:underline focus-visible:underline"
          >
            {t('settings.nav.modules')}
            <IconChevronEnd className="h-3.5 w-3.5 rtl:-scale-x-100" aria-hidden />
          </Link>
        </p>
      )}

      {/* Columns of at least 20rem, so a narrow window shows fewer cards
          instead of squeezing the logo, the name and the figures of each. */}
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,20rem),1fr))] gap-4">
        {/* Open goes to the local download list, with no ?instance=. */}
        <InstanceCard
          name={ownName || t('instances.thisInstance')}
          url={location.host}
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
                  key={shakes[f.id] ?? 0}
                  kind="secondary"
                  className={`px-2.5 text-xs${shakes[f.id] ? ' glim-shake' : ''}`}
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
        <Button kind="secondary" hint={t('instances.connectHint')} onClick={() => navigate('/settings/access')}>
          {t('instances.connectButton')}
        </Button>
      </div>
    </div>
  );
}
