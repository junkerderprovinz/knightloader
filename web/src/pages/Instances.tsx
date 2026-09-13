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
import { useToast } from '../lib/toast';
import { PageHeader, Card, Button, InfoBubble, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';

export function Instances() {
  const { t } = useT();
  const { toast } = useToast();
  const [peers, setPeers] = useState<Instance[]>([]);
  // One counter PER discovered row, not one for the list: two rows share this
  // handler, and a single nonce would shake whichever button was rendered
  // last rather than the one that was pressed. Bumping it remounts that row's
  // own button, which is what lets .glim-shake replay on a repeated identical
  // failure - a class that leaves and returns in the same frame does not.
  const [shakes, setShakes] = useState<Record<string, number>>({});
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
  /** Bumps one row's shake counter, which remounts that row's own button. */
  function shake(id: string) {
    setShakes((s) => ({ ...s, [id]: (s[id] ?? 0) + 1 }));
  }

  async function onAddFound(f: DiscoveredInstance) {
    try {
      const r = await addInstance(f.name, f.url);
      // "Refused us" and "could not be reached" have completely different
      // fixes, so they get different sentences. See addInstance's own doc.
      // Both go into the toast rather than onto the page: an outcome sentence
      // left standing beside the button never clears itself, so a refusal
      // from ten minutes ago reads exactly as current as one from a second
      // ago - and this one used to sit inside the discovery card, which is
      // gone entirely the moment the network stops announcing anything.
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
    // gap-10, the house gap between stacked cards - pages/Accounts.tsx and
    // every settings page's own root carry the same one. This page stood at
    // gap-6 alone, which only became visible once settings/Instances.tsx wrapped
    // it beside a toggle card at the correct 40: one stack, two rhythms.
    <div className="flex flex-col gap-10">
      {/* Subtitle removed (jdp, 2026-08-24: "text entfernen: Alle
          KnightLoader von einer Oberfläche aus sehen und steuern.") - the
          title alone already says what this page is. */}
      {/* No explainer strip here any more (jdp, 2026-09-06: "im instanzentab
          dieser hinweis weg: Ein Gegenstück, keine Kopie ... Diese art von
          infotexten können überall weg"). The page's own cards say what they
          are, and anything genuinely worth explaining belongs in the (i) on a
          card badge, which is where the last two of these already went. */}
      <PageHeader title={t('instances.title')} />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {/* Its own Open button, pointing at the local download list (jdp,
            2026-08-27: "bei der eigenenn instanz soll der button öffnen
            genauso da sien und in den downloadtab verweisen") - without an
            ?instance= parameter, which is exactly what "the one you are on"
            means to the Downloads page. One card shape for every instance;
            which one you are looking at is said by the label, not by a
            missing button. */}
        <InstanceCard
          name={ownName || t('instances.thisInstance')}
          url={location.host}
          base="/api"
          // Only when the name says something else. With no configured name
          // the card is already titled "Diese Instanz", and a label repeating
          // the title verbatim reads as a rendering bug rather than as a hint.
          isSelf={ownName !== ''}
          onOpen={() => navigate('/downloads')}
          // This grid is one equal-member set, and the card you are standing
          // on is its first member, not an exception to it: leaving it without
          // a position left the one card that is always on screen wearing the
          // single accent while every neighbour beside it was coloured.
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
            // No remove badge for a relay peer, because there is nothing to
            // remove. A relay peer is synthesised per request from whoever is
            // currently connected to the relay (federation.Manager.reachable)
            // and is never in the stored list, so Remove deleted nothing, the
            // route still answered 204, and the reload put the identical card
            // straight back - a button that reproducibly did nothing, silently.
            // A relay peer goes away by disconnecting it or clearing the relay
            // config, not from here.
            onRemove={p.relayId ? undefined : () => onRemove(p.name)}
            // i + 1, because the own-instance card above is position 0 of the
            // same run - the loop's own index counts peers, not cards.
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
                <span className="ml-2 text-xs text-carbon-textMuted" dir="ltr">
                  {f.url}
                </span>
              </span>
              {f.known ? (
                <span className="text-xs text-carbon-textMuted">{t('instances.foundKnown')}</span>
              ) : (
                <Button
                  // Remounted on every failed press of THIS row, so the
                  // animation plays again on a second identical refusal.
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
        {/* Behind the (i) rather than as grey prose beside the button (jdp,
            2026-09-06: "Diese art von infotexten können überall weg") - the
            same rule that put every other explanation in this app into a
            bubble. */}
        <InfoBubble tip={t('instances.connectHint')} />
      </div>
    </div>
  );
}
