// Fleet shows every instance of the group, this one first, as a card with its
// state and its live figures, and below them the instances announcing
// themselves on this network, which can be added by address.
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
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
} from '../../lib/api';
import { useT } from '../../lib/i18n';
import { basePath } from '../../lib/basePath';
import { IconInstances } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { Button, Card, EmptyState, SectionTitle } from '../../components/ui';
import { InstanceCard } from '../../components/InstanceCard';

/** How often the tab asks who is there. The answer is this instance's own
 *  view of the group and of its stored peers, so asking costs nothing on the
 *  network; each card reads its own figures. */
export const FLEET_REFRESH_MS = 20_000;

export function Fleet({ onOpenPairing }: { onOpenPairing: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const [peers, setPeers] = useState<Instance[] | null>(null);
  // One failure counter per discovered row, so a refusal shakes that row's button.
  const [shakes, setShakes] = useState<Record<string, number>>({});
  // The name set in settings/Access.tsx, so this instance shows like a peer.
  const [ownName, setOwnName] = useState('');
  // Instances announcing themselves on this network (internal/discovery),
  // polled so an open page follows them coming and going.
  const [found, setFound] = useState<DiscoveredInstance[]>([]);
  const navigate = useNavigate();

  const load = () =>
    fetchInstances()
      .then(setPeers)
      .catch(() => {});
  const loadFound = () => fetchDiscovered().then(setFound).catch(() => {});
  useEffect(() => {
    load();
    loadFound();
    fetchSettings()
      .then((s: Settings) => setOwnName(s.instanceName))
      .catch(() => {});
    const everyFew = window.setInterval(loadFound, 5000);
    const refresh = () => {
      if (document.visibilityState === 'visible') load();
    };
    const every20 = window.setInterval(refresh, FLEET_REFRESH_MS);
    document.addEventListener('visibilitychange', refresh);
    const close = connectWS(
      (type) => {
        if (type === 'settings') load();
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
      await load();
      await loadFound();
    } catch (e: unknown) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail', 'action-failed');
      shake(f.id);
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-10">
      {/* One card width wherever the list stands, sized so the name with its
          state and the three figures each keep one line. A narrow window shows
          fewer cards. */}
      <div className="grid grid-cols-[repeat(auto-fill,min(100%,28rem))] gap-4">
        {/* Open goes to the local download list, with no ?instance=. */}
        <InstanceCard
          name={ownName || t('instances.thisInstance')}
          address={location.host + basePath()}
          base="/api"
          // Without a configured name the title already says "this instance".
          isSelf={ownName !== ''}
          onOpen={() => navigate('/downloads')}
          hue={0}
        />
        {(peers ?? []).map((p, i) => (
          <InstanceCard
            key={p.name}
            name={p.displayName ?? p.name}
            // A group member is reached however the group reaches it, so it
            // shows no address.
            address={p.relayId ? '' : p.url}
            base={`/api/instances/${encodeURIComponent(p.name)}`}
            onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)}
            // A group member is built per request from the relay's connections
            // and is not stored, so there is nothing to remove.
            onRemove={p.relayId ? undefined : () => onRemove(p.name)}
            // The own card above is position 0.
            hue={i + 1}
          />
        ))}
      </div>

      {peers?.length === 0 && (
        <EmptyState
          icon={<IconInstances width={26} height={26} />}
          title={t('fleet.emptyTitle')}
          hint={t('fleet.empty')}
          action={
            <Button kind="secondary" onClick={onOpenPairing}>
              {t('fleet.openPairing')}
            </Button>
          }
        />
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
