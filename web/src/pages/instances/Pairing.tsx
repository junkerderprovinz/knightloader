// Pairing is the Instances tab where instances become one group by twelve
// words: a sentence and three cards explain it, one card holds the words and
// the members, and one picks the relay the group meets on. The Fleet tab, the
// phone app and the browser extension all build on the group it sets up.
import { useCallback, useEffect, useState } from 'react';
import { fetchConnect, fetchRelayConfig, type ConnectInfo, type RelayConfig } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { PairingSteps } from './PairingSteps';
import { PhraseCard } from './PhraseCard';
import { RelayCard } from './RelayCard';

// How often the page asks again who is there. Members come and go with the
// relay connection, which nothing pushes here.
const REFRESH_MS = 10_000;

export function Pairing() {
  const { t } = useT();
  const [group, setGroup] = useState<ConnectInfo | null>(null);
  const [relay, setRelay] = useState<RelayConfig | null>(null);
  const [error, setError] = useState(false);

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

  return (
    <div className="flex flex-col gap-10">
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
    </div>
  );
}
