// PairingSection is where instances become one group by twelve words: three
// cards explain it, one holds the words, one lists who came with them and one
// picks the relay for members on other networks. The Instances page, the phone
// app and the browser extension all build on the group it sets up.
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { fetchConnect, fetchRelayConfig, type ConnectInfo, type RelayConfig } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { PairingGroup } from './PairingGroup';
import { PairingSteps } from './PairingSteps';
import { RelayCard } from './RelayCard';

// How often the section asks again who is there. Members come and go with the
// relay connection and the local network, which nothing pushes here.
const REFRESH_MS = 10_000;

export function PairingSection({
  identity,
}: {
  /** The card with this instance's name and addresses, which reads what the
   *  instance found about itself from the group's answer. */
  identity: (group: ConnectInfo | null) => ReactNode;
}) {
  const { t } = useT();
  const [group, setGroup] = useState<ConnectInfo | null>(null);
  const [relay, setRelay] = useState<RelayConfig | null>(null);
  const [error, setError] = useState(false);
  // Counts the reads and the answers the cards hand up. A read that was out
  // while a newer one or such an answer came back would put back the state
  // from before, such as an inactive group over words just created.
  const asked = useRef(0);

  const load = useCallback(() => {
    const read = ++asked.current;
    Promise.all([fetchConnect(), fetchRelayConfig()])
      .then(([g, r]) => {
        if (read !== asked.current) return;
        setGroup(g);
        setRelay(r);
        setError(false);
      })
      .catch(() => read === asked.current && setError(true));
  }, []);

  const takeGroup = (g: ConnectInfo) => {
    asked.current++;
    setGroup(g);
  };

  useEffect(() => {
    load();
    const id = window.setInterval(load, REFRESH_MS);
    return () => window.clearInterval(id);
  }, [load]);

  return (
    <section className="flex flex-col gap-10">
      <PairingSteps hues={[0, 1, 2]} />
      {error && <p className="text-sm text-statusFail">{t('pairing.loadError')}</p>}
      {group && <PairingGroup group={group} onGroup={takeGroup} onRefresh={load} hues={[3, 4]} />}
      {/* Between the group and the relay, since both show the name it sets. */}
      {identity(group)}
      {group && relay && (
        <RelayCard
          group={group}
          relay={relay}
          onRelay={(r) => {
            setRelay(r);
            load();
          }}
          onRefresh={load}
          hue={6}
        />
      )}
    </section>
  );
}
