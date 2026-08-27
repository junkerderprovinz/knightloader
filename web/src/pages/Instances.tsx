import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  type DiscoveredInstance,
  type Instance,
  type Settings,
  addInstance,
  fetchDiscovered,
  fetchInstances,
  fetchSettings,
  removeInstance,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { PageHeader, Card, Button, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';
import { FirstTouchHint } from '../components/FirstTouchHint';

export function Instances() {
  const { t } = useT();
  const [peers, setPeers] = useState<Instance[]>([]);
  const [err, setErr] = useState('');
  // The configured name (settings/Access.tsx's own IdentityCard), so this
  // instance shows up on its own card the same way a peer does - not the
  // generic "this instance" placeholder (jdp: "unter instanz soll diese
  // instanz mit dem eingestellten namen erscheinen nicht mit 'diese
  // instanz'"). Falls back to the placeholder for the common case of never
  // having named it.
  const [ownName, setOwnName] = useState('');
  // Instances announcing themselves on this network (internal/discovery).
  // Polled rather than pushed: an instance appears when it boots and drops out
  // when it stops announcing, so a page left open should follow that without
  // needing a reload.
  const [found, setFound] = useState<DiscoveredInstance[]>([]);
  const navigate = useNavigate();

  const load = () => fetchInstances().then(setPeers);
  const loadFound = () => fetchDiscovered().then(setFound).catch(() => {});
  useEffect(() => {
    load();
    loadFound();
    fetchSettings()
      .then((s: Settings) => setOwnName(s.instanceName))
      .catch(() => {});
    const iv = setInterval(loadFound, 5000);
    return () => clearInterval(iv);
  }, []);

  // One click instead of typing an address. Deliberately the SAME add the
  // form below runs - discovery supplies the address, it does not grant any
  // trust of its own, and a peer with a password will still refuse it. What
  // makes two instances trust each other is the connection phrase, which they
  // both hold rather than trade.
  async function onAddFound(f: DiscoveredInstance) {
    setErr('');
    try {
      const r = await addInstance(f.name, f.url);
      // "Refused us" and "could not be reached" have completely different
      // fixes, so they get different sentences. See addInstance's own doc.
      if (r.refused) setErr(t('instances.refused'));
      else if (!r.online) setErr(t('instances.offlineWarning'));
      await load();
      await loadFound();
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Subtitle removed (jdp, 2026-08-24: "text entfernen: Alle
          KnightLoader von einer Oberfläche aus sehen und steuern.") - the
          title alone already says what this page is. */}
      <PageHeader title={t('instances.title')} />

      <FirstTouchHint id="instances" />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <InstanceCard name={ownName || t('instances.thisInstance')} url={location.host} base="/api" />
        {peers.map((p, i) => (
          <InstanceCard
            key={p.name}
            name={p.displayName ?? p.name}
            url={p.url}
            relayId={p.relayId}
            base={`/api/instances/${encodeURIComponent(p.name)}`}
            onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)}
            // No remove badge for a relay peer, because there is nothing to
            // remove. A relay peer is synthesised per request from whoever is
            // currently connected to the relay (federation.Manager.reachable)
            // and is never in the stored list, so Remove deleted nothing, the
            // route still answered 204, and the reload put the identical card
            // straight back - a button that reproducibly did nothing, silently.
            // A relay peer goes away by disconnecting it or clearing the relay
            // config, not from here.
            onRemove={p.relayId ? undefined : () => onRemove(p.name)}
            hue={i}
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
                <span className="ml-2 text-xs text-carbon-textMuted" dir="ltr">
                  {f.url}
                </span>
              </span>
              {f.known ? (
                <span className="text-xs text-carbon-textMuted">{t('instances.foundKnown')}</span>
              ) : (
                <Button kind="secondary" className="px-2.5 text-xs" onClick={() => void onAddFound(f)}>
                  {t('instances.foundAdd')}
                </Button>
              )}
            </div>
          ))}
          {/* Moved in from the now-removed manual-add card (jdp,
              2026-08-26): this is the only remaining action in this file
              that can set err (onAddFound), so it is the only remaining
              place that needs to show it. */}
          {err && <div className="text-statusFail text-sm">{err}</div>}
        </Card>
      )}

      {/* Manual add and pairing-by-code used to be two cards here, duplicating
          what settings/Access.tsx's own RemoteAccessCard already does more
          completely - jdp, 2026-08-26: "nur ein button der auf den zugangstab
          in den einstellungen verweist soll in dem tab sein". One button now,
          instead of two separate, narrower forms for the same job in two
          places. Pairing itself is gone entirely; the connection phrase over
          there does what it did, for every instance at once. */}
      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" onClick={() => navigate('/settings/access')}>
          {t('instances.connectButton')}
        </Button>
        <span className="text-xs text-carbon-textMuted">{t('instances.connectHint')}</span>
      </div>
    </div>
  );
}
