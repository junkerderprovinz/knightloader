// PairingSection is where instances become one group by twelve words, on the
// Remote access page beside the password that guards them: a sentence and
// three cards explain it, one card holds the words and the members, and one
// picks the relay for members on other networks. The Instances page, the
// phone app and the browser extension all build on the group it sets up.
import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchConnect, fetchRelayConfig, type ConnectInfo, type RelayConfig } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { PairingSteps } from './PairingSteps';
import { PhraseCard } from './PhraseCard';
import { RelayCard } from './RelayCard';

/** The address fragment the Instances page opens this section with. */
export const PAIRING_ANCHOR = 'pairing';

// How often the section asks again who is there. Members come and go with the
// relay connection and the local network, which nothing pushes here.
const REFRESH_MS = 10_000;

export function PairingSection() {
  const { t } = useT();
  const [group, setGroup] = useState<ConnectInfo | null>(null);
  const [relay, setRelay] = useState<RelayConfig | null>(null);
  const [error, setError] = useState(false);
  const box = useRef<HTMLElement>(null);

  const load = useCallback(() => {
    Promise.all([fetchConnect(), fetchRelayConfig()])
      .then(([g, r]) => {
        setGroup(g);
        setRelay(r);
        setError(false);
      })
      .catch(() => setError(true));
  }, []);

  useEffect(() => {
    load();
    const id = window.setInterval(load, REFRESH_MS);
    return () => window.clearInterval(id);
  }, [load]);

  // Scrolled to once the cards below have drawn, so the steps land at the top.
  const arrived = group !== null;
  useEffect(() => {
    if (arrived && window.location.hash === `#${PAIRING_ANCHOR}`) box.current?.scrollIntoView({ block: 'start' });
  }, [arrived]);

  return (
    <section ref={box} id={PAIRING_ANCHOR} className="flex scroll-mt-6 flex-col gap-10">
      <PairingSteps hues={[0, 1, 2]} />
      {error && <p className="text-sm text-statusFail">{t('pairing.loadError')}</p>}
      {group && <PhraseCard group={group} onGroup={setGroup} onRefresh={load} hue={3} />}
      {group && relay && (
        <RelayCard
          group={group}
          relay={relay}
          onRelay={(r) => {
            setRelay(r);
            load();
          }}
          hue={4}
        />
      )}
    </section>
  );
}
